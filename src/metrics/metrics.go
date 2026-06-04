package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

var (
	// ChecksTotal 预算校验总次数
	ChecksTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "budget_checks_total",
			Help: "Total number of budget checks",
		},
		[]string{"webhook", "action"}, // action: accept, refuse
	)

	// CheckDuration 预算校验延迟
	CheckDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "budget_check_duration_seconds",
			Help:    "Budget check duration in seconds",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10},
		},
		[]string{"webhook"},
	)

	// QueueSize 队列容量
	QueueSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "budget_queue_size",
			Help: "Queue capacity",
		},
	)

	// QueuePending 队列待处理数
	QueuePending = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "budget_queue_pending",
			Help: "Number of pending items in queue",
		},
	)

	// SyncTotal 同步次数
	SyncTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "budget_sync_total",
			Help: "Total number of budget syncs",
		},
		[]string{"status"}, // status: success, error
	)

	// SyncDuration 同步延迟
	SyncDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "budget_sync_duration_seconds",
			Help:    "Budget sync duration in seconds",
			Buckets: []float64{1, 5, 10, 30, 60, 120},
		},
	)

	// LastSyncTimestamp 最后同步时间戳
	LastSyncTimestamp = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "budget_last_sync_timestamp",
			Help: "Timestamp of last successful sync",
		},
	)
)

func init() {
	prometheus.MustRegister(
		ChecksTotal,
		CheckDuration,
		QueueSize,
		QueuePending,
		SyncTotal,
		SyncDuration,
		LastSyncTimestamp,
	)
}

// GetMetrics 获取所有指标的当前值
func GetMetrics() map[string]any {
	result := map[string]any{}

	// 校验次数（按 webhook 和 action 分组）
	checks := map[string]map[string]float64{}
	metricCh := make(chan prometheus.Metric, 10)
	go func() {
		ChecksTotal.Collect(metricCh)
		close(metricCh)
	}()
	for m := range metricCh {
		dto := &dto.Metric{}
		m.Write(dto)
		labels := map[string]string{}
		for _, l := range dto.GetLabel() {
			labels[l.GetName()] = l.GetValue()
		}
		webhook := labels["webhook"]
		action := labels["action"]
		if checks[webhook] == nil {
			checks[webhook] = map[string]float64{}
		}
		checks[webhook][action] = dto.GetCounter().GetValue()
	}
	result["checks"] = checks

	// 同步次数
	syncs := map[string]float64{}
	syncCh := make(chan prometheus.Metric, 10)
	go func() {
		SyncTotal.Collect(syncCh)
		close(syncCh)
	}()
	for m := range syncCh {
		dto := &dto.Metric{}
		m.Write(dto)
		labels := map[string]string{}
		for _, l := range dto.GetLabel() {
			labels[l.GetName()] = l.GetValue()
		}
		status := labels["status"]
		syncs[status] = dto.GetCounter().GetValue()
	}
	result["syncs"] = syncs

	// 队列状态
	queueSizeDto := &dto.Metric{}
	QueueSize.Write(queueSizeDto)
	queuePendingDto := &dto.Metric{}
	QueuePending.Write(queuePendingDto)
	result["queue"] = map[string]float64{
		"size":    queueSizeDto.GetGauge().GetValue(),
		"pending": queuePendingDto.GetGauge().GetValue(),
	}

	// 最后同步时间戳
	lastSyncDto := &dto.Metric{}
	LastSyncTimestamp.Write(lastSyncDto)
	result["last_sync_timestamp"] = lastSyncDto.GetGauge().GetValue()

	return result
}
