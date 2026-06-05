package web

import (
	"budget/src/budget"
	"budget/src/config"
	"budget/src/consumer"
	"budget/src/ekb"
	"budget/src/metrics"
	"budget/src/types"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func handleMetrics() http.Handler {
	return promhttp.Handler()
}

func handleStatus(w http.ResponseWriter, r *http.Request, store *budget.Store, syncing func() bool, version string, interval int, queueSize int, queuePending int, startTime time.Time, lastSyncDuration *atomic.Int64, client *ekb.Client) {
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

	// 计算运行时间
	uptime := time.Since(startTime)
	uptimeStr := formatDuration(uptime)

	// 同步耗时
	syncDurationSec := float64(0)
	if lastSyncDuration != nil {
		syncDurationSec = float64(lastSyncDuration.Load()) / float64(time.Second)
	}

	// 更新队列指标
	metrics.QueueSize.Set(float64(queueSize))
	metrics.QueuePending.Set(float64(queuePending))

	// 获取 Prometheus 指标
	promMetrics := metrics.GetMetrics()

	// 获取费用类型数量
	feeTypeCount := 0
	if client != nil {
		feeTypeCount = client.FeeTypeCount()
	}

	writeJSON(w, 200, map[string]interface{}{
		"status":           "ok",
		"version":          version,
		"uptime":           uptimeStr,
		"total_leaf_count": store.TotalLeafCount(),
		"fee_type_count":   feeTypeCount,
		"is_syncing":       syncing(),
		"last_sync_at":     lastSyncStr,
		"sync_duration_sec": syncDurationSec,
		"memory_mb":        bToMB(m.Alloc),
		"goroutines":       runtime.NumGoroutine(),
		"interval_minutes": interval,
		"queue": map[string]interface{}{
			"pending":  queuePending,
			"capacity": queueSize,
		},
		"targets":  targets,
		"metrics":  promMetrics,
	})
}

// formatDuration 格式化持续时间为可读字符串
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
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

// handleSaveRules 保存规则配置 PUT /api/rules/{webhookKey}
func handleSaveRules(w http.ResponseWriter, r *http.Request, rulesCfgs map[string]*types.RulesConfig, saveFunc func(string, *types.RulesConfig) error) {
	if r.Method != http.MethodPut {
		writeJSON(w, 405, map[string]string{"error": "方法不允许"})
		return
	}
	prefix := "/api/rules/"
	key := r.URL.Path[len(prefix):]
	if key == "" {
		writeJSON(w, 400, map[string]string{"error": "缺少 webhook key"})
		return
	}
	if _, ok := rulesCfgs[key]; !ok {
		writeJSON(w, 404, map[string]string{"error": "规则配置未找到"})
		return
	}

	var cfg types.RulesConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSON(w, 400, map[string]string{"error": "JSON 解析失败: " + err.Error()})
		return
	}

	if err := saveFunc(key, &cfg); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	// 更新内存中的配置
	rulesCfgs[key] = &cfg
	writeJSON(w, 200, map[string]string{"status": "ok"})
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

// handleCreateWebhook 创建新 webhook POST /api/webhooks
func handleCreateWebhook(w http.ResponseWriter, r *http.Request, createFunc func(string, string) error) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "方法不允许"})
		return
	}
	var req struct {
		Key     string `json:"key"`
		SignKey string `json:"sign_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "JSON 解析失败"})
		return
	}
	if req.Key == "" {
		writeJSON(w, 400, map[string]string{"error": "webhook key 不能为空"})
		return
	}
	if req.SignKey == "" {
		writeJSON(w, 400, map[string]string{"error": "sign_key 不能为空"})
		return
	}
	if createFunc == nil {
		writeJSON(w, 500, map[string]string{"error": "创建功能未启用"})
		return
	}
	if err := createFunc(req.Key, req.SignKey); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok", "key": req.Key})
}

func handleHome(w http.ResponseWriter, r *http.Request) {
	data, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "页面加载失败", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}
