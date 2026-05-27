package webhook

import (
	"budget/src/types"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// Request 合思回调请求
type Request struct {
	Code   string `json:"code"`
	FlowID string `json:"flowId"`
	NodeID string `json:"nodeId"`
}

// Response 返回给合思的响应
type Response struct {
	BudgetCheck string `json:"budget-check"`
	Success     bool   `json:"success"`
	Message     string `json:"message"`
	TaskID      string `json:"task_id,omitempty"`
}

// Handle HTTP 入口：解析请求、校验、入队、返回
func Handle(w http.ResponseWriter, r *http.Request, enqueue func(types.Task) bool, genID func(string) string) {
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, Response{BudgetCheck: "0", Message: "请求格式错误"})
		return
	}
	// 测试通路：三个字段均为空，不入队直接返回 201
	if req.Code == "" && req.FlowID == "" && req.NodeID == "" {
		log.Printf("[Webhook] 测试通路: 空参数请求，不入队")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(Response{BudgetCheck: "1", Success: true, Message: "测试通路，已跳过"})
		return
	}

	if req.Code == "" || req.FlowID == "" || req.NodeID == "" {
		writeJSON(w, 400, Response{BudgetCheck: "0", Message: "code、flowId、nodeId 不能为空"})
		return
	}

	task := types.Task{
		ID:         genID(req.Code),
		Code:       req.Code,
		FlowID:     req.FlowID,
		NodeID:     req.NodeID,
		EnqueuedAt: time.Now(),
		ClientIP:   r.RemoteAddr,
	}

	if enqueue(task) {
		log.Printf("[Webhook] 入队: taskID=%s code=%s flowId=%s nodeId=%s", task.ID, task.Code, task.FlowID, task.NodeID)
		writeJSON(w, 200, Response{
			BudgetCheck: "1",
			Success:     true,
			Message:     "已入队等待处理",
			TaskID:      task.ID,
		})
	} else {
		log.Printf("[Webhook] 队列已满，拒绝: code=%s", req.Code)
		writeJSON(w, 503, Response{BudgetCheck: "0", Message: "队列已满，请稍后重试"})
	}
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
