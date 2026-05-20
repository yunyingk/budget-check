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
)

// Checker 校验器，持有共享依赖
type Checker struct {
	Client        *ekb.Client
	Store         *budget.Store
	SignKey       string
	ExpenseNature map[string]string // 费用性质 ID → 中文名
}

// NewChecker 创建校验器
func NewChecker(client *ekb.Client, store *budget.Store, signKey string, expenseNature map[string]string) *Checker {
	return &Checker{
		Client:        client,
		Store:         store,
		SignKey:       signKey,
		ExpenseNature: expenseNature,
	}
}

// Process 处理校验任务
func (c *Checker) Process(task types.Task) {
	log.Printf("[Consumer] 开始处理: taskID=%s code=%s", task.ID, task.Code)

	// 1. 获取单据详情
	form, details, err := c.fetchFlowData(task.Code)
	if err != nil {
		log.Printf("[Consumer] 获取单据失败: %v", err)
		return
	}
	if form == nil {
		log.Printf("[Consumer] 单据 %s 未找到", task.Code)
		return
	}

	// 2. 提取表头字段
	natureID, _ := form["u_费用性质"].(string)
	costCenter, _ := form["E_system_costcenter"].(string)
	project, _ := form["项目"].(string)

	natureName := c.ExpenseNature[natureID]
	log.Printf("[Consumer] 单据 %s: 费用性质=%s(%s) 成本中心=%s 项目=%s 明细数=%d",
		task.Code, natureID, natureName, costCenter, project, len(details))

	// 3. 按费用性质分支校验
	var action string
	var comment string

	switch natureName {
	case "业务", "管理":
		action, comment = c.checkBusinessOrManage(costCenter, details)
	case "生产":
		action, comment = c.checkProduction(costCenter, project)
	default:
		action = "refuse"
		comment = fmt.Sprintf("未配置的性质ID: %s", natureID)
	}

	log.Printf("[Consumer] 校验结果: taskID=%s action=%s comment=%s", task.ID, action, comment)

	// 4. 回调审批
	if err := c.callbackApproval(task.FlowID, task.NodeID, action, comment); err != nil {
		log.Printf("[Consumer] 回调审批失败: %v", err)
		return
	}

	log.Printf("[Consumer] 处理完成: taskID=%s", task.ID)
}

// checkBusinessOrManage 业务/管理费用：查成本中心预算包
// 三层结构：成本中心 → 预算管控(跳过) → 费用档案
func (c *Checker) checkBusinessOrManage(costCenter string, details []map[string]interface{}) (string, string) {
	if costCenter == "" {
		return "refuse", "缺少成本中心"
	}

	tree := c.Store.GetTreeByName("2026成本中心预算")
	if tree == nil {
		log.Printf("[Consumer] 成本中心预算包未找到")
		return "refuse", "成本中心预算包未同步"
	}

	// 第一层：查成本中心
	ccNode, ok := tree.Root[costCenter]
	if !ok {
		return "refuse", "成本中心不在预算内"
	}

	// 第二层：预算管控（只有一个子节点，跳过，直接取它的 children）
	var feeTypeBudget map[string]*budget.Node
	for _, child := range ccNode.Children {
		feeTypeBudget = child.Children // 预算管控下的费用档案
		break
	}

	// 第三层：遍历明细，查每条的费用档案
	if len(details) == 0 {
		return "accept", "成本中心在预算内"
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

		// 在预算管控的子节点中查找费用档案
		if feeTypeBudget != nil {
			if _, ok := feeTypeBudget[feeType]; ok {
				continue // 找到，通过
			}
		}
		missing = append(missing, fmt.Sprintf("明细%d费用档案(%s)", i+1, feeType))
	}

	if len(missing) > 0 {
		return "refuse", fmt.Sprintf("费用档案不在预算内: %s", joinStrings(missing, "、"))
	}

	return "accept", "成本中心+费用档案在预算内"
}

// checkProduction 生产费用：先查项目预算包的项目，再查项目下的成本中心
func (c *Checker) checkProduction(costCenter, project string) (string, string) {
	if project == "" {
		return "refuse", "生产费用缺少项目"
	}

	tree := c.Store.GetTreeByName("项目预算包")
	if tree == nil {
		log.Printf("[Consumer] 项目预算包未同步")
		return "refuse", "项目预算包未同步"
	}

	// 查项目
	projectNode, ok := tree.Root[project]
	if !ok {
		return "refuse", "项目不在预算内"
	}

	// 项目下有成本中心子预算时，必须命中
	if len(projectNode.Children) > 0 {
		if costCenter == "" {
			return "refuse", "项目要求成本中心但未填写"
		}
		if _, ok := projectNode.Children[costCenter]; ok {
			return "accept", "项目+成本中心在预算内"
		}
		return "refuse", "成本中心不在项目预算内"
	}

	// 项目下没有成本中心子预算，只命中项目即可
	return "accept", "项目在预算内"
}

// fetchFlowData 获取单据表单和明细
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

	// 提取明细
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

// callbackApproval 回调审批系统
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
