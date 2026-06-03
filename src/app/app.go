package app

import (
	"budget/src/budget"
	"budget/src/config"
	"budget/src/consumer"
	"budget/src/ekb"
	"budget/src/queue"
	rotatelog "budget/src/log"
	"budget/src/rules"
	"budget/src/web"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// App 应用程序容器，持有所有运行时状态
type App struct {
	Config  *config.Config
	Logger  *rotatelog.RotatingLogger
	Queue   *queue.Queue
	Store   *budget.Store
	StoreMu sync.RWMutex
	Client  *ekb.Client
	Syncing atomic.Bool
	SyncCfg budget.SyncConfig
	Checker *consumer.Checker
	Engine  *rules.Engine
	Version string
}

// New 创建 App 实例（不初始化组件）
func New(cfg *config.Config, logger *rotatelog.RotatingLogger) *App {
	return &App{
		Config: cfg,
		Logger: logger,
	}
}

// Init 初始化所有组件（store、client、queue、checker、engine）
func (a *App) Init() error {
	log.Printf("配置加载成功: 端口=%d, 合思主机=%s", a.Config.Server.Port, a.Config.Ekb.Host)

	workers := a.Config.Sync.Workers
	if workers <= 0 {
		workers = 10
	}

	a.Store = budget.NewStore()
	a.Client = ekb.NewClient(a.Config.Ekb.Host, a.Config.Ekb.AppKey, a.Config.Ekb.AppSecret)

	queueSize := a.Config.Sync.QueueSize
	if queueSize <= 0 {
		queueSize = 100
	}
	a.Queue = queue.New(queueSize)

	// 从 webhooks 中收集所有 targets（去重）
	targetMap := make(map[string]budget.Target)
	for _, wh := range a.Config.Webhooks {
		for _, t := range wh.Targets {
			targetMap[t.ID] = budget.Target{ID: t.ID, Name: t.Name}
		}
	}
	var targets []budget.Target
	for _, t := range targetMap {
		targets = append(targets, t)
	}
	a.SyncCfg = budget.SyncConfig{Targets: targets, Workers: workers}

	// 获取 budget-check webhook 的 sign_key 和 rules
	signKey := ""
	rulesPath := ""
	if wh, ok := a.Config.Webhooks["budget-check"]; ok {
		signKey = wh.SignKey
		rulesPath = wh.Rules
	}

	if rulesPath != "" {
		rulesCfg, err := config.LoadRules(rulesPath)
		if err != nil {
			log.Printf("[Init] 加载规则文件失败: %v", err)
		} else {
			a.Engine, err = rules.NewEngine(a.Store, a.Client, rulesCfg)
			if err != nil {
				log.Printf("[Init] 规则编译失败: %v", err)
			} else {
				log.Printf("[Init] 规则引擎加载成功: %s", rulesPath)
			}
		}
	}

	a.Checker = consumer.NewChecker(a.Client, a.Store, signKey, a.Engine)
	return nil
}

// Sync 手动触发一次预算同步（带锁保护）
func (a *App) Sync() {
	a.Syncing.Store(true)
	defer a.Syncing.Store(false)
	a.StoreMu.Lock()
	defer a.StoreMu.Unlock()
	budget.Sync(a.Store, a.Client, a.SyncCfg)
}

// Run 启动后台同步循环、消费循环和 HTTP Server（阻塞）
func (a *App) Run() error {
	log.Println("[Init] 开始后台同步预算数据...")
	go func() {
		a.Sync()
		log.Println("[Init] 首次同步完成，开始消费队列")

		// 消费循环
		go func() {
			for task := range a.Queue.Chan() {
				func() {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("[Consumer] panic: %v, taskID=%s", r, task.ID)
						}
					}()
					a.StoreMu.RLock()
					action, comment := a.Checker.Evaluate(task)
					a.StoreMu.RUnlock()
					if err := a.Checker.CallbackApproval(task.FlowID, task.NodeID, action, comment); err != nil {
						log.Printf("[Consumer] 回调审批失败: %v", err)
					}
				}()
			}
		}()

		// 定时同步
		for {
			time.Sleep(time.Duration(a.Config.Sync.IntervalMinutes) * time.Minute)
			a.Sync()
		}
	}()

	mux := http.NewServeMux()
	tokenStore := web.NewTokenStore()
	web.Register(mux, web.Deps{
		Config:     a.Config,
		Store:      a.Store,
		Checker:    a.Checker,
		TokenStore: tokenStore,
		Queue:      a.Queue,
		Syncing:    a.Syncing.Load,
		Version:    a.Version,
		OnSync:     a.Sync,
	})

	addr := fmt.Sprintf("0.0.0.0:%d", a.Config.Server.Port)
	log.Printf("服务启动: %s", addr)
	return http.ListenAndServe(addr, mux)
}
