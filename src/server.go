package main

import (
	"embed"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"budget/src/budget"
	"budget/src/consumer"
	"budget/src/ekb"
	"budget/src/rules"
	"budget/src/webhook"
)

//go:embed static/*
var staticFS embed.FS

var (
	cfg        *Config
	store      *budget.Store
	client     *ekb.Client
	syncCfg    budget.SyncConfig
	checker    *consumer.Checker
	storeMu     sync.RWMutex
	syncing    atomic.Bool
	tokenStore *TokenStore
)

func initComponents() {
	log.Printf("配置加载成功: 端口=%d, 合思主机=%s", cfg.Server.Port, cfg.Ekb.Host)

	workers := cfg.Sync.Workers
	if workers <= 0 {
		workers = 10
	}

	store = budget.NewStore()
	client = ekb.NewClient(cfg.Ekb.Host, cfg.Ekb.AppKey, cfg.Ekb.AppSecret)
	tokenStore = NewTokenStore()

	queueSize := cfg.Sync.QueueSize
	if queueSize <= 0 {
		queueSize = 100
	}
	InitQueue(queueSize)

	// 从 webhooks 中收集所有 targets（去重）
	targetMap := make(map[string]budget.Target)
	for _, wh := range cfg.Webhooks {
		for _, t := range wh.Targets {
			targetMap[t.ID] = budget.Target{ID: t.ID, Name: t.Name}
		}
	}
	var targets []budget.Target
	for _, t := range targetMap {
		targets = append(targets, t)
	}
	syncCfg = budget.SyncConfig{Targets: targets, Workers: workers}

	// 获取 budget-check webhook 的 sign_key 和 rules
	signKey := ""
	rulesPath := ""
	if wh, ok := cfg.Webhooks["budget-check"]; ok {
		signKey = wh.SignKey
		rulesPath = wh.Rules
	}
	
	var engine *rules.Engine
	if rulesPath != "" {
		rulesCfg, err := LoadRules(rulesPath)
		if err != nil {
			log.Printf("[Init] 加载规则文件失败: %v", err)
		} else {
			engine, err = rules.NewEngine(store, client, rulesCfg)
			if err != nil {
				log.Printf("[Init] 规则编译失败: %v", err)
			} else {
				log.Printf("[Init] 规则引擎加载成功: %s", rulesPath)
			}
		}
	}
	checker = consumer.NewChecker(client, store, signKey, engine)
}

func doSync() {
	syncing.Store(true)
	defer syncing.Store(false)
	storeMu.Lock()
	defer storeMu.Unlock()
	budget.Sync(store, client, syncCfg)
}

func mainLogic() {
	initComponents()

	log.Println("[Init] 开始后台同步预算数据...")
	go func() {
		doSync()
		log.Println("[Init] 首次同步完成，开始消费队列")

		go func() {
			for task := range QueueChan() {
				func() {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("[Consumer] panic: %v, taskID=%s", r, task.ID)
						}
					}()
					storeMu.RLock()
					action, comment := checker.Evaluate(task)
					storeMu.RUnlock()
					if err := checker.CallbackApproval(task.FlowID, task.NodeID, action, comment); err != nil {
						log.Printf("[Consumer] 回调审批失败: %v", err)
					}
				}()
			}
		}()

		for {
			time.Sleep(time.Duration(cfg.Sync.IntervalMinutes) * time.Minute)
			doSync()
		}
	}()

	mux := http.NewServeMux()
	if cfg.Web.Enabled {
		mux.Handle("/static/", http.FileServer(http.FS(staticFS)))
		mux.HandleFunc("/login", handleLoginPage)
		mux.HandleFunc("/api/login", handleLogin)
		mux.HandleFunc("/api/logout", handleLogout)
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			authMiddleware(handleHome)(w, r)
		})
		mux.HandleFunc("/api/status", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
			handleStatus(w, r, store)
		}))
		mux.HandleFunc("/api/history", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
			handleHistory(w, r, checker)
		}))
		log.Println("[Web] 管理页面已启用: http://localhost" + fmt.Sprintf(":%d", cfg.Server.Port))
	} else {
		mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
			handleStatus(w, r, store)
		})
	}
	mux.HandleFunc("/api/webhook/budget-check", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		webhook.Handle(w, r, Enqueue, GenTaskID)
	})
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
	mux.HandleFunc("/api/sync", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		if cfg.Sync.Password != "" && r.URL.Query().Get("password") != cfg.Sync.Password {
			writeJSON(w, 403, map[string]string{"error": "密码错误"})
			return
		}
		go func() {
			log.Println("[API] 收到手动同步请求")
			doSync()
		}()
		writeJSON(w, 200, map[string]interface{}{
			"success":       true,
			"message":       "同步已启动",
			"started_at":    time.Now().Format(time.RFC3339),
			"last_sync_at":  store.UpdatedAt().Format(time.RFC3339),
			"client_ip":     r.RemoteAddr,
			"current_count": store.Count(),
			"workers":       cfg.Sync.Workers,
		})
	})

	addr := fmt.Sprintf("0.0.0.0:%d", cfg.Server.Port)
	log.Printf("服务启动: %s 版本=%s", addr, version)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
