package webhook

import (
	"log"
	"time"
)

// Task 校验任务
type Task struct {
	ID         string
	Code       string
	FlowID     string
	NodeID     string
	EnqueuedAt time.Time
	ClientIP   string
}

// Process 处理校验任务
func Process(task Task) {
	log.Printf("[Task] 开始处理: taskID=%s code=%s flowId=%s nodeId=%s", task.ID, task.Code, task.FlowID, task.NodeID)
	// TODO: 业务校验逻辑
	log.Printf("[Task] 处理完成: taskID=%s (暂未实现业务逻辑)", task.ID)
}
