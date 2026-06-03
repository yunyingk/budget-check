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
	When   string `json:"when,omitempty"`
	Then   string `json:"then,omitempty"`
	Action string `json:"action,omitempty"`
}

// RuleTarget 规则目标，对应一个预算包
type RuleTarget struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Steps []Step `json:"steps"`
}

// RulesConfig 规则配置
type RulesConfig struct {
	Version int          `json:"version"`
	Targets []RuleTarget `json:"targets"`
}
