package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"budget/src/budget"
)

func main() {
	configPath := flag.String("config", "config.yaml", "配置文件路径（默认与exe同目录）")
	syncNow := flag.Bool("sync", false, "立即执行一次同步后退出")
	flag.Parse()

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	logger, err := NewRotatingLogger("logs", RotatePeriod(cfg.Logging.Rotation))
	if err != nil {
		log.Fatalf("初始化日志失败: %v", err)
	}
	defer logger.Close()
	log.SetOutput(logger)

	log.Printf("配置加载成功: 端口=%d, 合思主机=%s", cfg.Server.Port, cfg.Ekb.Host)

	workers := cfg.Sync.Workers
	if workers <= 0 {
		workers = 10
	}

	store := budget.NewStore()
	fetcher := budget.NewEkbFetcher(cfg.Ekb.Host, cfg.Ekb.AppKey, cfg.Ekb.AppSecret)

	queueSize := cfg.Sync.QueueSize
	if queueSize <= 0 {
		queueSize = 100
	}
	taskQueue = make(chan CheckTask, queueSize)

	var targets []budget.Target
	for _, t := range cfg.BudgetTargets {
		targets = append(targets, budget.Target{ID: t.ID, Name: t.Name, Depth: t.Depth})
	}
	syncCfg := budget.SyncConfig{Targets: targets, Workers: workers}

	if *syncNow {
		budget.Sync(store, fetcher, syncCfg)
		fmt.Printf("同步完成，缓存条目: %d\n", store.Count())
		return
	}

	log.Println("[Init] 开始后台同步预算数据...")
	go func() {
		budget.Sync(store, fetcher, syncCfg)
		log.Println("[Init] 首次同步完成，开始消费队列")
		go func() {
			for task := range taskQueue {
				processTask(task, store)
			}
		}()
		for {
			time.Sleep(time.Duration(cfg.Sync.IntervalMinutes) * time.Minute)
			budget.Sync(store, fetcher, syncCfg)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/webhook/budget-check", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		handleCheck(w, r, store, cfg)
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
			budget.Sync(store, fetcher, syncCfg)
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