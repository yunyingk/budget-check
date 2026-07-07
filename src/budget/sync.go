package budget

import (
	"budget/src/ekb"
	"context"
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
	ID   string
	Name string
}

// SyncConfig 同步配置
type SyncConfig struct {
	Targets        []Target
	Workers        int
	TimeoutMinutes int
}

// Sync 同步所有匹配的预算包，构建树并写入 Store。
// 同步必须完整成功，避免审批消费使用半成品预算树。
func Sync(ctx context.Context, store *Store, client *ekb.Client, cfg SyncConfig) error {
	if cfg.Workers <= 0 {
		cfg.Workers = 10
	}
	start := time.Now()
	log.Printf("[Sync] 开始同步预算数据... (并发数: %d, 目标数: %d, 超时: %d 分钟)", cfg.Workers, len(cfg.Targets), cfg.TimeoutMinutes)

	token, err := client.GetTokenContext(ctx)
	if err != nil {
		log.Printf("[Sync] 获取Token失败: %v", err)
		return err
	}
	log.Printf("[Sync] Token OK")

	// 同步费用类型（全量拉取存内存）
	feeTypeStart := time.Now()
	log.Printf("[Sync] 开始同步费用类型...")
	if err := client.SyncFeeTypesContext(ctx); err != nil {
		log.Printf("[Sync] 同步费用类型失败: %v", err)
		return err
	}
	log.Printf("[Sync] 费用类型同步完成, 耗时: %v", time.Since(feeTypeStart))

	nextStore := NewStore()
	nextStore.ResetSyncProgress()

	budgetListStart := time.Now()
	log.Printf("[Sync] 开始获取预算包列表...")
	budgets, err := fetchBudgetList(ctx, client, token)
	if err != nil {
		log.Printf("[Sync] 获取预算列表失败: %v", err)
		return err
	}
	log.Printf("[Sync] 全量预算包共 %d 个, 耗时: %v", len(budgets), time.Since(budgetListStart))
	budgetIDs := make(map[string]string, len(budgets))
	for _, b := range budgets {
		if err := ctx.Err(); err != nil {
			return err
		}
		bName, _ := b["name"].(string)
		bID, _ := b["id"].(string)
		if bID != "" {
			budgetIDs[bID] = bName
		}
		log.Printf("    [%s] %s", bID, bName)
	}

	for _, t := range cfg.Targets {
		if t.ID == "" {
			continue
		}
		if _, ok := budgetIDs[t.ID]; ok {
			continue
		}
		reason := "配置的预算包 ID 未在合思预算列表中找到"
		nextStore.MarkMissingTarget(MissingTarget{ID: t.ID, Name: t.Name, Reason: reason})
		log.Printf("[Sync] 配置预算包不存在: name=%s id=%s reason=%s", t.Name, t.ID, reason)
	}

	for _, b := range budgets {
		bName, _ := b["name"].(string)
		bID, _ := b["id"].(string)

		matched := false
		for _, t := range cfg.Targets {
			if t.ID != "" {
				if bID == t.ID {
					matched = true
					break
				}
			} else if strings.Contains(bName, t.Name) {
				matched = true
				break
			}
		}
		if !matched {
			log.Printf("    [Skip] 跳过: %s", bName)
			continue
		}

		targetStart := time.Now()
		log.Printf("[Sync] 同步预算包开始: name=%s id=%s", bName, bID)
		if err := buildTree(ctx, nextStore, bID, bName, client, token, cfg.Workers); err != nil {
			log.Printf("    [Error] 同步预算包失败: %s, err=%v", bName, err)
			return err
		}
		log.Printf("[Sync] 同步预算包完成: name=%s id=%s 耗时=%v 当前总条目=%d", bName, bID, time.Since(targetStart), nextStore.Count())
	}

	store.Replace(nextStore)
	log.Printf("[Sync] 同步完成! 耗时: %v, 总维度条目: %d", time.Since(start), store.Count())
	return nil
}

