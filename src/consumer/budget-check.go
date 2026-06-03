package consumer

import (
	"budget/src/budget"
	"budget/src/ekb"
	"budget/src/rules"
	"budget/src/types"
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
	Client     *ekb.Client
	Store      *budget.Store
	SignKey    string
	Engine     *rules.Engine
	History    []HistoryItem
	HistoryMax int
	mu         sync.Mutex
}

func NewChecker(client *ekb.Client, store *budget.Store, signKey string, engine *rules.Engine) *Checker {
	return &Checker{
		Client:     client,
		Store:      store,
		SignKey:    signKey,
		Engine:     engine,
		HistoryMax: 50,
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

type checkUnit struct {
	costCenter string
	project    string
	feeType    string
	label      string
}

func (c *Checker) extractCheckUnits(form map[string]interface{}, details []map[string]interface{}) []checkUnit {
	formCostCenter, _ := form["E_system_costcenter"].(string)
	formProject, _ := form["项目"].(string)

	if len(details) == 0 {
		return []checkUnit{{
			costCenter: formCostCenter,
			project:    formProject,
			feeType:    "",
			label:      "单据",
		}}
	}

	var units []checkUnit
	for i, detail := range details {
		feeTypeForm, _ := detail["feeTypeForm"].(map[string]interface{})
		feeType := ""
		if feeTypeForm != nil {
			feeType, _ = feeTypeForm["u_费用类型档案"].(string)
		}

		var rawApportions []interface{}
		if feeTypeForm != nil {
			rawApportions, _ = feeTypeForm["apportions"].([]interface{})
		}

		if len(rawApportions) > 0 {
			for j, a := range rawApportions {
				apportion, _ := a.(map[string]interface{})
				apportionForm, _ := apportion["apportionForm"].(map[string]interface{})

				cc := formCostCenter
				proj := formProject
				if apportionForm != nil {
					if v, ok := apportionForm["E_system_costcenter"].(string); ok && v != "" {
						cc = v
					}
					if v, ok := apportionForm["项目"].(string); ok && v != "" {
						proj = v
					}
				}

				units = append(units, checkUnit{
					costCenter: cc,
					project:    proj,
					feeType:    feeType,
					label:      fmt.Sprintf("明细%d分摊%d", i+1, j+1),
				})
			}
		} else {
			units = append(units, checkUnit{
				costCenter: formCostCenter,
				project:    formProject,
				feeType:    feeType,
				label:      fmt.Sprintf("明细%d", i+1),
			})
		}
	}

	return units
}

// Evaluate 执行预算校验逻辑，返回 action 和 comment（不调用回调）
func (c *Checker) Evaluate(task types.Task) (string, string) {
	log.Printf("[Consumer] 开始处理: taskID=%s code=%s", task.ID, task.Code)

	form, details, err := c.fetchFlowData(task.Code)
	if err != nil {
		log.Printf("[Consumer] 获取单据失败: %v", err)
		return "refuse", fmt.Sprintf("系统错误：获取单据失败: %v", err)
	}
	if form == nil {
		log.Printf("[Consumer] 单据 %s 未找到", task.Code)
		return "refuse", "系统错误：单据未找到"
	}

	natureID, _ := form["u_费用性质"].(string)
	log.Printf("[Consumer] 单据 %s: 费用性质=%s", task.Code, natureID)

	if c.Engine != nil {
		return c.Engine.Evaluate(form, details)
	}

	return "refuse", "规则引擎未配置"
}

func (c *Checker) Process(task types.Task) {
	action, comment := c.Evaluate(task)
	log.Printf("[Consumer] 校验结果: taskID=%s action=%s comment=%s", task.ID, action, comment)
	c.AddHistory(task.Code, action, comment)

	if err := c.callbackApproval(task.FlowID, task.NodeID, action, comment); err != nil {
		log.Printf("[Consumer] 回调审批失败: %v", err)
		return
	}

	log.Printf("[Consumer] 处理完成: taskID=%s", task.ID)
}

func (c *Checker) checkBusinessUnit(unit checkUnit) (string, string) {
	if unit.costCenter == "" {
		return "refuse", fmt.Sprintf("%s 缺少成本中心", unit.label)
	}

	// 从 store 中查找成本中心预算包（通过名称匹配）
	tree := c.Store.GetTreeByName("成本中心预算")
	if tree == nil {
		return "refuse", "成本中心预算包未同步"
	}

	rootSet := make(map[string]bool, len(tree.Root))
	for k := range tree.Root {
		rootSet[k] = true
	}

	foundID, found := c.Client.FindAncestorInTree(unit.costCenter, rootSet, 5)
	if !found {
		ccDim, _ := c.Client.GetDimension(unit.costCenter)
		ccName := unit.costCenter
		if ccDim != nil {
			ccName = ccDim.Name
		}
		return "refuse", fmt.Sprintf("%s 成本中心 %s(%s) 不在成本中心预算包内", unit.label, ccName, unit.costCenter)
	}

	if unit.feeType == "" {
		return "accept", ""
	}

	ccNode := tree.Root[foundID]
	feeTypeBudget := make(map[string]*budget.Node)
	for _, child := range ccNode.Children {
		for k, v := range child.Children {
			feeTypeBudget[k] = v
		}
	}

	if len(feeTypeBudget) > 0 {
		ftSet := make(map[string]bool, len(feeTypeBudget))
		for k := range feeTypeBudget {
			ftSet[k] = true
		}
		_, ftFound := c.Client.FindAncestorInTree(unit.feeType, ftSet, 5)
		if ftFound {
			return "accept", ""
		}
	}

	ftDim, _ := c.Client.GetDimension(unit.feeType)
	ftName := unit.feeType
	if ftDim != nil {
		ftName = ftDim.Name
	}
	return "refuse", fmt.Sprintf("%s 费用类型 %s(%s) 不在成本中心预算包内", unit.label, ftName, unit.feeType)
}

func (c *Checker) checkProductionUnit(unit checkUnit) (string, string) {
	if unit.project == "" {
		return "refuse", fmt.Sprintf("%s 生产费用缺少项目", unit.label)
	}

	// 从 store 中查找项目预算包（通过名称匹配）
	tree := c.Store.GetTreeByName("项目预算")
	if tree == nil {
		return "refuse", "项目预算包未同步"
	}

	projectSet := make(map[string]bool, len(tree.Root))
	for k := range tree.Root {
		projectSet[k] = true
	}
	foundProjectID, found := c.Client.FindAncestorInTree(unit.project, projectSet, 5)
	if !found {
		pjDim, _ := c.Client.GetDimension(unit.project)
		pjName := unit.project
		if pjDim != nil {
			pjName = pjDim.Name
		}
		return "refuse", fmt.Sprintf("%s 项目 %s(%s) 不在项目预算包内", unit.label, pjName, unit.project)
	}

	projectNode := tree.Root[foundProjectID]
	if len(projectNode.Children) > 0 {
		if unit.costCenter == "" {
			return "refuse", fmt.Sprintf("%s 项目要求成本中心但未填写", unit.label)
		}
		ccSet := make(map[string]bool, len(projectNode.Children))
		for k := range projectNode.Children {
			ccSet[k] = true
		}
		_, ccFound := c.Client.FindAncestorInTree(unit.costCenter, ccSet, 5)
		if !ccFound {
			ccDim, _ := c.Client.GetDimension(unit.costCenter)
			ccName := unit.costCenter
			if ccDim != nil {
				ccName = ccDim.Name
			}
			return "refuse", fmt.Sprintf("%s 成本中心 %s(%s) 不在项目预算包内", unit.label, ccName, unit.costCenter)
		}
	}

	action, comment := c.checkBusinessUnit(unit)
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
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, nil, fmt.Errorf("解析响应失败: %w", err)
	}

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

func (c *Checker) CallbackApproval(flowID, nodeID, action, comment string) error {
	return c.callbackApproval(flowID, nodeID, action, comment)
}

func (c *Checker) callbackApproval(flowID, nodeID, action, comment string) error {
	body, _ := json.Marshal(map[string]string{
		"signKey": c.SignKey,
		"flowId":  flowID,
		"nodeId":  nodeID,
		"action":  action,
		"comment": comment,
	})

	url := c.Client.HostURL("/api/openapi/v1/approval")
	resp, err := c.Client.Post(url, body)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}

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
