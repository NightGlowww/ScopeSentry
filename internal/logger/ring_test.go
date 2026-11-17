package logger

import (
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// 环形缓冲绕过一圈后要保留最近的条目，且顺序是时间升序
func TestRingBufferWrapsAndKeepsOrder(t *testing.T) {
	ring := newRing(3)
	for _, msg := range []string{"m1", "m2", "m3", "m4", "m5"} {
		ring.add(LogEntry{Message: msg})
	}

	snapshot := ring.snapshot()
	if len(snapshot) != 3 {
		t.Fatalf("snapshot len = %d, want 3", len(snapshot))
	}
	want := []string{"m3", "m4", "m5"}
	for i, w := range want {
		if snapshot[i].Message != w {
			t.Fatalf("snapshot[%d] = %q, want %q (all=%+v)", i, snapshot[i].Message, w, snapshot)
		}
	}
}

// 没绕过一圈时只返回已写入的条目
func TestRingBufferPartial(t *testing.T) {
	ring := newRing(5)
	ring.add(LogEntry{Message: "a"})
	ring.add(LogEntry{Message: "b"})

	snapshot := ring.snapshot()
	if len(snapshot) != 2 || snapshot[0].Message != "a" || snapshot[1].Message != "b" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

// ringCore 要真的把 zap 日志收进全局缓冲（dashboard/logs 的 memory 源就是它）
func TestRingCoreRecordsZapEntries(t *testing.T) {
	logger := zap.New(newRingCore(), zap.AddCaller())

	const marker = "ring-core-test-2026-11-18"
	logger.Info(marker, zap.String("k", "v"))
	logger.Error(marker + "-error")

	entries := RecentLogs()
	var foundInfo, foundError bool
	for _, entry := range entries {
		if !strings.Contains(entry.Message, marker) {
			continue
		}
		if entry.Time == "" {
			t.Fatalf("entry has empty time: %+v", entry)
		}
		switch entry.Level {
		case "info":
			foundInfo = true
		case "error":
			foundError = true
			if entry.Caller == "" {
				t.Fatalf("error entry lost caller: %+v", entry)
			}
		}
	}
	if !foundInfo || !foundError {
		t.Fatalf("ring did not record both entries (info=%v error=%v)", foundInfo, foundError)
	}
}

// 级别过滤留给查询侧：debug 也要进缓冲
func TestRingCoreAcceptsDebug(t *testing.T) {
	if !newRingCore().Enabled(zapcore.DebugLevel) {
		t.Fatalf("ring core must accept debug level, otherwise /logs?level=debug 永远是空的")
	}
}
