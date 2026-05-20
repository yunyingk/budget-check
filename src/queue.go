package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// CheckTask 校验任务
type CheckTask struct {
	ID         string
	Code       string
	FlowID     string
	NodeID     string
	EnqueuedAt time.Time
	ClientIP   string
}

// taskQueue 全局任务队列
var taskQueue chan CheckTask

// InitQueue 初始化队列
func InitQueue(size int) {
	taskQueue = make(chan CheckTask, size)
}

// Enqueue 入队
func Enqueue(task CheckTask) bool {
	select {
	case taskQueue <- task:
		return true
	default:
		return false
	}
}

// QueueChan 返回队列 channel，供消费端 range
func QueueChan() <-chan CheckTask {
	return taskQueue
}

// QueueLen 当前队列长度
func QueueLen() int {
	return len(taskQueue)
}

// GenTaskID 生成任务ID: YYMMDD-随机6位-单号后6位
func GenTaskID(code string) string {
	b := make([]byte, 3)
	rand.Read(b)
	date := time.Now().Format("060102")
	suffix := code
	if len(suffix) > 6 {
		suffix = suffix[len(suffix)-6:]
	} else if len(suffix) < 6 {
		suffix = fmt.Sprintf("%06s", suffix)
	}
	return fmt.Sprintf("%s-%s-%s", date, hex.EncodeToString(b), suffix)
}
