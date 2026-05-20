package consumer

import (
	"budget/src/budget"
	"budget/src/ekb"
	"budget/src/types"
	"encoding/json"
	"log"
	"net/http"
)

// Checker 校验器，持有共享依赖
type Checker struct {
	Client *ekb.Client
	Store  *budget.Store
}

// NewChecker 创建校验器
func NewChecker(client *ekb.Client, store *budget.Store) *Checker {
	return &Checker{Client: client, Store: store}
}

// Process 处理校验任务
func (c *Checker) Process(task types.Task) {
	log.Printf("[Consumer] 开始处理: taskID=%s code=%s", task.ID, task.Code)

	// 1. 获取单据详情
	form, err := c.fetchFlowForm(task.Code)
	if err != nil {
		log.Printf("[Consumer] 获取单据失败: %v", err)
		return
	}

	// 2. 提取表头字段
	nature, _ := form["u_费用性质"].(string)
	costCenter, _ := form["E_system_costcenter"].(string)
	project, _ := form["项目"].(string)

	log.Printf("[Consumer] 单据 %s: 费用性质=%s 成本中心=%s 项目=%s", task.Code, nature, costCenter, project)

	// 3. TODO: 按校验规则校验
	// 4. TODO: 回调合思

	log.Printf("[Consumer] 处理完成: taskID=%s (业务逻辑待实现)", task.ID)
}

// fetchFlowForm 获取单据表单数据
func (c *Checker) fetchFlowForm(code string) (map[string]interface{}, error) {
	u := c.Client.HostURL("/api/openapi/v1.1/flowDetails/byCode?code=" + code)
	resp, err := c.Client.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var body map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&body)
		log.Printf("[Consumer] API返回错误: status=%d body=%v", resp.StatusCode, body)
		return nil, nil
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	val, _ := result["value"].(map[string]interface{})
	form, _ := val["form"].(map[string]interface{})
	return form, nil
}
