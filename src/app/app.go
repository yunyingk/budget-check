package app

import (
	"budget/src/budget"
	"budget/src/config"
	"budget/src/consumer"
	"budget/src/ekb"
	rotatelog "budget/src/log"
	"budget/src/metrics"
	"budget/src/queue"
	"budget/src/rules"
	"budget/src/types"
	"budget/src/web"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// App 应用程序容器，持有所有运行时状态
type App struct {
	Config           *config.Config
	Logger           *rotatelog.RotatingLogger
	Queue            *queue.Queue
	Store            *budget.Store
	StoreMu          sync.RWMutex
	Client           *ekb.Client
	Syncing          atomic.Bool
	SyncCfg          budget.SyncConfig
	Checker          *consumer.Checker
	Engines          map[string]*rules.Engine      // webhookKey → Engine
	RulesCfgs        map[string]*types.RulesConfig // webhookKey → RulesConfig（前端展示用）
	RulesPaths       map[string]string             // webhookKey → 规则文件路径
	Version          string
	StartTime        time.Time    // 服务启动时间
	LastSyncDuration atomic.Int64 // 上次同步耗时（纳秒）
	SyncStartedAt    atomic.Int64 // 当前同步开始时间（Unix 秒，0 表示未同步中）
}

var ErrSyncAlreadyRunning = errors.New("同步已在进行中")

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

	a.Store = budget.NewStore()
	a.Client = ekb.NewClient(a.Config.Ekb.Host, a.Config.Ekb.AppKey, a.Config.Ekb.AppSecret)

	queueSize := a.Config.Sync.QueueSize
	if queueSize <= 0 {
		queueSize = 100
	}
	a.Queue = queue.New(queueSize)

	signKeys, engines, rulesCfgs, rulesPaths := a.buildRuleRuntime(a.Config)
	a.SyncCfg = buildSyncConfig(a.Config)
	a.Engines = engines
	a.RulesCfgs = rulesCfgs
	a.RulesPaths = rulesPaths

	a.Checker = consumer.NewChecker(a.Client, a.Store, signKeys, a.Engines)
	return nil
}

func buildSyncConfig(cfg *config.Config) budget.SyncConfig {
	workers := cfg.Sync.Workers
	if workers <= 0 {
		workers = 10
	}
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
	timeoutMinutes := cfg.Sync.TimeoutMinutes
	if timeoutMinutes < 15 {
		timeoutMinutes = 15
	}
	return budget.SyncConfig{Targets: targets, Workers: workers, TimeoutMinutes: timeoutMinutes}
}

func (a *App) buildRuleRuntime(cfg *config.Config) (map[string]string, map[string]*rules.Engine, map[string]*types.RulesConfig, map[string]string) {
	signKeys := make(map[string]string)
	engines := make(map[string]*rules.Engine)
	rulesCfgs := make(map[string]*types.RulesConfig)
	rulesPaths := make(map[string]string)

	for key, wh := range cfg.Webhooks {
		if wh.SignKey != "" {
			signKeys[key] = wh.SignKey
		}

		rulesPath := wh.Rules
		if rulesPath == "" {
			rulesPath = fmt.Sprintf("rules/%s.json", key)
		}
		if !filepath.IsAbs(rulesPath) && cfg.BaseDir != "" {
			rulesPath = filepath.Join(cfg.BaseDir, rulesPath)
		}

		rulesCfg, err := config.LoadRules(rulesPath)
		if err != nil {
			log.Printf("[Config] webhook=%s 规则文件不存在或加载失败: %v", key, err)
			continue
		}
		engine, err := rules.NewEngine(a.Store, a.Client, rulesCfg, cfg.DimensionNames)
		if err != nil {
			log.Printf("[Config] webhook=%s 规则编译失败: %v", key, err)
			continue
		}
		engines[key] = engine
		rulesCfgs[key] = rulesCfg
		rulesPaths[key] = rulesPath
		log.Printf("[Config] webhook=%s 规则引擎加载成功: %s", key, rulesPath)
	}
	return signKeys, engines, rulesCfgs, rulesPaths
}

