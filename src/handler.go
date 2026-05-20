package main

import (
	"encoding/json"
	"net/http"
	"time"

	"budget/src/budget"
)

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
