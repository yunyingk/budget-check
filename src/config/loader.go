package config

import (
	"budget/src/types"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/expr-lang/expr"
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
		cfg, err := parseConfig(data)
		if err != nil {
			return nil, err
		}
		cfg.BaseDir = filepath.Dir(resolveAbs(path))
		cfg.ConfigPath = resolveAbs(path)
		return cfg, nil
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
			cfg, err := parseConfig(data)
			if err != nil {
				return nil, err
			}
			cfg.BaseDir = filepath.Dir(resolveAbs(p))
			cfg.ConfigPath = resolveAbs(p)
			return cfg, nil
		}
	}
	return nil, fmt.Errorf("未找到配置文件，已搜索: %v", searchPaths)
}

// resolveAbs 将路径转为绝对路径，失败则返回原路径
func resolveAbs(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
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

	// 验证规则语法
	if err := ValidateRules(&rules); err != nil {
		return nil, fmt.Errorf("规则验证失败: %w", err)
	}

	return &rules, nil
}

// SaveConfig 将配置写回 YAML 文件
func SaveConfig(cfg *Config) error {
	if cfg.ConfigPath == "" {
		return fmt.Errorf("配置文件路径未知，无法保存")
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	if err := os.WriteFile(cfg.ConfigPath, data, 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}
	return nil
}

// SaveRules 验证并保存规则配置到 JSON 文件
func SaveRules(path string, cfg *types.RulesConfig) error {
	if err := ValidateRules(cfg); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化规则失败: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("写入规则文件失败: %w", err)
	}
	return nil
}

// ValidateRules 验证规则配置的语法
func ValidateRules(cfg *types.RulesConfig) error {
	for _, target := range cfg.Targets {
		if err := validateTarget(target); err != nil {
			return fmt.Errorf("target %s: %w", target.Name, err)
		}
	}
	return nil
}

// validateTarget 验证单个 target 的规则
func validateTarget(target types.RuleTarget) error {
	seenSplitDetail := false

	for i, step := range target.Steps {
		// 验证 when 表达式语法
		if step.When != "" {
			if _, err := expr.Compile(step.When, expr.AllowUndefinedVariables()); err != nil {
				return fmt.Errorf("step %d: when 表达式语法错误 (%q): %w", i+1, step.When, err)
			}
		}

		// 验证 step 顺序
		if step.Action == "split_detail" {
			seenSplitDetail = true
		}
		if step.Action == "split_apportion" {
			if !seenSplitDetail {
				return fmt.Errorf("step %d: split_apportion 必须在 split_detail 之后", i+1)
			}
		}

		// 验证 then 值
		if step.Then != "" && step.Then != "pass" && step.Then != "refuse" {
			return fmt.Errorf("step %d: then 值无效 (%q)，必须是 pass 或 refuse", i+1, step.Then)
		}

		// 验证 action 值
		validActions := map[string]bool{
			"":                    true,
			"split_detail":        true,
			"split_apportion":     true,
			"match_info_to_budget": true,
		}
		if !validActions[step.Action] {
			return fmt.Errorf("step %d: action 值无效 (%q)", i+1, step.Action)
		}
	}

	return nil
}
