package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
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

func handleCheck(w http.ResponseWriter, r *http.Request, store *Store, client *EkbClient, cfg *Config) {
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

	go processTicket(req.TicketID, req.CallbackURL, store, client, cfg)

	writeJSON(w, 200, CheckResponse{Code: 200, Message: "处理中", Ticket: req.TicketID})
}

func processTicket(ticketID, callbackURL string, store *Store, client *EkbClient, cfg *Config) {
	log.Printf(">>> 正在处理单据: %s", ticketID)

	info, err := client.FetchTicket(ticketID, cfg.ExpenseNature)
	if err != nil {
		log.Printf("查询失败: %v", err)
		sendCallback(callbackURL, ticketID, "REJECT", "单据查询失败: "+err.Error(), client)
		return
	}

	result := CheckBudget(info, store, client)
	log.Printf(">>> [Result] pass=%v reason=%s", result.Pass, result.Reason)

	status := "PASS"
	if !result.Pass {
		status = "REJECT"
	}

	sendCallback(callbackURL, ticketID, status, result.Reason, client)
}

func sendCallback(callbackURL, ticketID, status, message string, client *EkbClient) {
	if callbackURL == "" {
		return
	}
	payload := CallbackPayload{
		TicketID: ticketID,
		Status:   status,
		Message:  message,
	}
	body, _ := json.Marshal(payload)
	resp, err := client.client.Post(callbackURL, "application/json", strings.NewReader(string(body)))
	if err != nil {
		log.Printf("回调失败: %v", err)
		return
	}
	resp.Body.Close()
	log.Printf(">>> [Callback] %s: status=%s message=%s", callbackURL, status, message)
}

func handleStatus(w http.ResponseWriter, r *http.Request, store *Store) {
	type statusResp struct {
		CacheCount int       `json:"cache_count"`
		UpdatedAt  time.Time `json:"updated_at"`
	}
	writeJSON(w, 200, statusResp{
		CacheCount: store.Count(),
		UpdatedAt:  store.UpdatedAt(),
	})
}

func handleSync(w http.ResponseWriter, r *http.Request, store *Store, client *EkbClient, cfg *Config, workers int) {
	go func() {
		log.Println("[API] 收到手动同步请求")
		SyncBudget(store, client, cfg.BudgetTargets, workers)
	}()
	writeJSON(w, 200, map[string]string{"message": "同步已启动"})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func intToStr(n int) string {
	return strconv.Itoa(n)
}