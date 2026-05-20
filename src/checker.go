package main

import (
	"fmt"
	"log"
	"strings"
)

type CheckResult struct {
	Pass   bool   `json:"pass"`
	Reason string `json:"reason"`
}

func CheckBudget(info *TicketInfo, store *Store, client *EkbClient) CheckResult {
	log.Printf("[Check] 性质=%s 成本中心=%s 费用档案=%s 项目=%s",
		info.ExpenseType, info.DeptCode, info.ArchiveCode, info.ProjectCode)

	okDept, _ := existsWithTraceback(info.DeptCode, "成本中心", store, client)
	okArchive, _ := existsWithTraceback(info.ArchiveCode, "费用档案", store, client)

	okProj := false
	isPublicCost := false

	if info.ProjectCode != "" {
		if _, ok := store.Get(info.ProjectCode); ok {
			okProj = true
			log.Printf("    [Match] 项目匹配成功")
		} else {
			pInfo, err := client.GetDimInfo(info.ProjectCode)
			if err == nil && pInfo != nil {
				name, _ := pInfo["name"].(string)
				if strings.Contains(name, "公摊成本") {
					isPublicCost = true
					okProj = true
					log.Printf("    [Special] 特例项目: %s，豁免校验", name)
				} else {
					log.Printf("    [Fail] 项目 (%s) 不在预算内且非特例", info.ProjectCode)
				}
			}
		}
	} else {
		log.Printf("    [Info] 单据无项目信息")
	}

	return judge(info.ExpenseType, okDept, okArchive, okProj, isPublicCost, info.RawNatureID)
}

func existsWithTraceback(dimCode, label string, store *Store, client *EkbClient) (bool, string) {
	if dimCode == "" {
		return false, ""
	}

	currentID := dimCode
	for i := 0; i < 5; i++ {
		if e, ok := store.Get(currentID); ok {
			log.Printf("    [Match] %s匹配成功: %s (Type: %s)", label, e.NodeName, e.DimType)
			return true, e.NodeName
		}
		log.Printf("    [Trace] %s ID %s 未命中，尝试查父级...", label, currentID)

		pid, err := client.GetParentID(currentID)
		if err != nil || pid == "" || pid == currentID {
			break
		}
		currentID = pid
	}

	log.Printf("    [Fail] %s (%s) 最终未在预算库中找到", label, dimCode)
	return false, ""
}

func judge(expenseType string, okDept, okArchive, okProj, isPublicCost bool, rawNatureID string) CheckResult {
	basicOK := okDept && okArchive
	var basicFails []string
	if !okDept {
		basicFails = append(basicFails, "成本中心不在预算内")
	}
	if !okArchive {
		basicFails = append(basicFails, "费用档案不在预算内")
	}

	switch expenseType {
	case "业务", "管理":
		if basicOK {
			return CheckResult{Pass: true, Reason: "通过"}
		}
		return CheckResult{Pass: false, Reason: strings.Join(basicFails, "、")}

	case "生产":
		if isPublicCost {
			if basicOK {
				return CheckResult{Pass: true, Reason: "通过(公摊豁免)"}
			}
			return CheckResult{Pass: false, Reason: strings.Join(basicFails, "、")}
		}
		if basicOK && okProj {
			return CheckResult{Pass: true, Reason: "通过"}
		}
		var fails []string
		fails = append(fails, basicFails...)
		if !okProj {
			fails = append(fails, "项目不在预算内")
		}
		return CheckResult{Pass: false, Reason: strings.Join(fails, "、")}

	default:
		return CheckResult{Pass: false, Reason: fmt.Sprintf("未配置性质ID: %s", rawNatureID)}
	}
}