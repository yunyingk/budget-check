package app

import (
	"testing"
	"time"
)

// TestNextSyncBackoff 验证退避序列：失败次数递增时间隔翻倍，封顶于 syncMaxBackoff，
// 且不超过 normalInterval；0 次失败（正常）直接用正常间隔。
func TestNextSyncBackoff(t *testing.T) {
	normal := 60 * time.Minute

	tests := []struct {
		name     string
		fails    int
		normal   time.Duration
		want     time.Duration
	}{
		{"正常（0次失败）", 0, normal, normal},
		{"第1次失败", 1, normal, 1 * time.Minute},
		{"第2次失败", 2, normal, 2 * time.Minute},
		{"第3次失败", 3, normal, 4 * time.Minute},
		{"第4次失败", 4, normal, 8 * time.Minute},
		{"第5次失败（封顶）", 5, normal, 10 * time.Minute},
		{"第10次失败（仍封顶）", 10, normal, 10 * time.Minute},
		{"normalInterval 较小，退避不应超过它", 5, 5 * time.Minute, 5 * time.Minute},
		{"normalInterval 为 0（异常防御）", 3, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nextSyncBackoff(tt.fails, tt.normal)
			if got != tt.want {
				t.Errorf("nextSyncBackoff(%d, %v) = %v, want %v", tt.fails, tt.normal, got, tt.want)
			}
		})
	}
}

// TestNextSyncBackoffMonotonic 退避序列单调递增（直到封顶）
func TestNextSyncBackoffMonotonic(t *testing.T) {
	normal := 60 * time.Minute
	prev := time.Duration(0)
	for fails := 1; fails <= 10; fails++ {
		got := nextSyncBackoff(fails, normal)
		if got < prev {
			t.Errorf("fails=%d 退避 %v 小于上一次 %v，应单调递增", fails, got, prev)
		}
		prev = got
	}
}
