package main

import (
	"os"

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
	Name  string `yaml:"name"`
	Depth int    `yaml:"depth"`
}

type SyncConfig struct {
	IntervalMinutes int `yaml:"interval_minutes"`
}

type LogConfig struct {
	Level string `yaml:"level"`
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
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}