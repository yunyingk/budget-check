package types

import "time"

// Task 校验任务
type Task struct {
	ID         string
	Code       string
	FlowID     string
	NodeID     string
	EnqueuedAt time.Time
	ClientIP   string
}

// Step 规则步骤
type Step struct {
	When   string `yaml:"when"`
	Then   string `yaml:"then"`
	Action string `yaml:"action"`
}

// RuleTarget 规则目标
type RuleTarget struct {
	ID    string `yaml:"id"`
	Name  string `yaml:"name"`
	Steps []Step `yaml:"steps"`
}

// RulesConfig 规则配置
type RulesConfig struct {
	Targets []RuleTarget `yaml:"targets"`
}
