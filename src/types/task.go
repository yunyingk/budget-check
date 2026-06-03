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
	Version     int          `json:"version"`
	GlobalSteps []Step       `json:"global_steps,omitempty"` // 单据级别前置规则
	SplitMode   string       `json:"split_mode,omitempty"`   // ""=不拆, "detail"=拆明细, "apportion"=拆明细+分摊
	Targets     []RuleTarget `json:"targets"`
}
