package exportfilter

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

func TestApplyTimeRangeLeavesQueryUntouchedWhenEmpty(t *testing.T) {
	base := map[string]interface{}{"host": "example.com"}
	got, err := ApplyTimeRange(base, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got["host"] != "example.com" {
		t.Fatalf("query changed: %+v", got)
	}
}

func TestApplyTimeRangeBuildsRange(t *testing.T) {
	got, err := ApplyTimeRange(nil, "2026-01-01 00:00:00", "2026-01-31 23:59:59")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rng, ok := got["time"].(bson.M)
	if !ok {
		t.Fatalf("time clause missing: %+v", got)
	}
	if rng["$gte"] != "2026-01-01 00:00:00" || rng["$lte"] != "2026-01-31 23:59:59" {
		t.Fatalf("range = %+v", rng)
	}
}

func TestApplyTimeRangeSupportsOpenEnds(t *testing.T) {
	startOnly, err := ApplyTimeRange(nil, "2026-01-01 00:00:00", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rng := startOnly["time"].(bson.M)
	if _, has := rng["$lte"]; has {
		t.Fatalf("unexpected upper bound: %+v", rng)
	}

	endOnly, err := ApplyTimeRange(nil, "", "2026-01-01 00:00:00")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rng = endOnly["time"].(bson.M)
	if _, has := rng["$gte"]; has {
		t.Fatalf("unexpected lower bound: %+v", rng)
	}
}

func TestApplyTimeRangeMergesWithExistingTimeFilter(t *testing.T) {
	base := map[string]interface{}{"time": bson.M{"$gte": "2025-01-01 00:00:00"}, "host": "a.example.com"}
	got, err := ApplyTimeRange(base, "2026-01-01 00:00:00", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	and, ok := got["$and"].([]interface{})
	if !ok || len(and) != 2 {
		t.Fatalf("expected $and with 2 clauses, got %+v", got)
	}
	// 原来的 time 条件必须保留，不能被导出范围覆盖
	first := and[0].(bson.M)
	if _, ok := first["time"]; !ok {
		t.Fatalf("existing time filter lost: %+v", first)
	}
}

func TestApplyTimeRangeRejectsBadInput(t *testing.T) {
	if _, err := ApplyTimeRange(nil, "2026/01/01", ""); err == nil {
		t.Fatal("expected error for bad start format")
	}
	if _, err := ApplyTimeRange(nil, "", "not-a-time"); err == nil {
		t.Fatal("expected error for bad end format")
	}
	if _, err := ApplyTimeRange(nil, "2026-02-01 00:00:00", "2026-01-01 00:00:00"); err == nil {
		t.Fatal("expected error when start is after end")
	}
}