func (a *App) reloadRuntimeConfig() error {
	if a.Config.ConfigPath == "" {
		return nil
	}
	nextCfg, err := config.LoadConfig(a.Config.ConfigPath)
	if err != nil {
		return fmt.Errorf("热重载配置失败: %w", err)
	}
	if nextCfg.Ekb.Host != a.Config.Ekb.Host || nextCfg.Ekb.AppKey != a.Config.Ekb.AppKey || nextCfg.Ekb.AppSecret != a.Config.Ekb.AppSecret {
		a.Client = ekb.NewClient(nextCfg.Ekb.Host, nextCfg.Ekb.AppKey, nextCfg.Ekb.AppSecret)
	}

	signKeys, engines, rulesCfgs, rulesPaths := a.buildRuleRuntime(nextCfg)
	*a.Config = *nextCfg
	a.SyncCfg = buildSyncConfig(nextCfg)
	a.Engines = engines
	a.RulesPaths = rulesPaths
	replaceRulesCfgs(a.RulesCfgs, rulesCfgs)
	a.Checker.ReplaceRuntime(a.Client, signKeys, engines)
	log.Printf("[Config] 热重载完成: webhooks=%d targets=%d rules=%d", len(a.Config.Webhooks), len(a.SyncCfg.Targets), len(a.RulesCfgs))
	return nil
}

func replaceRulesCfgs(dst, src map[string]*types.RulesConfig) {
	for key := range dst {
		delete(dst, key)
	}
	for key, value := range src {
		dst[key] = value
	}
}

// Sync 手动触发一次预算同步（带锁保护）
func (a *App) Sync() error {
	if !a.Syncing.CompareAndSwap(false, true) {
		return ErrSyncAlreadyRunning
	}
	start := time.Now()
	a.SyncStartedAt.Store(start.Unix())
	defer func() {
		a.SyncStartedAt.Store(0)
		a.Syncing.Store(false)
	}()
	if err := a.reloadRuntimeConfig(); err != nil {
		return err
	}
	timeout := time.Duration(a.SyncCfg.TimeoutMinutes) * time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	err := budget.Sync(ctx, a.Store, a.Client, a.SyncCfg)
	duration := time.Since(start)
	a.LastSyncDuration.Store(int64(duration))
	metrics.SyncDuration.Observe(duration.Seconds())
	if err != nil {
		metrics.SyncTotal.WithLabelValues("error").Inc()
		return err
	}
	metrics.SyncTotal.WithLabelValues("success").Inc()
	metrics.LastSyncTimestamp.Set(float64(time.Now().Unix()))
	return nil
}

func (a *App) processTask(task types.Task) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Consumer] panic: %v, taskID=%s", r, task.ID)
			a.Checker.AddHistory(task.Code, "panic", fmt.Sprintf("任务处理异常: %v", r))
		}
	}()
	action, comment := a.Checker.Evaluate(task)
	if err := a.Checker.CallbackApproval(task, action, comment); err != nil {
		failComment := fmt.Sprintf("%s；回调审批失败: %v", comment, err)
		log.Printf("[Consumer] 回调审批失败: taskID=%s code=%s flowID=%s action=%s error=%v", task.ID, task.Code, task.FlowID, action, err)
		a.Checker.AddHistory(task.Code, "callback_error", failComment)
	} else {
		a.Checker.AddHistory(task.Code, action, comment)
	}
}

