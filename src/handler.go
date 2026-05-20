package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"budget/src/budget"
)

type CheckRequest struct {
	Code   string `json:"code"`
	FlowID string `json:"flowId"`
	NodeID string `json:"nodeId"`
}

type CheckTask struct {
	ID        string
	Request   CheckRequest
	Enqueued  time.Time
	ClientIP  string
}

func genTaskID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

type CheckResponse struct {
	BudgetCheck string `json:"budget-check"`
	Success     bool   `json:"success"`
	Message     string `json:"message"`
	TaskID      string `json:"task_id,omitempty"`
	Pending     int    `json:"pending,omitempty"`
}

type CallbackPayload struct {
	TicketID string `json:"ticket_id"`
	Status   string `json:"status"`
	Message  string `json:"message"`
}

var taskQueue chan CheckTask

func handleCheck(w http.ResponseWriter, r *http.Request, store *budget.Store, cfg *Config) {
	var req CheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, CheckResponse{BudgetCheck: "0", Message: "请求格式错误"})
		return
	}
	if req.Code == "" || req.FlowID == "" || req.NodeID == "" {
		writeJSON(w, 400, CheckResponse{BudgetCheck: "0", Message: "code、flowId、nodeId 不能为空"})
		return
	}

	task := CheckTask{
		ID:       genTaskID(),
		Request:  req,
		Enqueued: time.Now(),
		ClientIP: r.RemoteAddr,
	}

	select {
	case taskQueue <- task:
		log.Printf("[Queue] 入队: taskID=%s code=%s flowId=%s nodeId=%s", task.ID, req.Code, req.FlowID, req.NodeID)
		writeJSON(w, 200, CheckResponse{
			BudgetCheck: "1",
			Success:     true,
			Message:     "已入队等待处理",
			TaskID:      task.ID,
			Pending:     len(taskQueue),
		})
	default:
		log.Printf("[Queue] 队列已满，拒绝: code=%s", req.Code)
		writeJSON(w, 503, CheckResponse{BudgetCheck: "0", Message: "队列已满，请稍后重试"})
	}
}

func handleStatus(w http.ResponseWriter, r *http.Request, store *budget.Store) {
	type statusResp struct {
		CacheCount int       `json:"cache_count"`
		UpdatedAt  time.Time `json:"updated_at"`
	}
	writeJSON(w, 200, statusResp{
		CacheCount: store.Count(),
		UpdatedAt:  store.UpdatedAt(),
	})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func processTask(task CheckTask, store *budget.Store) {
	log.Printf("[Task] 开始处理: taskID=%s code=%s flowId=%s nodeId=%s", task.ID, task.Request.Code, task.Request.FlowID, task.Request.NodeID)
	// TODO: 业务校验逻辑
	log.Printf("[Task] 处理完成: taskID=%s (暂未实现业务逻辑)", task.ID)
}