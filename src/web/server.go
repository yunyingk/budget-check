package web

import (
	"budget/src/budget"
	"budget/src/config"
	"budget/src/consumer"
	"budget/src/ekb"
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
	OnSync           func() error                  // 手动同步回调
	RulesCfgs        map[string]*types.RulesConfig // webhookKey → RulesConfig
	SaveRulesFunc    func(string, *types.RulesConfig) error // 保存规则+重编译引擎
	CreateWebhookFunc func(string, string) error   // 创建新 webhook
	StartTime        time.Time                     // 服务启动时间
	LastSyncDuration *atomic.Int64                 // 上次同步耗时（纳秒）
	Client           *ekb.Client                   // 合思客户端（用于获取费用类型数量）
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
			handleStatus(w, r, deps.Store, deps.Syncing, deps.Version, cfg.Sync.IntervalMinutes, cfg.Sync.QueueSize, deps.Queue.Len(), deps.StartTime, deps.LastSyncDuration, deps.Client)
		}))
		mux.HandleFunc("/api/history", authMiddleware(deps.TokenStore, cfg.Web.Password)(func(w http.ResponseWriter, r *http.Request) {
			handleHistory(w, r, deps.Checker)
		}))

		// 规则配置 API
		mux.HandleFunc("/api/rules/", authMiddleware(deps.TokenStore, cfg.Web.Password)(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPut {
				handleSaveRules(w, r, deps.RulesCfgs, deps.SaveRulesFunc, cfg.Web.AdminPassword)
			} else {
				handleRules(w, r, deps.RulesCfgs)
			}
		}))

		// Webhooks 配置 API（GET 列表 / POST 创建）
		mux.HandleFunc("/api/webhooks", authMiddleware(deps.TokenStore, cfg.Web.Password)(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				handleCreateWebhook(w, r, deps.CreateWebhookFunc, cfg.Web.AdminPassword)
			} else {
				handleWebhooks(w, r, cfg)
			}
		}))

		log.Printf("[Web] 管理页面已启用: http://localhost:%d", cfg.Server.Port)
	} else {
		mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
			handleStatus(w, r, deps.Store, deps.Syncing, deps.Version, cfg.Sync.IntervalMinutes, cfg.Sync.QueueSize, deps.Queue.Len(), deps.StartTime, deps.LastSyncDuration, deps.Client)
		})
	}

	// Prometheus metrics 端点（不需要认证）
	mux.Handle("/metrics", handleMetrics())

	// Webhooks：catch-all 动态路由，运行期新建的 webhook 自动生效
	mux.HandleFunc("/api/webhook/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		key := r.URL.Path[len("/api/webhook/"):]
		if key == "" {
			http.NotFound(w, r)
			return
		}
		// 动态检查：运行时创建的 webhook 也能路由
		if _, ok := cfg.Webhooks[key]; !ok {
			http.NotFound(w, r)
			return
		}
		handleWebhook(w, r, key, deps.Queue.Enqueue, queue.GenTaskID)
	})

	// Config API
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		if cfg.Web.AdminPassword == "" {
			http.Error(w, "disabled", 404)
			return
		}
		if r.URL.Query().Get("password") != cfg.Web.AdminPassword {
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
		if cfg.Web.AdminPassword != "" && r.URL.Query().Get("password") != cfg.Web.AdminPassword {
			writeJSON(w, 403, map[string]string{"error": "密码错误"})
			return
		}
		if deps.OnSync != nil {
			go func() {
				if err := deps.OnSync(); err != nil {
					log.Printf("[Sync] 手动同步失败: %v", err)
				}
			}()
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
