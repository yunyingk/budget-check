package consumer

import (
	"budget/src/budget"
	"budget/src/ekb"
	"budget/src/types"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

var ExpenseNature = map[string]string{
	"ID01LPD78hZRsr": "业务",
	"ID01LPDisfN3qv": "管理",
	"ID01LPDfjPcnyn": "生产",
}

type HistoryItem struct {
	Time    string `json:"time"`
	Code    string `json:"code"`
	Action  string `json:"action"`
	Comment string `json:"comment"`
}

type Checker struct {
	Client         *ekb.Client
	Store          *budget.Store
	SignKey        string
	ExemptProjects map[string]bool
	CostCenterID   string
	ProjectID      string
	History        []HistoryItem
	HistoryMax     int
	mu             sync.Mutex
}

func NewChecker(client *ekb.Client, store *budget.Store, signKey string, exemptProjects []string, costCenterID, projectID string) *Checker {
	exempt := make(map[string]bool, len(exemptProjects))
	for _, id := range exemptProjects {
		exempt[id] = true
	}
	return &Checker{
		Client:         client,
		Store:          store,
		SignKey:        signKey,
		ExemptProjects: exempt,
		CostCenterID:   costCenterID,
		ProjectID:      projectID,
		HistoryMax:     50,
	}
}

func (c *Checker) AddHistory(code, action, comment string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.History = append([]HistoryItem{{Time: time.Now().Format("15:04:05"), Code: code, Action: action, Comment: comment}}, c.History...)
	if len(c.History) > c.HistoryMax {
		c.History = c.History[:c.HistoryMax]
	}
}

func (c *Checker) GetHistory() []HistoryItem {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]HistoryItem, len(c.History))
	copy(result, c.History)
	return result
}

func (c *Checker) Process(task types.Task) {
	log.Printf("[Consumer] 开始处理: taskID=%s code=%s", task.ID, task.Code)

	form, details, err := c.fetchFlowData(task.Code)
	if err != nil {
		log.Printf("[Consumer] 获取单据失败: %v", err)
		c.AddHistory(task.Code, "error", fmt.Sprintf("获取单据失败: %v", err))
		return
	}
	if form == nil {
		log.Printf("[Consumer] 单据 %s 未找到", task.Code)
		c.AddHistory(task.Code, "error", "单据未找到")
		return
	}

	natureID, _ := form["u_费用性质"].(string)
	costCenter, _ := form["E_system_costcenter"].(string)
	project, _ := form["项目"].(string)
	natureName := ExpenseNature[natureID]

	log.Printf("[Consumer] 单据 %s: 费用性质=%s(%s) 成本中心=%s 项目=%s 明细数=%d",
		task.Code, natureID, natureName, costCenter, project, len(details))

	var action, comment string
	switch natureName {
	case "业务", "管理":
		action, comment = c.checkBusinessOrManage(costCenter, details)
	case "生产":
		action, comment = c.checkProduction(costCenter, project, details)
	default:
		action = "refuse"
		comment = fmt.Sprintf("未配置的性质ID: %s", natureID)
	}

	log.Printf("[Consumer] 校验结果: taskID=%s action=%s comment=%s", task.ID, action, comment)
	c.AddHistory(task.Code, action, comment)

	if err := c.callbackApproval(task.FlowID, task.NodeID, action, comment); err != nil {
		log.Printf("[Consumer] 回调审批失败: %v", err)
		return
	}

	log.Printf("[Consumer] 处理完成: taskID=%s", task.ID)
}

func (c *Checker) checkBusinessOrManage(costCenter string, details []map[string]interface{}) (string, string) {
	if costCenter == "" {
		return "refuse", "缺少成本中心"
	}

	tree := c.Store.GetTreeByID(c.CostCenterID)
	if tree == nil {
		return "refuse", "成本中心预算包未同步"
	}

	rootSet := make(map[string]bool, len(tree.Root))
	for k := range tree.Root {
		rootSet[k] = true
	}

	foundID, found := c.Client.FindAncestorInTree(costCenter, rootSet, 5)
	if !found {
		ccDim, _ := c.Client.GetDimension(costCenter)
		ccName := costCenter
		if ccDim != nil {
			ccName = ccDim.Name
		}
		return "refuse", fmt.Sprintf("成本中心 %s(%s) 不在成本中心预算包内", ccName, costCenter)
	}

	if len(details) == 0 {
		return "accept", "同意"
	}

	ccNode := tree.Root[foundID]
	feeTypeBudget := make(map[string]*budget.Node)
	for _, child := range ccNode.Children {
		for k, v := range child.Children {
			feeTypeBudget[k] = v
		}
	}

	var missing []string
	for i, detail := range details {
		feeTypeForm, _ := detail["feeTypeForm"].(map[string]interface{})
		if feeTypeForm == nil {
			continue
		}
		feeType, _ := feeTypeForm["u_费用类型档案"].(string)
		if feeType == "" {
			continue
		}
		if feeTypeBudget != nil {
			ftSet := make(map[string]bool, len(feeTypeBudget))
			for k := range feeTypeBudget {
				ftSet[k] = true
			}
			_, found := c.Client.FindAncestorInTree(feeType, ftSet, 5)
			if found {
				continue
			}
		}
		ftDim, _ := c.Client.GetDimension(feeType)
		ftName := feeType
		if ftDim != nil {
			ftName = ftDim.Name
		}
		missing = append(missing, fmt.Sprintf("明细%d %s(%s)", i+1, ftName, feeType))
	}

	if len(missing) > 0 {
		return "refuse", fmt.Sprintf("%s 不在成本中心预算包内", joinStrings(missing, "、"))
	}

	return "accept", "同意"
}

