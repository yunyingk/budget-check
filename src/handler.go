package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"budget/src/budget"
)

type CheckRequest struct {
	TicketID    string `json:"ticket_id"`
	CallbackURL string `json:"callback_url"`
}

type CheckResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Ticket  string `json:"ticket_id"`
}

type CallbackPayload struct {
	TicketID string `json:"ticket_id"`
	Status   string `json:"status"`
	Message  string `json:"message"`
}

func handleCheck(w http.ResponseWriter, r *http.Request, store *budget.Store, cfg *Config) {
	var req CheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, CheckResponse{Code: 400, Message: "请求格式错误"})
		return
	}
	if req.TicketID == "" {
		writeJSON(w, 400, CheckResponse{Code: 400, Message: "ticket_id不能为空"})
		return
	}

	if store.Count() == 0 {
		writeJSON(w, 503, CheckResponse{Code: 503, Message: "预算数据尚未同步，请稍后重试"})
		return
	}

	// TODO: 业务校验逻辑待重写（使用树结构）
	log.Printf(">>> [TODO] 单据校验暂未实现: %s", req.TicketID)
	writeJSON(w, 200, CheckResponse{Code: 200, Message: "处理中", Ticket: req.TicketID})
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