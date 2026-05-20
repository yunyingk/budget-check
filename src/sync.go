package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
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

		currentNodes := []string{""}
		totalNodes := 0

		for level := 0; level < targetDepth; level++ {
			if len(currentNodes) == 0 {
				break
			}
			log.Printf("    [Layer %d] 处理 %d 个节点...", level+1, len(currentNodes))

			type fetchResult struct {
				rows     []DimEntry
				children []string
				err      error
				nodeID   string
				total    int
			}

			ch := make(chan fetchResult, len(currentNodes))
			sem := make(chan struct{}, workers)
			var wg sync.WaitGroup

			for _, nodeID := range currentNodes {
				wg.Add(1)
				go func(nid string) {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()
					rows, children, total, err := fetchNodeChildren(client, token, queryURL, nid)
					ch <- fetchResult{rows: rows, children: children, err: err, nodeID: nid, total: total}
				}(nodeID)
			}

			go func() {
				wg.Wait()
				close(ch)
			}()

			var allRows []DimEntry
			var nextIDs []string
			for res := range ch {
				if res.err != nil {
					log.Printf("    [Warn] 节点 %s 失败: %v", res.nodeID, res.err)
					continue
				}
				if res.total > 0 && totalNodes == 0 {
					log.Printf("    [Layer %d] 预计子节点总量: %d", level+1, res.total-1)
				}
				allRows = append(allRows, res.rows...)
				nextIDs = append(nextIDs, res.children...)
				totalNodes++
			}

			store.BatchPut(allRows)
			currentNodes = nextIDs
		}
		total += totalNodes
		log.Printf("    -> %s 完成", bName)
	}

	log.Printf("[Sync] 同步完成! 节点数: %d, 耗时: %v, 缓存条目: %d", total, time.Since(start), store.Count())
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

func fetchNodeChildren(client *EkbClient, token, queryURL, nodeID string) ([]DimEntry, []string, int, error) {
	var allRows []DimEntry
	var nextIDs []string
	totalCount := 0
	offset := 0
	pageSize := 100

	for {
		params := url.Values{
			"accessToken": {token},
			"start":       {intToStr(offset)},
			"count":       {intToStr(pageSize)},
		}
		if nodeID != "" {
			params.Set("nodeId", nodeID)
		}

		resp, err := client.client.Get(queryURL + "?" + params.Encode())
		if err != nil {
			return allRows, nextIDs, 0, err
		}

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		val, _ := result["value"].(map[string]interface{})
		nodes, _ := val["nodes"].([]interface{})
		if len(nodes) == 0 {
			break
		}

		// 从 API 响应中取 count（节点+子节点总数）
		if c, ok := val["count"].(float64); ok && int(c) > 0 {
			totalCount = int(c)
		}

		// 每页第一条是节点自身，子节点从第二条开始
		if len(nodes) == 1 {
			// 只有自身，没有子节点
			break
		}
		children := nodes[1:]

		for _, ch := range children {
			node, _ := ch.(map[string]interface{})
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
					allRows = append(allRows, DimEntry{
						DimCode:  dimCode,
						DimType:  dimType,
						NodeName: nName,
					})
				}
			}
		}

		// 如果子节点数不足 pageSize，说明没有下一页了
		if len(children) < pageSize {
			break
		}
		offset += pageSize
	}

	return allRows, nextIDs, totalCount, nil
}