func (c *Checker) checkProduction(costCenter, project string, details []map[string]interface{}) (string, string) {
	if project == "" {
		return "refuse", "生产费用缺少项目"
	}

	if c.ExemptProjects[project] {
		return c.checkBusinessOrManage(costCenter, details)
	}

	tree := c.Store.GetTreeByID(c.ProjectID)
	if tree == nil {
		return "refuse", "项目预算包未同步"
	}

	projectSet := make(map[string]bool, len(tree.Root))
	for k := range tree.Root {
		projectSet[k] = true
	}
	foundProjectID, found := c.Client.FindAncestorInTree(project, projectSet, 5)
	if !found {
		pjDim, _ := c.Client.GetDimension(project)
		pjName := project
		if pjDim != nil {
			pjName = pjDim.Name
		}
		return "refuse", fmt.Sprintf("项目 %s(%s) 不在项目预算包内", pjName, project)
	}

	projectNode := tree.Root[foundProjectID]
	if len(projectNode.Children) > 0 {
		if costCenter == "" {
			return "refuse", "项目要求成本中心但未填写"
		}
		ccSet := make(map[string]bool, len(projectNode.Children))
		for k := range projectNode.Children {
			ccSet[k] = true
		}
		_, ccFound := c.Client.FindAncestorInTree(costCenter, ccSet, 5)
		if !ccFound {
			ccDim, _ := c.Client.GetDimension(costCenter)
			ccName := costCenter
			if ccDim != nil {
				ccName = ccDim.Name
			}
			return "refuse", fmt.Sprintf("成本中心 %s(%s) 不在项目预算包内", ccName, costCenter)
		}
	}

	action, comment := c.checkBusinessOrManage(costCenter, details)
	if action == "refuse" {
		return action, comment
	}

	return "accept", "同意"
}

func (c *Checker) fetchFlowData(code string) (map[string]interface{}, []map[string]interface{}, error) {
	u := c.Client.HostURL("/api/openapi/v1.1/flowDetails/byCode?code=" + code)
	resp, err := c.Client.Get(u)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	val, _ := result["value"].(map[string]interface{})
	if val == nil {
		return nil, nil, nil
	}

	form, _ := val["form"].(map[string]interface{})
	if form == nil {
		return nil, nil, nil
	}

	var details []map[string]interface{}
	if rawDetails, ok := form["details"].([]interface{}); ok {
		for _, d := range rawDetails {
			if detail, ok := d.(map[string]interface{}); ok {
				details = append(details, detail)
			}
		}
	}

	return form, details, nil
}

func (c *Checker) callbackApproval(flowID, nodeID, action, comment string) error {
	token, err := c.Client.GetToken()
	if err != nil {
		return fmt.Errorf("获取token失败: %w", err)
	}

	body, _ := json.Marshal(map[string]string{
		"signKey": c.SignKey,
		"flowId":  flowID,
		"nodeId":  nodeID,
		"action":  action,
		"comment": comment,
	})

	url := c.Client.HostURL("/api/openapi/v1/approval?accessToken=" + token)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API返回错误: status=%d body=%v", resp.StatusCode, result)
	}

	if val, ok := result["value"].(map[string]interface{}); ok {
		if success, ok := val["success"].(bool); ok && !success {
			return fmt.Errorf("审批回调失败: %v", val)
		}
	}

	log.Printf("[Consumer] 审批回调成功: flowID=%s action=%s", flowID, action)
	return nil
}

func joinStrings(strs []string, sep string) string {
	result := ""
	for i, s := range strs {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}
