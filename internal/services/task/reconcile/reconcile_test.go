package reconcile

import (
	"strings"
	"testing"
	"time"
)

// 终态收敛的纯判断：没有进度信号不动；刚写完进度不动；超过宽限期才收尾。
func TestShouldFinalizeStale(t *testing.T) {
	now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.Local)
	if ShouldFinalizeStale(time.Time{}, now, staleFinalizeGrace) {
		t.Fatal("zero lastEnd must not finalize")
	}
	if ShouldFinalizeStale(now.Add(-10*time.Minute), now, staleFinalizeGrace) {
		t.Fatal("fresh progress must not finalize")
	}
	if !ShouldFinalizeStale(now.Add(-31*time.Minute), now, staleFinalizeGrace) {
		t.Fatal("stale progress should finalize")
	}
}

// 说明里必须带「最后进度时间 + 百分比」：用户要看的是跑到哪一步没信号了。
func TestStaleFinalizeReason(t *testing.T) {
	last := time.Date(2026, 11, 28, 1, 56, 12, 0, time.Local)
	got := StaleFinalizeReason(92, last)
	if !strings.Contains(got, "2026-11-28 01:56:12") || !strings.Contains(got, "92%") {
		t.Fatalf("reason = %q, want last end + percent", got)
	}
}
