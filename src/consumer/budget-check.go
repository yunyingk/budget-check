package consumer

import (
	"budget/src/budget"
	"budget/src/ekb"
	"budget/src/metrics"
	"budget/src/rules"
	"budget/src/types"
	"encoding/json"
	"fmt"
	"io"
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

const defaultHistoryMax = 50

type Checker struct {
	Client   *ekb.Client
	Store    *budget.Store
	SignKeys map[string]string        // webhookKey → signKey
	Engines  map[string]*rules.Engine // webhookKey → Engine
	History  []HistoryItem
	mu       sync.Mutex
}

func NewChecker(client *ekb.Client, store *budget.Store, signKeys map[string]string, engines map[string]*rules.Engine) *Checker {
	return &Checker{
		Client:   client,
		Store:    store,
		SignKeys: signKeys,
		Engines:  engines,
	}
}

func (c *Checker) AddHistory(code, action, comment string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.History = append([]HistoryItem{{Time: time.Now().Format("15:04:05"), Code: code, Action: action, Comment: comment}}, c.History...)
	if len(c.History) > defaultHistoryMax {
		c.History = c.History[:defaultHistoryMax]
	}
}

// UpdateEngine 更新指定 webhook 的规则引擎（线程安全）
func (c *Checker) UpdateEngine(key string, engine *rules.Engine) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Engines[key] = engine
}

// AddSignKey 注册新的 webhook sign key（线程安全）
func (c *Checker) AddSignKey(key, signKey string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.SignKeys[key] = signKey
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

	start := time.Now()
	defer func() {
		metrics.CheckDuration.WithLabelValues(task.WebhookKey).Observe(time.Since(start).Seconds())
	}()

	form, err := c.fetchFlowData(task.Code)
	if err != nil {
		log.Printf("[Consumer] 获取单据失败: %v", err)
		metrics.ChecksTotal.WithLabelValues(task.WebhookKey, "error").Inc()
		return "refuse", fmt.Sprintf("系统错误：获取单据失败: %v", err)
	}
	if form == nil {
		log.Printf("[Consumer] 单据 %s 未找到", task.Code)
		metrics.ChecksTotal.WithLabelValues(task.WebhookKey, "error").Inc()
		return "refuse", "系统错误：单据未找到"
	}

	engine := c.Engines[task.WebhookKey]
	if engine == nil {
		metrics.ChecksTotal.WithLabelValues(task.WebhookKey, "error").Inc()
		return "refuse", fmt.Sprintf("规则引擎未配置: webhook=%s", task.WebhookKey)
	}

	action, comment := engine.Evaluate(form)
	metrics.ChecksTotal.WithLabelValues(task.WebhookKey, action).Inc()
	return action, comment
}

func (c *Checker) fetchFlowData(code string) (map[string]interface{}, error) {
	u := c.Client.HostURL("/api/openapi/v1.1/flowDetails/byCode?code=" + code)
	resp, err := c.Client.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	val, _ := result["value"].(map[string]interface{})
	if val == nil {
		return nil, nil
	}

	form, _ := val["form"].(map[string]interface{})
	if form == nil {
		return nil, nil
	}

	return form, nil
}

func (c *Checker) CallbackApproval(task types.Task, action, comment string) error {
	signKey, ok := c.SignKeys[task.WebhookKey]
	if !ok {
		return fmt.Errorf("未找到 webhookKey=%s 对应的 sign_key", task.WebhookKey)
	}

	body, _ := json.Marshal(map[string]string{
		"signKey": signKey,
		"flowId":  task.FlowID,
		"nodeId":  task.NodeID,
		"action":  action,
		"comment": comment,
	})

	url := c.Client.HostURL("/api/openapi/v1/approval")
	resp, err := c.Client.Post(url, body)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}
	log.Printf("[Consumer] 审批回调响应: taskID=%s code=%s webhook=%s flowID=%s nodeID=%s action=%s comment=%q status=%d body=%s",
		task.ID, task.Code, task.WebhookKey, task.FlowID, task.NodeID, action, comment, resp.StatusCode, truncateLog(respBody, 4000))

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
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

	log.Printf("[Consumer] 审批回调成功: taskID=%s code=%s flowID=%s nodeID=%s action=%s", task.ID, task.Code, task.FlowID, task.NodeID, action)
	return nil
}

func truncateLog(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "...(truncated)"
}
