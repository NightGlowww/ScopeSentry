package watchdog

import (
	"testing"
	"time"
)

// stallDecision 是「活着的节点卡在某个模块」这条判定的唯一口径，
// 抽成纯函数是为了能在这里钉住边界（watchdog 其余部分都要连 Mongo/Redis）。
func TestStallDecision(t *testing.T) {
	now := time.Date(2026, 11, 29, 1, 30, 0, 0, time.UTC)
	threshold := 60 * time.Minute

	cases := []struct {
		name    string
		last    time.Time
		want    bool
		wantDur time.Duration
	}{
		{"没有进度写入（Redis 里查不到）不判定", time.Time{}, false, 0},
		{"刚写过进度", now.Add(-5 * time.Minute), false, 5 * time.Minute},
		{"差一秒到窗口", now.Add(-threshold + time.Second), false, threshold - time.Second},
		{"刚好到窗口", now.Add(-threshold), true, threshold},
		{"远超窗口（整夜卡住）", now.Add(-8 * time.Hour), true, 8 * time.Hour},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, dur := stallDecision(now, c.last, threshold)
			if got != c.want {
				t.Fatalf("stalled = %v, want %v", got, c.want)
			}
			if c.want && dur != c.wantDur {
				t.Fatalf("elapsed = %v, want %v", dur, c.wantDur)
			}
		})
	}
}

func TestStalledAfterEnv(t *testing.T) {
	t.Setenv("WATCHDOG_STALLED_AFTER_MIN", "15")
	if got := stalledAfter(); got != 15*time.Minute {
		t.Fatalf("stalledAfter = %v, want 15m", got)
	}
	t.Setenv("WATCHDOG_STALLED_AFTER_MIN", "")
	if got := stalledAfter(); got != 60*time.Minute {
		t.Fatalf("默认窗口 = %v, want 60m", got)
	}
}
