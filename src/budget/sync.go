package budget

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Target 预算包同步目标配置
type Target struct {
	ID    string
	Name  string
	Depth int
}

// SyncConfig 同步配置
type SyncConfig struct {
	Targets []Target
	Workers int
}

// Sync 同步所有匹配的预算包，构建树并写入 Store
func Sync(store *Store, fetcher *EkbFetcher, cfg SyncConfig) {
	start := time.Now()
	log.Printf("[Sync] 开始同步预算数据... (并发数: %d)", cfg.Workers)

	token, err := fetcher.GetToken()
	if err != nil {
		log.Printf("[Sync] 获取Token失败: %v", err)
		return
	}
	log.Printf("[Sync] Token OK")

	store.Clear()

	budgets, err := fetcher.FetchBudgetList(token)
	if err != nil {
		log.Printf("[Sync] 获取预算列表失败: %v", err)
		return
	}

	for _, b := range budgets {
		bName, _ := b["name"].(string)
		bID, _ := b["id"].(string)

		targetDepth := 0
		for _, t := range cfg.Targets {
			if t.ID != "" {
				if bID == t.ID {
					targetDepth = t.Depth
					break
				}
			} else if strings.Contains(bName, t.Name) {
				targetDepth = t.Depth
				break
			}
		}
		if targetDepth == 0 {
			log.Printf("    [Skip] 跳过: %s", bName)
			continue
		}

		log.Printf("[Sync] 同步: %s (深度: %d)", bName, targetDepth)

		tree := buildTree(bID, bName, fetcher, token, cfg.Workers, targetDepth)
		store.AddTree(tree)
		log.Printf("    -> %s 完成, 维度条目: %d", bName, store.Count())
	}

	log.Printf("[Sync] 同步完成! 耗时: %v, 总维度条目: %d", time.Since(start), store.Count())
}

// rawNode 从 API 返回的原始节点数据
type rawNode struct {
	nodeID   string
	nodeName string
	dimCode  string
	dimType  string
	isLeaf   bool
}

// buildTree 从根节点开始逐层构建预算包树
func buildTree(bID, bName string, fetcher *EkbFetcher, token string, workers, maxDepth int) *Tree {
	tree := &Tree{
		ID:   bID,
		Name: bName,
		Root: make(map[string]*Node),
	}

	realID := bID
	if !strings.HasPrefix(realID, "$") {
		realID = "$" + realID
	}
	queryURL := fetcher.QueryURL(realID)

	rootNodes, _, total, err := fetcher.FetchNodes(queryURL, "", token, workers)
	if err != nil {
		log.Printf("    [Error] 拉取根节点失败: %v", err)
		return tree
	}
	if total > 0 {
		log.Printf("    根节点子项总量: %d", total-1)
	}

	if len(rootNodes) == 0 {
		return tree
	}

	tree.DimType = rootNodes[0].dimType
	tree.MaxDepth = maxDepth

	type drillTask struct {
		parent map[string]*Node
		nodeID string
		depth  int
	}

	var pendingDrills []drillTask
	for _, rn := range rootNodes {
		node := &Node{
			DimCode:  rn.dimCode,
			NodeName: rn.nodeName,
			IsLeaf:   rn.isLeaf,
			Children: make(map[string]*Node),
		}
		tree.Root[rn.dimCode] = node
		if !rn.isLeaf && maxDepth > 1 {
			pendingDrills = append(pendingDrills, drillTask{parent: node.Children, nodeID: rn.nodeID, depth: 1})
		}
	}

	for depth := 1; depth < maxDepth; depth++ {
		if len(pendingDrills) == 0 {
			break
		}
		log.Printf("    [Layer %d] 钻取 %d 个节点...", depth+1, len(pendingDrills))

		type drillResult struct {
			parent   map[string]*Node
			children []rawNode
			task     drillTask
			err      error
		}

		ch := make(chan drillResult, len(pendingDrills))
		sem := make(chan struct{}, workers)
		var wg sync.WaitGroup

		for _, task := range pendingDrills {
			wg.Add(1)
			go func(t drillTask) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				nodes, _, _, err := fetcher.FetchNodes(queryURL, t.nodeID, token, workers)
				ch <- drillResult{parent: t.parent, children: nodes, task: t, err: err}
			}(task)
		}

		go func() {
			wg.Wait()
			close(ch)
		}()

		var nextDrills []drillTask
		for res := range ch {
			if res.err != nil {
				log.Printf("    [Warn] 节点 %s 钻取失败: %v", res.task.nodeID, res.err)
				continue
			}
			for _, rn := range res.children {
				node := &Node{
					DimCode:  rn.dimCode,
					NodeName: rn.nodeName,
					IsLeaf:   rn.isLeaf,
					Children: make(map[string]*Node),
				}
				res.parent[rn.dimCode] = node
				if !rn.isLeaf && res.task.depth+1 < maxDepth {
					nextDrills = append(nextDrills, drillTask{parent: node.Children, nodeID: rn.nodeID, depth: res.task.depth + 1})
				}
			}
		}

		pendingDrills = nextDrills
	}

	return tree
}

// --- EkbFetcher 是 Fetcher 接口的合思实现 ---

// EkbFetcher 通过合思 API 拉取预算数据
type EkbFetcher struct {
	Host    string
	AppKey  string
	Secret  string
	token   string
	expiry  time.Time
	client  *http.Client
}

