package web

import (
	"budget/src/budget"
	"budget/src/config"
	"budget/src/consumer"
	"budget/src/types"
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

// handleRules 返回指定 webhook 的规则配置 /api/rules/{webhookKey}
func handleRules(w http.ResponseWriter, r *http.Request, rulesCfgs map[string]*types.RulesConfig) {
	// 从 /api/rules/{webhookKey} 中提取 webhookKey
	prefix := "/api/rules/"
	key := r.URL.Path[len(prefix):]
	if key == "" {
		writeJSON(w, 400, map[string]string{"error": "缺少 webhook key"})
		return
	}
	cfg, ok := rulesCfgs[key]
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "规则配置未找到"})
		return
	}
	writeJSON(w, 200, cfg)
}

// handleWebhooks 返回 webhook 配置列表（sign_key 脱敏）
func handleWebhooks(w http.ResponseWriter, r *http.Request, cfg *config.Config) {
	var list []map[string]interface{}
	for key, wh := range cfg.Webhooks {
		signKey := wh.SignKey
		if len(signKey) > 8 {
			signKey = signKey[:4] + "****" + signKey[len(signKey)-4:]
		} else if signKey != "" {
			signKey = "****"
		}
		list = append(list, map[string]interface{}{
			"key":      key,
			"sign_key": signKey,
			"rules":    wh.Rules,
			"targets":  wh.Targets,
		})
	}
	writeJSON(w, 200, list)
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
