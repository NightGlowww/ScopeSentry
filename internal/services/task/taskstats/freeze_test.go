package taskstats

import (
	"context"
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/constants"
	"go.mongodb.org/mongo-driver/bson"
)

// 任务名为空绝不能统计：空条件等于匹配全库（会把全库数量写成某个任务的产出）。
func TestFreezeRejectsEmptyName(t *testing.T) {
	for _, name := range []string{"", "   "} {
		if got := Freeze(context.Background(), name); got != nil {
			t.Fatalf("name=%q got %v, want nil", name, got)
		}
	}
}

// Merge 必须 nil 安全，并且把 stats/statsFrozenAt 并进同一个 $set。
func TestMerge(t *testing.T) {
	frozen := bson.M{"stats": bson.M{"asset": 1}, "statsFrozenAt": "2026-11-28 00:00:00"}
	set := Merge(bson.M{"status": 4}, frozen)
	if set["status"] != 4 || set["statsFrozenAt"] != "2026-11-28 00:00:00" {
		t.Fatalf("set = %v", set)
	}
	if _, ok := set["stats"]; !ok {
		t.Fatalf("stats missing: %v", set)
	}
	// nil frozen 不该改变原文档
	if got := Merge(bson.M{"status": 3}, nil); len(got) != 1 || got["status"] != 3 {
		t.Fatalf("nil merge = %v", got)
	}
	// nil set 时新建 map
	if got := Merge(nil, frozen); got["statsFrozenAt"] == nil {
		t.Fatalf("nil set merge = %v", got)
	}
}

func TestIsTerminal(t *testing.T) {
	if !IsTerminal(constants.TaskStatusFinished) || !IsTerminal(constants.TaskStatusFailed) {
		t.Fatal("finished/failed must be terminal")
	}
	for _, s := range []int{constants.TaskStatusPending, constants.TaskStatusRunning, constants.TaskStatusPaused} {
		if IsTerminal(s) {
			t.Fatalf("status %d must not be terminal", s)
		}
	}
}
