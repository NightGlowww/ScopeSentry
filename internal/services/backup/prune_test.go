package backup

import (
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// 保留策略必须可预测：删备份不可逆，靠「现场点几下看看」验证不行。
func TestPrunePlan(t *testing.T) {
	list := []models.BackupInfo{
		{Name: "b1", CreatedAt: "2026-11-01T00:00:00Z"},
		{Name: "b3", CreatedAt: "2026-11-03T00:00:00Z"},
		{Name: "b2", CreatedAt: "2026-11-02T00:00:00Z"},
	}

	// keep=2：留下最新的 b3/b2，删 b1
	got := PrunePlan(list, 2)
	if len(got) != 1 || got[0] != "b1" {
		t.Fatalf("keep=2 got %v, want [b1]", got)
	}

	// keep=0：不清理
	if got := PrunePlan(list, 0); len(got) != 0 {
		t.Fatalf("keep=0 got %v, want empty", got)
	}

	// 数量不足：不动
	if got := PrunePlan(list, 5); len(got) != 0 {
		t.Fatalf("keep=5 got %v, want empty", got)
	}

	// 同时间戳时按名字倒序，保证结果稳定（不会每次删的不一样）
	same := []models.BackupInfo{
		{Name: "a", CreatedAt: "2026-11-01T00:00:00Z"},
		{Name: "c", CreatedAt: "2026-11-01T00:00:00Z"},
		{Name: "b", CreatedAt: "2026-11-01T00:00:00Z"},
	}
	got = PrunePlan(same, 1)
	if len(got) != 2 || got[0] != "b" || got[1] != "a" {
		t.Fatalf("tie-break got %v, want [b a]", got)
	}
}