// rawNode 从 API 返回的原始节点数据
type rawNode struct {
	nodeID   string
	nodeName string
	dimCode  string
	dimType  string
	dimId    string // dimensionId = 表单字段名
	isLeaf   bool
}

// buildTree 从根节点开始逐层构建预算包树，边建边往 store 索引里写
func buildTree(ctx context.Context, store *Store, bID, bName string, client *ekb.Client, token string, workers int) error {
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

	rootStart := time.Now()
	log.Printf("    [Root] 开始拉取根节点: budget=%s(%s)", bName, bID)
	rootNodes, _, total, err := fetchNodes(ctx, client, queryURL, "", token, workers)
	if err != nil {
		log.Printf("    [Error] 拉取根节点失败: %v", err)
		return err
	}
	log.Printf("    [Root] 根节点拉取完成: budget=%s(%s) nodes=%d total=%d 耗时=%v", bName, bID, len(rootNodes), total, time.Since(rootStart))
	if total > 0 {
		log.Printf("    根节点子项总量: %d", total-1)
	}

	if len(rootNodes) == 0 {
		return fmt.Errorf("预算包 %s(%s) 未同步到任何节点", bName, bID)
	}

	store.addTreeRef(tree)

	type drillTask struct {
		parent map[string]*Node
		nodeID string
	}

	var pendingDrills []drillTask
	drilled := make(map[string]bool)
	for _, rn := range rootNodes {
		node := &Node{
			DimCode:  rn.dimCode,
			DimType:  rn.dimType,
			DimId:    rn.dimId,
			NodeName: rn.nodeName,
			NodeID:   rn.nodeID,
			IsLeaf:   rn.isLeaf,
			Children: make(map[string]*Node),
		}
		tree.Root[rn.dimCode] = node
		store.indexNode(rn.dimCode, node, tree)
		if !rn.isLeaf && !drilled[rn.nodeID] {
			drilled[rn.nodeID] = true
			pendingDrills = append(pendingDrills, drillTask{parent: node.Children, nodeID: rn.nodeID})
		}
	}
	log.Printf("    [Layer 1] 根节点 %d 个, 当前总条目: %d", len(rootNodes), store.Count())

	layer := 2
	for len(pendingDrills) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		layerStart := time.Now()
		layerTotal := len(pendingDrills)
		log.Printf("    [Layer %d] 钻取开始: budget=%s(%s) nodes=%d 当前总条目=%d", layer, bName, bID, layerTotal, store.Count())

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
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return ctx.Err()
			}
			wg.Add(1)
			go func(t drillTask) {
				defer wg.Done()
				defer func() { <-sem }()
				nodes, _, _, err := fetchNodes(ctx, client, queryURL, t.nodeID, token, workers)
				ch <- drillResult{parent: t.parent, children: nodes, task: t, err: err}
			}(task)
		}

		go func() {
			wg.Wait()
			close(ch)
		}()

		var nextDrills []drillTask
		completed := 0
		ticker := time.NewTicker(30 * time.Second)
		for completed < layerTotal {
			var res drillResult
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				log.Printf("    [Layer %d] 钻取等待中: budget=%s(%s) completed=%d/%d elapsed=%v 当前总条目=%d", layer, bName, bID, completed, layerTotal, time.Since(layerStart).Round(time.Second), store.Count())
				continue
			case r, ok := <-ch:
				if !ok {
					return fmt.Errorf("预算包 %s(%s) 第 %d 层钻取结果通道提前关闭: completed=%d/%d", bName, bID, layer, completed, layerTotal)
				}
				res = r
				completed++
			}
			if res.err != nil {
				log.Printf("    [Warn] 节点 %s 钻取失败: %v", res.task.nodeID, res.err)
				return fmt.Errorf("节点 %s 钻取失败: %w", res.task.nodeID, res.err)
			}
			for _, rn := range res.children {
				node := &Node{
					DimCode:  rn.dimCode,
					DimType:  rn.dimType,
					DimId:    rn.dimId,
					NodeName: rn.nodeName,
					NodeID:   rn.nodeID,
					IsLeaf:   rn.isLeaf,
					Children: make(map[string]*Node),
				}
				res.parent[rn.dimCode] = node
				store.indexNode(rn.dimCode, node, tree)
				if !rn.isLeaf && !drilled[rn.nodeID] {
					drilled[rn.nodeID] = true
					nextDrills = append(nextDrills, drillTask{parent: node.Children, nodeID: rn.nodeID})
				}
			}
		}
		ticker.Stop()

		log.Printf("    [Layer %d] 钻取完成: budget=%s(%s) completed=%d/%d next=%d 耗时=%v 当前总条目=%d", layer, bName, bID, completed, layerTotal, len(nextDrills), time.Since(layerStart), store.Count())
		pendingDrills = nextDrills
		layer++
	}
	return nil
}

