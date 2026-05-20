package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"
)

func SyncBudget(store *Store, client *EkbClient, targets []BudgetTarget) {
	start := time.Now()
	log.Printf("[Sync] 开始同步预算数据...")

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
		queryURL := client.host + "/v2/budgets/" + realID + "/query"

		currentNodes := []string{""}
		totalNodes := 0

		for level := 0; level < targetDepth; level++ {
			if len(currentNodes) == 0 {
				break
			}
			log.Printf("    [Layer %d] 处理 %d 个节点...", level+1, len(currentNodes))

			var allRows []DimEntry
			var nextIDs []string

			for _, nodeID := range currentNodes {
				rows, children, err := fetchNodeChildren(client, token, queryURL, nodeID)
				if err != nil {
					log.Printf("    [Warn] 节点 %s 失败: %v", nodeID, err)
					continue
				}
				allRows = append(allRows, rows...)
				nextIDs = append(nextIDs, children...)
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

func fetchNodeChildren(client *EkbClient, token, queryURL, nodeID string) ([]DimEntry, []string, error) {
	var allRows []DimEntry
	var nextIDs []string
	start := 0

	for {
		params := url.Values{
			"accessToken": {token},
			"start":       {intToStr(start)},
			"count":       {"100"},
		}
		if nodeID != "" {
			params.Set("nodeId", nodeID)
		}

		resp, err := client.client.Get(queryURL + "?" + params.Encode())
		if err != nil {
			return allRows, nextIDs, err
		}

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		val, _ := result["value"].(map[string]interface{})
		nodes, _ := val["nodes"].([]interface{})
		if len(nodes) == 0 {
			break
		}

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
					allRows = append(allRows, DimEntry{
						DimCode:  dimCode,
						DimType:  dimType,
						NodeName: nName,
					})
				}
			}
		}

		if len(nodes) < 100 {
			break
		}
		start += 100
	}

	return allRows, nextIDs, nil
}