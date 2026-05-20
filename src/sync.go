package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func SyncBudget(store *Store, client *EkbClient, targets []BudgetTarget, workers int) {
	start := time.Now()
	log.Printf("[Sync] 开始同步预算数据... (并发数: %d)", workers)

	token, err := client.GetToken()
	if err != nil {
		log.Printf("[Sync] 获取Token失败: %v", err)
		return
	}
	log.Printf("[Sync] Token OK")

	store.Clear()

	budgets, err := fetchBudgetList(client, token)
	if err != nil {
		log.Printf("[Sync] 获取预算列表失败: %v", err)
		return
	}

	total := 0
	for _, b := range budgets {
		bName, _ := b["name"].(string)
		bID, _ := b["id"].(string)

		targetDepth := 0
		for _, t := range targets {
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

		realID := bID
		if !strings.HasPrefix(realID, "$") {
			realID = "$" + realID
		}
		queryURL := client.apiURL("/v2/budgets/" + realID + "/query")

		n := syncBudgetTarget(store, client, token, queryURL, targetDepth, workers)
		total += n
		log.Printf("    -> %s 完成, 节点数: %d", bName, n)
	}

	log.Printf("[Sync] 同步完成! 耗时: %v, 缓存条目: %d", time.Since(start), store.Count())
}

type nodeTask struct {
	nodeID string
	depth  int
}

type nodeResult struct {
	rows     []DimEntry
	children []string
	total    int
	err      error
	task     nodeTask
}

func syncBudgetTarget(store *Store, client *EkbClient, token, queryURL string, maxDepth, workers int) int {
	tasks := make(chan nodeTask, 50000)
	results := make(chan nodeResult, 50000)

	var pendingCount int64
	pendingCount++ // root node
	tasks <- nodeTask{nodeID: "", depth: 0}

	// 启动 worker 池
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range tasks {
				rows, children, total, err := fetchNodeChildren(client, token, queryURL, task.nodeID, workers)
				results <- nodeResult{
					rows:     rows,
					children: children,
					total:    total,
					err:      err,
					task:     task,
				}
			}
		}()
	}

	// 关闭 tasks 后等 workers 完成, 再关 results
	go func() {
		wg.Wait()
		close(results)
	}()

	totalNodes := 0
	var allRows []DimEntry
	var mu sync.Mutex

	for res := range results {
		if res.err != nil {
			log.Printf("    [Warn] 节点 %s depth=%d 失败: %v", res.task.nodeID, res.task.depth, res.err)
		} else {
			if res.total > 0 && totalNodes == 0 {
				log.Printf("    根节点子项总量: %d", res.total-1)
			}

			mu.Lock()
			allRows = append(allRows, res.rows...)
			mu.Unlock()

			// 发现子节点且未到最大深度，扔进任务队列
			if res.task.depth < maxDepth-1 {
				for _, childID := range res.children {
					atomic.AddInt64(&pendingCount, 1)
					tasks <- nodeTask{nodeID: childID, depth: res.task.depth + 1}
				}
			}
			totalNodes++
		}

		// 所有任务处理完毕，关闭任务通道
		if atomic.AddInt64(&pendingCount, -1) == 0 {
			close(tasks)
		}
	}

	store.BatchPut(allRows)
	return totalNodes
}

func fetchBudgetList(client *EkbClient, token string) ([]map[string]interface{}, error) {
	resp, err := client.get("/v2/budgets", url.Values{
		"accessToken": {token},
		"start":       {"0"},
		"count":       {"100"},
	})
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

func fetchNodeChildren(client *EkbClient, token, queryURL, nodeID string, workers int) ([]DimEntry, []string, int, error) {
	pageSize := 100

	// 先请求第一页，拿到 count 总数
	params := url.Values{
		"accessToken": {token},
		"start":       {"0"},
		"count":       {intToStr(pageSize)},
	}
	if nodeID != "" {
		params.Set("nodeId", nodeID)
	}

	resp, err := client.client.Get(queryURL + "?" + params.Encode())
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

	allRows, nextIDs := parseNodes(nodes[1:])

	// 只有一页，直接返回
	if len(nodes)-1 < pageSize {
		return allRows, nextIDs, totalCount, nil
	}

	// 并发拉取剩余页
	childCount := totalCount - 1 // 总子节点数（去掉自身）
	if childCount <= 0 {
		childCount = len(nodes) - 1
	}
	totalPages := (childCount + pageSize - 1) / pageSize
	if totalPages <= 1 {
		return allRows, nextIDs, totalCount, nil
	}

	type pageResult struct {
		rows     []DimEntry
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

			pResp, pErr := client.client.Get(queryURL + "?" + pParams.Encode())
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

			rows, children := parseNodes(pNodes[1:])
			ch <- pageResult{rows: rows, children: children, page: p}
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
		allRows = append(allRows, pr.rows...)
		nextIDs = append(nextIDs, pr.children...)
	}

	return allRows, nextIDs, totalCount, nil
}

func parseNodes(nodes []interface{}) ([]DimEntry, []string) {
	var rows []DimEntry
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
				rows = append(rows, DimEntry{
					DimCode:  dimCode,
					DimType:  dimType,
					NodeName: nName,
				})
			}
		}
	}

	return rows, nextIDs
}