// fetchBudgetList 获取预算包列表
func fetchBudgetList(ctx context.Context, client *ekb.Client, token string) ([]map[string]interface{}, error) {
	u := client.HostURL("/api/openapi/v2/budgets?start=0&count=100")
	resp, err := client.GetWithTokenContext(ctx, u, token)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析预算列表响应失败: %w", err)
	}
	items, ok := result["items"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("预算列表响应格式错误: 缺少 items 字段")
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
func fetchNodes(ctx context.Context, client *ekb.Client, queryURL, nodeID, token string, workers int) ([]rawNode, []string, int, error) {
	pageSize := 100

	params := url.Values{
		"accessToken": {token},
		"start":       {"0"},
		"count":       {intToStr(pageSize)},
	}
	if nodeID != "" {
		params.Set("nodeId", nodeID)
	}

	resp, err := client.GetWithTokenContext(ctx, queryURL+"?"+params.Encode(), token)
	if err != nil {
		return nil, nil, 0, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, nil, 0, fmt.Errorf("解析节点响应失败: %w", err)
	}

	val, _ := result["value"].(map[string]interface{})
	nodes, _ := val["nodes"].([]interface{})

	totalCount := 0
	if c, ok := val["count"].(float64); ok && int(c) > 0 {
		totalCount = int(c)
	}

	if len(nodes) == 0 || len(nodes) == 1 {
		return nil, nil, totalCount, nil
	}

	allNodes, nextIDs, err := parseRawNodes(nodes[1:])
	if err != nil {
		return nil, nil, totalCount, err
	}

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
	pageFetchStart := time.Now()
	log.Printf("    [Page] 分页拉取开始: nodeID=%s pages=%d childCount=%d", blankAsRoot(nodeID), totalPages, childCount)

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
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return nil, nil, totalCount, ctx.Err()
		}
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

			pResp, pErr := client.GetWithTokenContext(ctx, queryURL+"?"+pParams.Encode(), token)
			if pErr != nil {
				ch <- pageResult{err: pErr, page: p}
				return
			}
			defer pResp.Body.Close()

			var pResult map[string]interface{}
			if err := json.NewDecoder(pResp.Body).Decode(&pResult); err != nil {
				ch <- pageResult{err: fmt.Errorf("解析分页响应失败: %w", err), page: p}
				return
			}

			pVal, _ := pResult["value"].(map[string]interface{})
			pNodes, _ := pVal["nodes"].([]interface{})

			if len(pNodes) <= 1 {
				ch <- pageResult{page: p}
				return
			}

			rows, children, err := parseRawNodes(pNodes[1:])
			if err != nil {
				ch <- pageResult{err: err, page: p}
				return
			}
			ch <- pageResult{nodes: rows, children: children, page: p}
		}(page)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	completedPages := 0
	ticker := time.NewTicker(30 * time.Second)
	for completedPages < totalPages-1 {
		var pr pageResult
		select {
		case <-ctx.Done():
			return nil, nil, totalCount, ctx.Err()
		case <-ticker.C:
			log.Printf("    [Page] 分页拉取等待中: nodeID=%s completed=%d/%d elapsed=%v", blankAsRoot(nodeID), completedPages, totalPages-1, time.Since(pageFetchStart).Round(time.Second))
			continue
		case r, ok := <-ch:
			if !ok {
				return nil, nil, totalCount, fmt.Errorf("分页结果通道提前关闭: nodeID=%s completed=%d/%d", blankAsRoot(nodeID), completedPages, totalPages-1)
			}
			pr = r
			completedPages++
		}
		if pr.err != nil {
			log.Printf("    [Warn] 分页请求失败 page=%d: %v", pr.page, pr.err)
			return nil, nil, totalCount, fmt.Errorf("分页请求失败 page=%d: %w", pr.page, pr.err)
		}
		allNodes = append(allNodes, pr.nodes...)
		nextIDs = append(nextIDs, pr.children...)
	}
	ticker.Stop()
	log.Printf("    [Page] 分页拉取完成: nodeID=%s pages=%d 耗时=%v", blankAsRoot(nodeID), totalPages, time.Since(pageFetchStart))

	return allNodes, nextIDs, totalCount, nil
}

