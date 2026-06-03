package config

// ServerConfig HTTP 服务配置
type ServerConfig struct {
	Port int `yaml:"port"`
}

// EkbConfig 合思 API 配置
type EkbConfig struct {
	Host      string `yaml:"host"`
	AppKey    string `yaml:"app_key"`
	AppSecret string `yaml:"app_secret"`
}

// BudgetTarget 预算包目标（配置文件中引用）
type BudgetTarget struct {
	ID   string `yaml:"id" json:"id"`
	Name string `yaml:"name" json:"name"`
}

// WebhookEntry 单个 webhook 配置
type WebhookEntry struct {
	SignKey string         `yaml:"sign_key"`
	Targets []BudgetTarget `yaml:"targets"`
	Rules   string         `yaml:"rules"`
}

// SyncConfig 预算同步配置
type SyncConfig struct {
	IntervalMinutes int    `yaml:"interval_minutes"`
	Workers         int    `yaml:"workers"`
	Password        string `yaml:"password"`
	QueueSize       int    `yaml:"queue_size"`
}

// LogConfig 日志配置
type LogConfig struct {
	Level    string `yaml:"level"`
	Rotation string `yaml:"rotation"`
}

// WebConfig Web 管理页面配置
type WebConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Password string `yaml:"password"`
}

// Config 全局运行时配置
type Config struct {
	Server       ServerConfig            `yaml:"server"`
	Ekb          EkbConfig               `yaml:"ekuaibao"`
	Webhooks     map[string]WebhookEntry `yaml:"webhooks"`
	Sync         SyncConfig              `yaml:"sync"`
	Logging      LogConfig               `yaml:"logging"`
	Web          WebConfig               `yaml:"web"`
}
