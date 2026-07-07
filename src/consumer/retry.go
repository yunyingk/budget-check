package consumer

import (
	"budget/src/types"
	"fmt"
	"log"
	"sync"
	"time"
)

// 回调失败重试参数：
//   - 最多重试 3 次，间隔递增（1m → 3m → 10m）
//   - 仍失败则放弃，记 callback_dropped 历史，需人工补审批
// 重试只重试 CallbackApproval，不重新 Evaluate，避免在合思宕机期间反复打"拉单据"接口。
const (
	maxCallbackRetries = 3
)

var callbackRetryDelay = []time.Duration{
	1 * time.Minute,
	3 * time.Minute,
	10 * time.Minute,
}

// retryItem 一个待重试的回调任务
type retryItem struct {
	Task        types.Task
	Action      string // 第一次 Evaluate 算好的结果，重试时直接复用
	Comment     string
	Attempt     int       // 已重试次数（0 表示刚入桶还没重试过）
	NextRetryAt time.Time // 下次可重试的时间
}

// RetryBucket 回调失败重试桶。
// 独立于主 Queue，避免失败任务和新任务互相污染（你担心的"无限队列"不会发生）。
type RetryBucket struct {
	mu      sync.Mutex
	items   []retryItem
	checker *Checker
}

// NewRetryBucket 创建重试桶
func NewRetryBucket(checker *Checker) *RetryBucket {
	return &RetryBucket{checker: checker}
}

// Add 把一个回调失败的任务加入重试桶。
// action/comment 是第一次 Evaluate 算好的结果，重试时直接复用，不再重新跑规则。
func (b *RetryBucket) Add(task types.Task, action, comment string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.items = append(b.items, retryItem{
		Task:        task,
		Action:      action,
		Comment:     comment,
		Attempt:     0,
		NextRetryAt: time.Now().Add(callbackRetryDelay[0]),
	})
	log.Printf("[Retry] 任务入重试桶: taskID=%s code=%s webhook=%s action=%s 下次重试=%v",
		task.ID, task.Code, task.WebhookKey, action, time.Now().Add(callbackRetryDelay[0]).Format("15:04:05"))
}

// Len 当前待重试任务数
func (b *RetryBucket) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.items)
}

// Run 启动重试循环，每 scanInterval 扫描一次桶，到点的任务重试回调。
// 阻塞调用，应在独立 goroutine 中运行。
func (b *RetryBucket) Run(scanInterval time.Duration) {
	ticker := time.NewTicker(scanInterval)
	defer ticker.Stop()
	for range ticker.C {
		b.scan()
	}
}

// scan 扫描一次桶，重试所有到点的任务
func (b *RetryBucket) scan() {
	now := time.Now()
	b.mu.Lock()
	if len(b.items) == 0 {
		b.mu.Unlock()
		return
	}
	var due []retryItem
	var keep []retryItem
	for _, it := range b.items {
		if now.Before(it.NextRetryAt) {
			keep = append(keep, it)
		} else {
			due = append(due, it)
		}
	}
	b.items = keep
	b.mu.Unlock()

	for _, it := range due {
		b.retry(it)
	}
}

// retry 重试单个任务。成功则消化掉；失败则按次数推进下一次重试时间，或放弃。
func (b *RetryBucket) retry(it retryItem) {
	err := b.checker.CallbackApproval(it.Task, it.Action, it.Comment)
	if err == nil {
		log.Printf("[Retry] 回调重试成功: taskID=%s code=%s attempt=%d", it.Task.ID, it.Task.Code, it.Attempt+1)
		b.checker.AddHistory(it.Task.Code, it.Action, fmt.Sprintf("重试回调成功（第 %d 次）: %s", it.Attempt+1, it.Comment))
		return
	}

	it.Attempt++
	if it.Attempt >= maxCallbackRetries {
		// 放弃，需人工补审批
		log.Printf("[Retry] 回调重试 %d 次仍失败，放弃，需人工补审批: taskID=%s code=%s flowID=%s action=%s err=%v",
			it.Attempt, it.Task.ID, it.Task.Code, it.Task.FlowID, it.Action, err)
		b.checker.AddHistory(it.Task.Code, "callback_dropped",
			fmt.Sprintf("回调重试 %d 次仍失败，需人工补审批。action=%s comment=%s 最后错误: %v",
				it.Attempt, it.Action, it.Comment, err))
		return
	}

	// 还有重试机会，按次数推迟下次重试
	delay := callbackRetryDelay[it.Attempt]
	it.NextRetryAt = time.Now().Add(delay)
	log.Printf("[Retry] 回调重试失败，%v 后再试: taskID=%s code=%s attempt=%d err=%v",
		delay, it.Task.ID, it.Task.Code, it.Attempt, err)
	b.mu.Lock()
	b.items = append(b.items, it)
	b.mu.Unlock()
}
