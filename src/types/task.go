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
