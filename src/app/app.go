package app

import (
	"budget/src/budget"
	"budget/src/config"
	"budget/src/consumer"
	"budget/src/ekb"
	"budget/src/metrics"
	"budget/src/queue"
	rotatelog "budget/src/log"
	"budget/src/rules"
	"budget/src/types"
	"budget/src/web"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// App 应用程序容器，持有所有运行时状态
type App struct {
	Config        *config.Config
	Logger        *rotatelog.RotatingLogger
	Queue         *queue.Queue
	Store         *budget.Store
	StoreMu       sync.RWMutex
	Client        *ekb.Client
	Syncing       atomic.Bool
	SyncCfg       budget.SyncConfig
	Checker       *consumer.Checker
	Engines       map[string]*rules.Engine        // webhookKey → Engine
	RulesCfgs     map[string]*types.RulesConfig   // webhookKey → RulesConfig（前端展示用）
	RulesPaths    map[string]string               // webhookKey → 规则文件路径
	Version       string
	StartTime     time.Time                       // 服务启动时间
	LastSyncDuration atomic.Int64                 // 上次同步耗时（纳秒）
}

// New 创建 App 实例（不初始化组件）
func New(cfg *config.Config, logger *rotatelog.RotatingLogger) *App {
	return &App{
		Config:    cfg,
		Logger:    logger,
		StartTime: time.Now(),
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

	// 收集所有 webhook 的 sign_key，并为每个 webhook 加载独立规则引擎
	signKeys := make(map[string]string)
	a.Engines = make(map[string]*rules.Engine)
	a.RulesCfgs = make(map[string]*types.RulesConfig)
	a.RulesPaths = make(map[string]string)

	for key, wh := range a.Config.Webhooks {
		if wh.SignKey != "" {
			signKeys[key] = wh.SignKey
		}

		rulesPath := wh.Rules
		if rulesPath == "" {
			rulesPath = fmt.Sprintf("rules/%s.json", key)
		}
		// 相对路径基于配置文件目录解析
		if !filepath.IsAbs(rulesPath) && a.Config.BaseDir != "" {
			rulesPath = filepath.Join(a.Config.BaseDir, rulesPath)
		}

		rulesCfg, err := config.LoadRules(rulesPath)
		if err != nil {
			log.Printf("[Init] webhook=%s 规则文件不存在或加载失败: %v", key, err)
			continue
		}
		engine, err := rules.NewEngine(a.Store, a.Client, rulesCfg, a.Config.DimensionNames)
		if err != nil {
			log.Printf("[Init] webhook=%s 规则编译失败: %v", key, err)
			continue
		}
		a.Engines[key] = engine
		a.RulesCfgs[key] = rulesCfg
		a.RulesPaths[key] = rulesPath
		log.Printf("[Init] webhook=%s 规则引擎加载成功: %s", key, rulesPath)
	}

	a.Checker = consumer.NewChecker(a.Client, a.Store, signKeys, a.Engines)
	return nil
}

// Sync 手动触发一次预算同步（带锁保护）
func (a *App) Sync() {
	a.Syncing.Store(true)
	defer a.Syncing.Store(false)
	a.StoreMu.Lock()
	defer a.StoreMu.Unlock()
	start := time.Now()
	budget.Sync(a.Store, a.Client, a.SyncCfg)
	duration := time.Since(start)
	a.LastSyncDuration.Store(int64(duration))
	metrics.SyncDuration.Observe(duration.Seconds())
	metrics.SyncTotal.WithLabelValues("success").Inc()
	metrics.LastSyncTimestamp.Set(float64(time.Now().Unix()))
}

// SaveRules 保存规则文件并重新编译引擎
func (a *App) SaveRules(key string, cfg *types.RulesConfig) error {
	path, ok := a.RulesPaths[key]
	if !ok {
		return fmt.Errorf("webhook %s 的规则路径未找到", key)
	}
	if err := config.SaveRules(path, cfg); err != nil {
		return err
	}
	// 重新编译引擎
	engine, err := rules.NewEngine(a.Store, a.Client, cfg, a.Config.DimensionNames)
	if err != nil {
		return fmt.Errorf("规则编译失败: %w", err)
	}
	a.Engines[key] = engine
	a.Checker.UpdateEngine(key, engine)
	log.Printf("[Rules] webhook=%s 规则已更新并重编译", key)
	return nil
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
					if err := a.Checker.CallbackApproval(task, action, comment); err != nil {
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
		Config:           a.Config,
		Store:            a.Store,
		Checker:          a.Checker,
		TokenStore:       tokenStore,
		Queue:            a.Queue,
		Syncing:          a.Syncing.Load,
		Version:          a.Version,
		OnSync:           a.Sync,
		RulesCfgs:        a.RulesCfgs,
		SaveRulesFunc:    a.SaveRules,
		StartTime:        a.StartTime,
		LastSyncDuration: &a.LastSyncDuration,
		Client:           a.Client,
	})

	addr := fmt.Sprintf("0.0.0.0:%d", a.Config.Server.Port)
	log.Printf("服务启动: %s", addr)
	return http.ListenAndServe(addr, mux)
}
