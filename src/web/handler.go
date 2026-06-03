package web

import (
	"budget/src/budget"
	"budget/src/consumer"
	"encoding/json"
	"net/http"
	"runtime"
	"time"
)

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func handleStatus(w http.ResponseWriter, r *http.Request, store *budget.Store, syncing func() bool, version string, interval int, queueSize int) {
	lastSync := store.UpdatedAt()
	lastSyncStr := ""
	if !lastSync.IsZero() {
		lastSyncStr = lastSync.Format(time.RFC3339)
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	var targets []map[string]interface{}
	for _, tree := range store.Trees() {
		targets = append(targets, map[string]interface{}{
			"name":  tree.Name,
			"count": store.GetTreeNodeCount(tree.ID),
		})
	}

	writeJSON(w, 200, map[string]interface{}{
		"status":           "ok",
		"version":          version,
		"total_leaf_count": store.TotalLeafCount(),
		"is_syncing":       syncing(),
		"last_sync_at":     lastSyncStr,
		"memory_mb":        bToMB(m.Alloc),
		"goroutines":       runtime.NumGoroutine(),
		"interval_minutes": interval,
		"queue_size":       queueSize,
		"targets":          targets,
	})
}

func handleHistory(w http.ResponseWriter, r *http.Request, checker *consumer.Checker) {
	writeJSON(w, 200, checker.GetHistory())
}

func bToMB(b uint64) uint64 { return b / 1024 / 1024 }

func handleHome(w http.ResponseWriter, r *http.Request) {
	data, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "页面加载失败", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}
