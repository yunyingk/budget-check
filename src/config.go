package main

import (
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
	ID    string `yaml:"id"`
	Name  string `yaml:"name"`
	Depth int    `yaml:"depth"`
}

type SyncConfig struct {
	IntervalMinutes int `yaml:"interval_minutes"`
}

type LogConfig struct {
	Level     string `yaml:"level"`
	Rotation  string `yaml:"rotation"`
}

type Config struct {
	Server        ServerConfig           `yaml:"server"`
	Ekb           EkbConfig              `yaml:"ekuaibao"`
	ExpenseNature map[string]string      `yaml:"expense_nature"`
	BudgetTargets []BudgetTarget         `yaml:"budget_targets"`
	Sync          SyncConfig             `yaml:"sync"`
	Logging       LogConfig              `yaml:"logging"`
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