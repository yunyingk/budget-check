package main

import (
	"budget/src/types"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type ServerConfig struct {
	Port int `yaml:"port"`
}

type EkbConfig struct {
	Host      string `yaml:"host"`
	AppKey    string `yaml:"app_key"`
	AppSecret string `yaml:"app_secret"`
}

type BudgetTarget struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
}

type WebhookEntry struct {
	SignKey string         `yaml:"sign_key"`
	Targets []BudgetTarget `yaml:"targets"`
	Rules   string         `yaml:"rules"`
}

type SyncConfig struct {
	IntervalMinutes int    `yaml:"interval_minutes"`
	Workers         int    `yaml:"workers"`
	Password        string `yaml:"password"`
	QueueSize       int    `yaml:"queue_size"`
}

type LogConfig struct {
	Level    string `yaml:"level"`
	Rotation string `yaml:"rotation"`
}

type WebConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Password string `yaml:"password"`
}

type Config struct {
	Server    ServerConfig             `yaml:"server"`
	Ekb       EkbConfig                `yaml:"ekuaibao"`
	Webhooks  map[string]WebhookEntry  `yaml:"webhooks"`
	Sync      SyncConfig               `yaml:"sync"`
	Logging   LogConfig                `yaml:"logging"`
	Web       WebConfig                `yaml:"web"`
}

func LoadConfig(path string) (*Config, error) {
	// 如果指定了非默认路径，直接用
	if path != "config.yaml" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return parseConfig(data)
	}

	// 默认路径：优先 config/config.yaml，其次 config.yaml
	searchPaths := []string{
		filepath.Join("config", "config.yaml"),
		"config.yaml",
	}
	for _, p := range searchPaths {
		data, err := os.ReadFile(p)
		if err == nil {
			fmt.Printf("[Config] 使用配置文件: %s\n", p)
			return parseConfig(data)
		}
	}
	return nil, fmt.Errorf("未找到配置文件，已搜索: %v", searchPaths)
}

func parseConfig(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func LoadRules(path string) (*types.RulesConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取规则文件失败: %w", err)
	}
	var rules types.RulesConfig
	if err := yaml.Unmarshal(data, &rules); err != nil {
		return nil, fmt.Errorf("解析规则文件失败: %w", err)
	}
	return &rules, nil
}