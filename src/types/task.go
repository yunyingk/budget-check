package types

import "time"

// Task 校验任务
type Task struct {
	ID         string
	Code       string
	FlowID     string
	NodeID     string
	WebhookKey string // 来源 webhook 标识（如 budget-check）
	EnqueuedAt time.Time
	ClientIP   string
}

// Step 规则步骤
type Step struct {
	When        string `json:"when,omitempty"`
	Then        string `json:"then,omitempty"`
	Action      string `json:"action,omitempty"`
	Reason      string `json:"reason,omitempty"`      // 拒绝原因说明（then=refuse 时返回）
	Description string `json:"description,omitempty"` // 步骤描述，仅用于配置注释
}

// RuleTarget 规则目标，对应一个预算包，有自己的完整工作流
type RuleTarget struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Steps []Step `json:"steps"` // 工作流步骤（顺序执行）
}

// RulesConfig 规则配置
type RulesConfig struct {
	Version int          `json:"version"`
	Targets []RuleTarget `json:"targets"`
}
