package budget

import (
	"budget/src/ekb"
	"encoding/json"
	"fmt"
	"log"
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
func Sync(store *Store, client *ekb.Client, cfg SyncConfig) {
	start := time.Now()
	log.Printf("[Sync] 开始同步预算数据... (并发数: %d)", cfg.Workers)

	token, err := client.GetToken()
	if err != nil {
		log.Printf("[Sync] 获取Token失败: %v", err)
		return
	}
	log.Printf("[Sync] Token OK")

	store.Clear()
	store.ResetSyncProgress()

	budgets, err := fetchBudgetList(client, token)
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
		buildTree(store, bID, bName, client, token, cfg.Workers, targetDepth)
		log.Printf("    -> %s 完成, 当前总条目: %d", bName, store.Count())
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

// buildTree 从根节点开始逐层构建预算包树，边建边往 store 索引里写
func buildTree(store *Store, bID, bName string, client *ekb.Client, token string, workers, maxDepth int) {
	tree := &Tree{
		ID:   bID,
		Name: bName,
		Root: make(map[string]*Node),
	}

	realID := bID
	if !strings.HasPrefix(realID, "$") {
		realID = "$" + realID
	}
	queryURL := client.HostURL("/api/openapi/v2/budgets/" + realID + "/query")

	rootNodes, _, total, err := fetchNodes(client, queryURL, "", token, workers)
	if err != nil {
		log.Printf("    [Error] 拉取根节点失败: %v", err)
		return
	}
	if total > 0 {
		log.Printf("    根节点子项总量: %d", total-1)
	}

	if len(rootNodes) == 0 {
		return
	}

	tree.MaxDepth = maxDepth
	store.addTreeRef(tree)

	type drillTask struct {
		parent map[string]*Node
		nodeID string
		depth  int
	}

	var pendingDrills []drillTask
	for _, rn := range rootNodes {
		node := &Node{
			DimCode:  rn.dimCode,
			DimType:  rn.dimType,
			NodeName: rn.nodeName,
			IsLeaf:   rn.isLeaf,
			Children: make(map[string]*Node),
		}
		tree.Root[rn.dimCode] = node
		store.indexNode(rn.dimCode, node, tree)
		if !rn.isLeaf && maxDepth > 1 {
			pendingDrills = append(pendingDrills, drillTask{parent: node.Children, nodeID: rn.nodeID, depth: 1})
		}
	}
	log.Printf("    [Layer 1] 根节点 %d 个, 当前总条目: %d", len(rootNodes), store.Count())

	for depth := 1; depth < maxDepth; depth++ {
		if len(pendingDrills) == 0 {
			break
		}
		log.Printf("    [Layer %d] 钻取 %d 个节点... 当前总条目: %d", depth+1, len(pendingDrills), store.Count())

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
			sem <- struct{}{}
			wg.Add(1)
			go func(t drillTask) {
				defer wg.Done()
				defer func() { <-sem }()
				nodes, _, _, err := fetchNodes(client, queryURL, t.nodeID, token, workers)
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
					DimType:  rn.dimType,
					NodeName: rn.nodeName,
					IsLeaf:   rn.isLeaf,
					Children: make(map[string]*Node),
				}
				res.parent[rn.dimCode] = node
				store.indexNode(rn.dimCode, node, tree)
				if !rn.isLeaf && res.task.depth+1 < maxDepth {
					nextDrills = append(nextDrills, drillTask{parent: node.Children, nodeID: rn.nodeID, depth: res.task.depth + 1})
				}
			}
		}

		pendingDrills = nextDrills
	}
}

// fetchBudgetList 获取预算包列表
func fetchBudgetList(client *ekb.Client, token string) ([]map[string]interface{}, error) {
	u := client.HostURL("/api/openapi/v2/budgets?start=0&count=100")
	resp, err := client.GetWithToken(u, token)
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

// fetchNodes 拉取预算节点（支持分页并发）
func fetchNodes(client *ekb.Client, queryURL, nodeID, token string, workers int) ([]rawNode, []string, int, error) {
	pageSize := 100

	params := url.Values{
		"accessToken": {token},
		"start":       {"0"},
		"count":       {intToStr(pageSize)},
	}
	if nodeID != "" {
		params.Set("nodeId", nodeID)
	}

	resp, err := client.GetWithToken(queryURL+"?"+params.Encode(), token)
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
		sem <- struct{}{}
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
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

			pResp, pErr := client.GetWithToken(queryURL+"?"+pParams.Encode(), token)
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
			dimType, _ := content["dimensionType"].(string)
			if dimCode != "" {
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
