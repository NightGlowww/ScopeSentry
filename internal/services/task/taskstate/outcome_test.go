package taskstate

import (
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/constants"
)

// 终态 + 有快照 → 用快照，且不调用实时统计。
func TestOutcomeForUsesFrozenStats(t *testing.T) {
	frozen := map[string]int64{"asset": 12, "subdomain": 34}
	called := false
	got := OutcomeFor(constants.TaskStatusFinished, frozen, func() map[string]int64 {
		called = true
		return map[string]int64{"asset": 999}
	})
	if called {
		t.Fatal("terminal task with snapshot must not hit live counts")
	}
	if got["asset"] != 12 || got["subdomain"] != 34 {
		t.Fatalf("got %v, want frozen", got)
	}
}

// 运行中 / 没有快照 → 回退实时统计。
func TestOutcomeForFallsBack(t *testing.T) {
	for _, status := range []int{constants.TaskStatusRunning, constants.TaskStatusPending, constants.TaskStatusFinished} {
		calls := 0
		got := OutcomeFor(status, nil, func() map[string]int64 {
			calls++
			return map[string]int64{"asset": 7}
		})
		if calls != 1 || got["asset"] != 7 {
			t.Fatalf("status=%d got %v calls=%d, want live once", status, got, calls)
		}
	}
}

// 失败任务同样用快照：失败时的产出也是事实，不该随后续清理漂移。
func TestOutcomeForFailedTaskUsesSnapshot(t *testing.T) {
	got := OutcomeFor(constants.TaskStatusFailed, map[string]int64{"vulnerability": 3}, func() map[string]int64 {
		t.Fatal("must not call live")
		return nil
	})
	if got["vulnerability"] != 3 {
		t.Fatalf("got %v", got)
	}
}
