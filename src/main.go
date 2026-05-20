package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"time"

	"budget/src/budget"
	"budget/src/consumer"
	"budget/src/ekb"
	"budget/src/webhook"

	"golang.org/x/sys/windows/svc"
)

var (
	cfg        *Config
	store      *budget.Store
	client     *ekb.Client
	syncCfg    budget.SyncConfig
	checker    *consumer.Checker
)

func main() {
	configPath := flag.String("config", "config.yaml", "配置文件路径（默认与exe同目录）")
	syncNow := flag.Bool("sync", false, "立即执行一次同步后退出")
	install := flag.Bool("install", false, "注册为 Windows 服务")
	uninstall := flag.Bool("uninstall", false, "卸载 Windows 服务")
	flag.Parse()

	// Windows 服务管理命令
	if *install {
		if runtime.GOOS != "windows" {
			fmt.Println("服务注册仅支持 Windows")
			os.Exit(1)
		}
		if err := installService(); err != nil {
			fmt.Printf("注册服务失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("服务注册成功！启动: sc start BudgetCheck")
		return
	}
	if *uninstall {
		if runtime.GOOS != "windows" {
			fmt.Println("服务卸载仅支持 Windows")
			os.Exit(1)
		}
		if err := uninstallService(); err != nil {
			fmt.Printf("卸载服务失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("服务已卸载")
		return
	}

	// 加载配置
	var err error
	cfg, err = LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 初始化日志
	logger, err := NewRotatingLogger("logs", RotatePeriod(cfg.Logging.Rotation))
	if err != nil {
		log.Fatalf("初始化日志失败: %v", err)
	}
	defer logger.Close()
	log.SetOutput(logger)

	// 手动同步模式
	if *syncNow {
		initComponents()
		budget.Sync(store, client, syncCfg)
		fmt.Printf("同步完成，缓存条目: %d\n", store.Count())
		return
	}

	// Windows 服务模式 or 控制台模式
	if runtime.GOOS == "windows" {
		isWinService, _ := svc.IsWindowsService()
		if isWinService {
			if err := runService(); err != nil {
				log.Fatalf("Windows 服务启动失败: %v", err)
			}
			return
		}
	}

	// 控制台模式
	mainLogic()
}

// initComponents 初始化所有组件
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
	checker = consumer.NewChecker(client, store, cfg.Ekb.SignKey, cfg.ExpenseNature, cfg.ExemptProjects)
}

// mainLogic 主业务逻辑（服务模式和控制台模式共用）
func mainLogic() {
	initComponents()

	log.Println("[Init] 开始后台同步预算数据...")
	go func() {
		budget.Sync(store, client, syncCfg)
		log.Println("[Init] 首次同步完成，开始消费队列")

		go func() {
			for task := range QueueChan() {
				func() {
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
			budget.Sync(store, client, syncCfg)
		}
	}()

	mux := http.NewServeMux()
	if cfg.Web.Enabled {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			handleHome(w, r, store, cfg)
		})
		log.Println("[Web] 管理页面已启用: /")
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
			budget.Sync(store, client, syncCfg)
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
