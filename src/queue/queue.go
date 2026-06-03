package queue

import (
	"budget/src/types"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// Queue 任务队列
type Queue struct {
	ch chan types.Task
}

// New 创建指定容量的队列
func New(size int) *Queue {
	return &Queue{ch: make(chan types.Task, size)}
}

// Enqueue 入队，队列满时返回 false
func (q *Queue) Enqueue(t types.Task) bool {
	select {
	case q.ch <- t:
		return true
	default:
		return false
	}
}

// Chan 返回队列 channel，供消费端 range
func (q *Queue) Chan() <-chan types.Task {
	return q.ch
}

// Len 当前队列长度
func (q *Queue) Len() int {
	return len(q.ch)
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
