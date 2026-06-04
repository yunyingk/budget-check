package web

import (
	"budget/src/budget"
	"budget/src/config"
	"budget/src/consumer"
	"budget/src/queue"
	"budget/src/types"
	"embed"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

//go:embed static/*
var staticFS embed.FS

// Deps web 层运行期依赖
type Deps struct {
	Config           *config.Config
	Store            *budget.Store
	Checker          *consumer.Checker
	TokenStore       *TokenStore
	Queue            *queue.Queue
	Syncing          func() bool
	Version          string
	OnSync           func()                        // 手动同步回调
	RulesCfgs        map[string]*types.RulesConfig // webhookKey → RulesConfig
	SaveRulesFunc    func(string, *types.RulesConfig) error // 保存规则+重编译引擎
	StartTime        time.Time                     // 服务启动时间
	LastSyncDuration *atomic.Int64                 // 上次同步耗时（纳秒）
}

// Register 向 mux 注册所有路由
func Register(mux *http.ServeMux, deps Deps) {
	cfg := deps.Config

	if cfg.Web.Enabled {
		mux.Handle("/static/", http.FileServer(http.FS(staticFS)))
		mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
			handleLoginPage(w, r, cfg.Web.Password)
		})
		mux.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
			handleLogin(w, r, deps.TokenStore, cfg.Web.Password)
		})
		mux.HandleFunc("/api/logout", func(w http.ResponseWriter, r *http.Request) {
			handleLogout(w, r, deps.TokenStore)
		})
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			authMiddleware(deps.TokenStore, cfg.Web.Password)(handleHome)(w, r)
		})
		mux.HandleFunc("/api/status", authMiddleware(deps.TokenStore, cfg.Web.Password)(func(w http.ResponseWriter, r *http.Request) {
			handleStatus(w, r, deps.Store, deps.Syncing, deps.Version, cfg.Sync.IntervalMinutes, cfg.Sync.QueueSize, deps.Queue.Len(), deps.StartTime, deps.LastSyncDuration)
		}))
		mux.HandleFunc("/api/history", authMiddleware(deps.TokenStore, cfg.Web.Password)(func(w http.ResponseWriter, r *http.Request) {
			handleHistory(w, r, deps.Checker)
		}))

		// 规则配置 API
		mux.HandleFunc("/api/rules/", authMiddleware(deps.TokenStore, cfg.Web.Password)(func(w http.ResponseWriter, r *http.Request) {
			handleRules(w, r, deps.RulesCfgs)
		}))

		// Webhooks 配置 API
		mux.HandleFunc("/api/webhooks", authMiddleware(deps.TokenStore, cfg.Web.Password)(func(w http.ResponseWriter, r *http.Request) {
			handleWebhooks(w, r, cfg)
		}))

		log.Printf("[Web] 管理页面已启用: http://localhost:%d", cfg.Server.Port)
	} else {
		mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
			handleStatus(w, r, deps.Store, deps.Syncing, deps.Version, cfg.Sync.IntervalMinutes, cfg.Sync.QueueSize, deps.Queue.Len(), deps.StartTime, deps.LastSyncDuration)
		})
	}

	// Prometheus metrics 端点（不需要认证）
	mux.Handle("/metrics", handleMetrics())

	// Webhooks：按配置动态注册，每个 webhook 一个路由
	for key := range cfg.Webhooks {
		webhookKey := key // 闭包捕获
		mux.HandleFunc("/api/webhook/"+webhookKey, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", 405)
				return
			}
			handleWebhook(w, r, webhookKey, deps.Queue.Enqueue, queue.GenTaskID)
		})
	}

	// Config API
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		if cfg.Sync.Password == "" {
			http.Error(w, "disabled", 404)
			return
		}
		if r.URL.Query().Get("password") != cfg.Sync.Password {
			writeJSON(w, 403, map[string]string{"error": "密码错误"})
			return
		}
		writeJSON(w, 200, cfg)
	})

	// Sync API
	mux.HandleFunc("/api/sync", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		if cfg.Sync.Password != "" && r.URL.Query().Get("password") != cfg.Sync.Password {
			writeJSON(w, 403, map[string]string{"error": "密码错误"})
			return
		}
		if deps.OnSync != nil {
			go deps.OnSync()
		}
		writeJSON(w, 200, map[string]interface{}{
			"success":       true,
			"message":       "同步已启动",
			"started_at":    time.Now().Format(time.RFC3339),
			"last_sync_at":  deps.Store.UpdatedAt().Format(time.RFC3339),
			"client_ip":     r.RemoteAddr,
			"current_count": deps.Store.Count(),
			"workers":       cfg.Sync.Workers,
		})
	})
}
