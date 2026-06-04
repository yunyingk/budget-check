package metrics

import "github.com/prometheus/client_golang/prometheus"

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
