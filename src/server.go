package main

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"budget/src/budget"
	"budget/src/consumer"
	"budget/src/ekb"
	"budget/src/webhook"
)

var (
	cfg     *Config
	store   *budget.Store
	client  *ekb.Client
	syncCfg budget.SyncConfig
	checker *consumer.Checker
	storeMu sync.RWMutex
	syncing atomic.Bool
)

func initComponents() {
	log.Printf("配置加载成功: 端口=%d, 合思主机=%s", cfg.Server.Port, cfg.Ekb.Host)

	workers := cfg.Sync.Workers
	if workers <= 0 {
		workers = 10
	}

	store = budget.NewStore()
	client = ekb.NewClient(cfg.Ekb.Host, cfg.Ekb.AppKey, cfg.Ekb.AppSecret)

	queueSize := cfg.Sync.QueueSize
	if queueSize <= 0 {
		queueSize = 100
	}
	InitQueue(queueSize)

	var targets []budget.Target
	for _, t := range cfg.BudgetTargets {
		targets = append(targets, budget.Target{ID: t.ID, Name: t.Name, Depth: t.Depth})
	}
	syncCfg = budget.SyncConfig{Targets: targets, Workers: workers}

	costCenterID := ""
	projectID := ""
	if len(cfg.BudgetTargets) >= 1 {
		costCenterID = cfg.BudgetTargets[0].ID
	}
	if len(cfg.BudgetTargets) >= 2 {
		projectID = cfg.BudgetTargets[1].ID
	}
	checker = consumer.NewChecker(client, store, cfg.Ekb.SignKey, cfg.ExemptProjects, costCenterID, projectID)
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
					storeMu.RLock()
					defer storeMu.RUnlock()
					defer func() {
						if r := recover(); r != nil {
							log.Printf("[Consumer] panic: %v, taskID=%s", r, task.ID)
						}
					}()
					checker.Process(task)
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
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			handleHome(w, r, cfg, store)
		})
		mux.HandleFunc("/api/history", func(w http.ResponseWriter, r *http.Request) {
			handleHistory(w, r, checker)
		})
		log.Println("[Web] 管理页面已启用: http://localhost" + fmt.Sprintf(":%d", cfg.Server.Port))
	}
	mux.HandleFunc("/api/webhook/budget-check", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		webhook.Handle(w, r, Enqueue, GenTaskID)
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		handleStatus(w, r, store)
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
	log.Printf("服务启动: %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
