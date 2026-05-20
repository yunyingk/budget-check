package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"budget/src/budget"
	"budget/src/webhook"
)

type CheckRequest struct {
	Code   string `json:"code"`
	FlowID string `json:"flowId"`
	NodeID string `json:"nodeId"`
}

type CheckResponse struct {
	BudgetCheck string `json:"budget-check"`
	Success     bool   `json:"success"`
	Message     string `json:"message"`
	TaskID      string `json:"task_id,omitempty"`
}

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
		ID:         GenTaskID(req.Code),
		Code:       req.Code,
		FlowID:     req.FlowID,
		NodeID:     req.NodeID,
		EnqueuedAt: time.Now(),
		ClientIP:   r.RemoteAddr,
	}

	if Enqueue(task) {
		log.Printf("[Queue] 入队: taskID=%s code=%s flowId=%s nodeId=%s", task.ID, task.Code, task.FlowID, task.NodeID)
		writeJSON(w, 200, CheckResponse{
			BudgetCheck: "1",
			Success:     true,
			Message:     "已入队等待处理",
			TaskID:      task.ID,
		})
	} else {
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
	webhook.Process(webhook.Task{
		ID:         task.ID,
		Code:       task.Code,
		FlowID:     task.FlowID,
		NodeID:     task.NodeID,
		EnqueuedAt: task.EnqueuedAt,
		ClientIP:   task.ClientIP,
	})
}