func blankAsRoot(s string) string {
	if s == "" {
		return "<root>"
	}
	return s
}

func parseRawNodes(nodes []interface{}) ([]rawNode, []string, error) {
	var result []rawNode
	var nextIDs []string

	for _, n := range nodes {
		node, ok := n.(map[string]interface{})
		if !ok {
			return nil, nil, fmt.Errorf("预算节点格式错误: %v", n)
		}
		nID, _ := node["nodeId"].(string)
		if nID == "" {
			return nil, nil, fmt.Errorf("预算节点缺少 nodeId: %v", node)
		}
		nName, _ := node["name"].(string)
		if nName == "" {
			nName, _ = node["code"].(string)
		}
		if nName == "" {
			return nil, nil, fmt.Errorf("预算节点缺少 name/code: nodeId=%s", nID)
		}
		isLeaf, ok := node["isLeaf"].(bool)
		if !ok {
			return nil, nil, fmt.Errorf("预算节点缺少 isLeaf: nodeId=%s name=%s", nID, nName)
		}

		if !isLeaf {
			nextIDs = append(nextIDs, nID)
		}

		contents, ok := node["content"].([]interface{})
		if !ok || len(contents) == 0 {
			return nil, nil, fmt.Errorf("预算节点缺少 content: nodeId=%s name=%s", nID, nName)
		}
		for _, c := range contents {
			content, ok := c.(map[string]interface{})
			if !ok {
				return nil, nil, fmt.Errorf("预算节点 content 格式错误: nodeId=%s name=%s content=%v", nID, nName, c)
			}
			dimCode, _ := content["contentId"].(string)
			if dimCode == "" {
				return nil, nil, fmt.Errorf("预算节点缺少 contentId: nodeId=%s name=%s", nID, nName)
			}
			dimType, _ := content["dimensionType"].(string)
			if dimType == "" {
				return nil, nil, fmt.Errorf("预算节点缺少 dimensionType: nodeId=%s name=%s contentId=%s", nID, nName, dimCode)
			}
			dimId, _ := content["dimensionId"].(string)
			if dimId == "" {
				return nil, nil, fmt.Errorf("预算节点缺少 dimensionId: nodeId=%s name=%s contentId=%s dimensionType=%s", nID, nName, dimCode, dimType)
			}
			result = append(result, rawNode{
				nodeID:   nID,
				nodeName: nName,
				dimCode:  dimCode,
				dimType:  dimType,
				dimId:    dimId,
				isLeaf:   isLeaf,
			})
		}
	}

	return result, nextIDs, nil
}

func intToStr(n int) string {
	return fmt.Sprintf("%d", n)
}
