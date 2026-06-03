package config

import (
	"budget/src/types"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadConfig 从指定路径加载 YAML 配置
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

// LoadRules 从 JSON 文件加载规则配置
func LoadRules(path string) (*types.RulesConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取规则文件失败: %w", err)
	}
	var rules types.RulesConfig
	if err := json.Unmarshal(data, &rules); err != nil {
		return nil, fmt.Errorf("解析规则文件失败: %w", err)
	}
	return &rules, nil
}
