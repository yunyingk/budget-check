package main

import (
	"budget/src/types"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// taskQueue 全局任务队列
var taskQueue chan types.Task

// InitQueue 初始化队列
func InitQueue(size int) {
	taskQueue = make(chan types.Task, size)
}

// Enqueue 入队
func Enqueue(task types.Task) bool {
	select {
	case taskQueue <- task:
		return true
	default:
		return false
	}
}

// QueueChan 返回队列 channel，供消费端 range
func QueueChan() <-chan types.Task {
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