// CreateWebhook 创建新 webhook：写入内存 + 规则文件 + 更新 Checker + 持久化配置
func (a *App) CreateWebhook(key, signKey string) error {
	if key == "" {
		return fmt.Errorf("webhook key 不能为空")
	}
	if signKey == "" {
		return fmt.Errorf("sign_key 不能为空")
	}
	if _, exists := a.Config.Webhooks[key]; exists {
		return fmt.Errorf("webhook %s 已存在", key)
	}

	// 初始化 maps（如果为 nil）
	if a.Engines == nil {
		a.Engines = make(map[string]*rules.Engine)
	}
	if a.RulesCfgs == nil {
		a.RulesCfgs = make(map[string]*types.RulesConfig)
	}
	if a.RulesPaths == nil {
		a.RulesPaths = make(map[string]string)
	}

	// 创建空规则文件（相对路径用于配置持久化，绝对路径用于实际读写）
	relRulesPath := fmt.Sprintf("rules/%s.json", key)
	absRulesPath := relRulesPath
	if a.Config.BaseDir != "" {
		absRulesPath = filepath.Join(a.Config.BaseDir, relRulesPath)
	}
	emptyRules := &types.RulesConfig{Version: 1, Targets: []types.RuleTarget{}}
	if err := config.SaveRules(absRulesPath, emptyRules); err != nil {
		return fmt.Errorf("创建规则文件失败: %w", err)
	}

	// 编译空引擎
	engine, err := rules.NewEngine(a.Store, a.Client, emptyRules, a.Config.DimensionNames)
	if err != nil {
		return fmt.Errorf("规则引擎编译失败: %w", err)
	}

	// 写入内存（配置用相对路径，运行时用绝对路径）
	a.Config.Webhooks[key] = config.WebhookEntry{
		SignKey: signKey,
		Targets: []config.BudgetTarget{},
		Rules:   relRulesPath,
	}
	a.Engines[key] = engine
	a.RulesCfgs[key] = emptyRules
	a.RulesPaths[key] = absRulesPath
	a.Checker.AddSignKey(key, signKey)
	a.Checker.UpdateEngine(key, engine)

	// 持久化配置
	if err := config.SaveConfig(a.Config); err != nil {
		log.Printf("[Webhook] 警告: webhook %s 已创建但配置保存失败: %v", key, err)
	}

	log.Printf("[Webhook] 新建 webhook: key=%s signKey=%s rules=%s", key, signKey, absRulesPath)
	return nil
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
		for {
			if err := a.Sync(); err != nil {
				log.Printf("[Init] 首次同步失败，暂不启动消费队列: %v", err)
				time.Sleep(time.Duration(a.Config.Sync.IntervalMinutes) * time.Minute)
				continue
			}
			break
		}
		log.Println("[Init] 首次同步完成，开始消费队列")

		// 消费循环
		go func() {
			var pausedLogAt time.Time
			for {
				if a.Store.HasMissingTargets() {
					if time.Since(pausedLogAt) >= 10*time.Second {
						log.Printf("[Consumer] 配置异常，暂停消费队列；webhook 仍可入队，待修复预算目标配置后恢复")
						pausedLogAt = time.Now()
					}
					time.Sleep(time.Second)
					continue
				}
				task := <-a.Queue.Chan()
				if a.Store.HasMissingTargets() {
					if !a.Queue.Enqueue(task) {
						log.Printf("[Consumer] 配置异常，任务重新入队失败: taskID=%s code=%s", task.ID, task.Code)
					}
					continue
				}
				a.processTask(task)
			}
		}()

		// 定时同步
		for {
			time.Sleep(time.Duration(a.Config.Sync.IntervalMinutes) * time.Minute)
			if err := a.Sync(); err != nil {
				log.Printf("[Sync] 定时同步失败，继续使用上次成功缓存: %v", err)
			}
		}
	}()

	mux := http.NewServeMux()
	tokenStore := web.NewTokenStore()
	web.Register(mux, web.Deps{
		Config:            a.Config,
		Store:             a.Store,
		Checker:           a.Checker,
		TokenStore:        tokenStore,
		Queue:             a.Queue,
		Syncing:           a.Syncing.Load,
		Version:           a.Version,
		OnSync:            a.Sync,
		RulesCfgs:         a.RulesCfgs,
		SaveRulesFunc:     a.SaveRules,
		CreateWebhookFunc: a.CreateWebhook,
		StartTime:         a.StartTime,
		LastSyncDuration:  &a.LastSyncDuration,
		SyncStartedAt:     &a.SyncStartedAt,
		Client:            a.Client,
	})

	port := a.Config.Server.Port
	maxRetries := 1
	if allowPortFallback() {
		maxRetries = 10
	}
	for i := 0; i < maxRetries; i++ {
		addr := fmt.Sprintf("0.0.0.0:%d", port)
		log.Printf("服务启动: %s", addr)

		// 先尝试监听端口
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			if i < maxRetries-1 {
				log.Printf("[WARN] 端口 %d 被占用，测试环境尝试端口 %d", port, port+1)
				port++
				continue
			}
			return fmt.Errorf("无法启动服务，端口 %d 不可用: %w", a.Config.Server.Port, err)
		}

		// 监听成功，启动 HTTP 服务
		return http.Serve(listener, mux)
	}

	return fmt.Errorf("无法启动服务，已尝试 %d 次", maxRetries)
}

func allowPortFallback() bool {
	return os.Getenv("BUDGET_ALLOW_PORT_FALLBACK") == "1" || os.Getenv("APP_ENV") == "test"
}
