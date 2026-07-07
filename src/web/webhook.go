package web

import (
	"budget/src/types"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"
)

// WebhookRequest 合思回调请求
type WebhookRequest struct {
	Code   string `json:"code"`
	FlowID string `json:"flowId"`
	NodeID string `json:"nodeId"`
}

// WebhookResponse 返回给合思的响应
type WebhookResponse struct {
	BudgetCheck string `json:"budget-check"`
	Success     bool   `json:"success"`
	Message     string `json:"message"`
	TaskID      string `json:"task_id,omitempty"`
}

// handleWebhook HTTP 入口：解析请求、校验、入队、返回
// webhookKey 用于 consumer 回调时选择对应的 sign_key
func handleWebhook(w http.ResponseWriter, r *http.Request, webhookKey string, enqueue func(types.Task) bool, genID func(string) string) {
	var req WebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if errors.Is(err, io.EOF) {
			writeWebhookTestOK(w)
			return
		}
		writeJSON(w, 400, WebhookResponse{BudgetCheck: "0", Message: "请求格式错误"})
		return
	}
	// 测试通路：三个字段均为空，不入队直接返回 200
	if req.Code == "" && req.FlowID == "" && req.NodeID == "" {
		writeWebhookTestOK(w)
		return
	}

	if req.Code == "" || req.FlowID == "" || req.NodeID == "" {
		writeJSON(w, 400, WebhookResponse{BudgetCheck: "0", Message: "code、flowId、nodeId 不能为空"})
		return
	}

	task := types.Task{
		ID:         genID(req.Code),
		Code:       req.Code,
		FlowID:     req.FlowID,
		NodeID:     req.NodeID,
		WebhookKey: webhookKey,
		EnqueuedAt: time.Now(),
		ClientIP:   r.RemoteAddr,
	}

	if enqueue(task) {
		log.Printf("[Webhook] 入队: taskID=%s code=%s flowId=%s nodeId=%s webhook=%s", task.ID, task.Code, task.FlowID, task.NodeID, webhookKey)
		writeJSON(w, 200, WebhookResponse{
			BudgetCheck: "1",
			Success:     true,
			Message:     "已入队等待处理",
			TaskID:      task.ID,
		})
	} else {
		log.Printf("[Webhook] 队列已满，拒绝: code=%s webhook=%s", req.Code, webhookKey)
		writeJSON(w, 503, WebhookResponse{BudgetCheck: "0", Message: "队列已满，请稍后重试"})
	}
}

func writeWebhookTestOK(w http.ResponseWriter) {
	log.Printf("[Webhook] 测试通路: 空参数请求，不入队")
	writeJSON(w, http.StatusOK, WebhookResponse{BudgetCheck: "1", Success: true, Message: "测试通路，已跳过"})
}