func NewEkbFetcher(host, appKey, secret string) *EkbFetcher {
	return &EkbFetcher{
		Host:   host,
		AppKey:  appKey,
		Secret: secret,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (f *EkbFetcher) GetToken() (string, error) {
	if f.token != "" && time.Now().Before(f.expiry) {
		return f.token, nil
	}
	body, _ := json.Marshal(map[string]string{"appKey": f.AppKey, "appSecurity": f.Secret})
	resp, err := f.client.Post(f.Host+"/api/openapi/v1/auth/getAccessToken", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	val, _ := result["value"].(map[string]interface{})
	token, _ := val["accessToken"].(string)
	f.token = token
	f.expiry = time.Now().Add(110 * time.Minute)
	return token, nil
}

func (f *EkbFetcher) QueryURL(budgetPathID string) string {
	return f.Host + "/api/openapi/v2/budgets/" + budgetPathID + "/query"
}

func (f *EkbFetcher) FetchBudgetList(token string) ([]map[string]interface{}, error) {
	u := f.Host + "/api/openapi/v2/budgets?accessToken=" + url.QueryEscape(token) + "&start=0&count=100"
	resp, err := f.client.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	items, ok := result["items"].([]interface{})
	if !ok {
		return nil, nil
	}
	var budgets []map[string]interface{}
	for _, item := range items {
		if m, ok := item.(map[string]interface{}); ok {
			budgets = append(budgets, m)
		}
	}
	return budgets, nil
}

func (f *EkbFetcher) FetchNodes(queryURL, nodeID, token string, workers int) ([]rawNode, []string, int, error) {
	pageSize := 100

	params := url.Values{
		"accessToken": {token},
		"start":       {"0"},
		"count":       {intToStr(pageSize)},
	}
	if nodeID != "" {
		params.Set("nodeId", nodeID)
	}

	resp, err := f.client.Get(queryURL + "?" + params.Encode())
	if err != nil {
		return nil, nil, 0, err
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()

	val, _ := result["value"].(map[string]interface{})
	nodes, _ := val["nodes"].([]interface{})

	totalCount := 0
	if c, ok := val["count"].(float64); ok && int(c) > 0 {
		totalCount = int(c)
	}

	if len(nodes) == 0 || len(nodes) == 1 {
		return nil, nil, totalCount, nil
	}

	allNodes, nextIDs := parseRawNodes(nodes[1:])

	if len(nodes)-1 < pageSize {
		return allNodes, nextIDs, totalCount, nil
	}

	childCount := totalCount - 1
	if childCount <= 0 {
		childCount = len(nodes) - 1
	}
	totalPages := (childCount + pageSize - 1) / pageSize
	if totalPages <= 1 {
		return allNodes, nextIDs, totalCount, nil
	}

	type pageResult struct {
		nodes    []rawNode
		children []string
		err      error
		page     int
	}

	ch := make(chan pageResult, totalPages-1)
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup

	for page := 1; page < totalPages; page++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			offset := p * pageSize
			pParams := url.Values{
				"accessToken": {token},
				"start":       {intToStr(offset)},
				"count":       {intToStr(pageSize)},
			}
			if nodeID != "" {
				pParams.Set("nodeId", nodeID)
			}

			pResp, pErr := f.client.Get(queryURL + "?" + pParams.Encode())
			if pErr != nil {
				ch <- pageResult{err: pErr, page: p}
				return
			}

			var pResult map[string]interface{}
			json.NewDecoder(pResp.Body).Decode(&pResult)
			pResp.Body.Close()

			pVal, _ := pResult["value"].(map[string]interface{})
			pNodes, _ := pVal["nodes"].([]interface{})

			if len(pNodes) <= 1 {
				ch <- pageResult{page: p}
				return
			}

			rows, children := parseRawNodes(pNodes[1:])
			ch <- pageResult{nodes: rows, children: children, page: p}
		}(page)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	for pr := range ch {
		if pr.err != nil {
			log.Printf("    [Warn] 分页请求失败 page=%d: %v", pr.page, pr.err)
			continue
		}
		allNodes = append(allNodes, pr.nodes...)
		nextIDs = append(nextIDs, pr.children...)
	}

	return allNodes, nextIDs, totalCount, nil
}

func parseRawNodes(nodes []interface{}) ([]rawNode, []string) {
	var result []rawNode
	var nextIDs []string

	for _, n := range nodes {
		node, _ := n.(map[string]interface{})
		nID, _ := node["nodeId"].(string)
		nName, _ := node["name"].(string)
		if nName == "" {
			nName, _ = node["code"].(string)
		}
		isLeaf, _ := node["isLeaf"].(bool)

		if !isLeaf {
			nextIDs = append(nextIDs, nID)
		}

		contents, _ := node["content"].([]interface{})
		for _, c := range contents {
			content, _ := c.(map[string]interface{})
			dimCode, _ := content["contentId"].(string)
			dimID := strings.TrimSpace(fmt.Sprintf("%v", content["dimensionId"]))

			dimType := "UNKNOWN"
			if dimID == "E_system_costcenter" || strings.Contains(strings.ToLower(dimID), "costcenter") {
				dimType = "DEPARTMENT"
			} else if dimID == "u_费用类型档案" || strings.Contains(dimID, "费用类型") {
				dimType = "ARCHIVE"
			} else if dimID == "项目" || strings.Contains(strings.ToLower(dimID), "project") || fmt.Sprintf("%v", content["dimensionType"]) == "PROJECT" {
				dimType = "PROJECT"
			}

			if dimType != "UNKNOWN" && dimCode != "" {
				result = append(result, rawNode{
					nodeID:   nID,
					nodeName: nName,
					dimCode:  dimCode,
					dimType:  dimType,
					isLeaf:   isLeaf,
				})
			}
		}
	}

	return result, nextIDs
}

func intToStr(n int) string {
	return fmt.Sprintf("%d", n)
}