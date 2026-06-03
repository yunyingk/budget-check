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

	if c.Engine == nil {
		return "refuse", "规则引擎未配置"
	}
	return c.Engine.Evaluate(form, details)
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
