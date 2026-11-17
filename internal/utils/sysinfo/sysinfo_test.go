package sysinfo

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestHumanDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{5 * time.Second, "5s"},
		{90 * time.Second, "1m30s"},
		{2*time.Hour + 3*time.Minute + 4*time.Second, "2h3m4s"},
		{26*time.Hour + time.Second, "1d2h0m1s"},
		{-time.Second, "0s"},
	}
	for _, c := range cases {
		if got := HumanDuration(c.d); got != c.want {
			t.Fatalf("HumanDuration(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestPercent(t *testing.T) {
	if got := percent(0, 0); got != 0 {
		t.Fatalf("percent(0,0) = %v, want 0", got)
	}
	if got := percent(50, 200); got != 25 {
		t.Fatalf("percent(50,200) = %v, want 25", got)
	}
}

// CPU 占用率：整段空闲 = 0，整段忙碌 = 100，一半空闲 = 50
func TestUsagePercentSince(t *testing.T) {
	if got := usagePercentSince(cpuSample{idle: 100, total: 200}, cpuSample{idle: 200, total: 300}); got != 0 {
		t.Fatalf("all idle = %v, want 0", got)
	}
	if got := usagePercentSince(cpuSample{idle: 100, total: 200}, cpuSample{idle: 100, total: 300}); got != 100 {
		t.Fatalf("all busy = %v, want 100", got)
	}
	if got := usagePercentSince(cpuSample{idle: 100, total: 200}, cpuSample{idle: 150, total: 300}); got != 50 {
		t.Fatalf("half busy = %v, want 50", got)
	}
	// 采样没有推进时不能除零
	if got := usagePercentSince(cpuSample{idle: 100, total: 200}, cpuSample{idle: 100, total: 200}); got != 0 {
		t.Fatalf("no progress = %v, want 0", got)
	}
}

// Collect：runtime / 进程 uptime 在任何平台都必须有；
// /proc 相关的指标只有 Linux 才有，其它平台必须明确标 available=false + reason。
func TestCollect(t *testing.T) {
	info := Collect()
	if info == nil {
		t.Fatal("Collect returned nil")
	}
	if info.CollectedAt == "" {
		t.Error("collectedAt is empty")
	}
	if info.Runtime.GoVersion != runtime.Version() {
		t.Errorf("goVersion = %q, want %q", info.Runtime.GoVersion, runtime.Version())
	}
	if info.Runtime.Goroutines <= 0 {
		t.Errorf("goroutines = %d, want > 0", info.Runtime.Goroutines)
	}
	if info.Runtime.NumCPU <= 0 {
		t.Errorf("numCPU = %d, want > 0", info.Runtime.NumCPU)
	}
	if info.Uptime.ProcessSeconds > 3600 {
		t.Errorf("process uptime = %ds, want a small value in tests", info.Uptime.ProcessSeconds)
	}
	if info.Uptime.ProcessHuman == "" || info.Uptime.StartedAt == "" {
		t.Errorf("uptime human/startedAt empty: %+v", info.Uptime)
	}

	if runtime.GOOS == "linux" {
		// 容器/CI 里必须真的读到 /proc
		if !info.Available {
			t.Fatalf("linux should have /proc data, reason=%q", info.Reason)
		}
		if info.CPU.Cores <= 0 {
			t.Errorf("cpu.cores = %d, want > 0", info.CPU.Cores)
		}
		if info.CPU.UsagePercent < 0 || info.CPU.UsagePercent > 100 {
			t.Errorf("cpu.usagePercent = %v, want 0..100", info.CPU.UsagePercent)
		}
		if info.Memory.Total == 0 {
			t.Errorf("memory.total = 0, want > 0")
		}
		if info.Memory.Used > info.Memory.Total {
			t.Errorf("memory.used %d > total %d", info.Memory.Used, info.Memory.Total)
		}
		if info.Disk.Total == 0 {
			t.Errorf("disk.total = 0, want > 0")
		}
		if info.Uptime.SystemSeconds == 0 {
			t.Errorf("uptime.systemSeconds = 0, want > 0")
		}
		return
	}

	// macOS 本地（没有 /proc）：允许降级，但必须说明原因，且磁盘仍然可读
	if info.Available {
		t.Fatalf("non-linux should report available=false")
	}
	if !strings.Contains(info.Reason, "platform") {
		t.Errorf("reason = %q, want a platform explanation", info.Reason)
	}
	if info.Disk.Total != 0 && info.Disk.Used > info.Disk.Total {
		t.Errorf("disk.used %d > total %d", info.Disk.Used, info.Disk.Total)
	}
}
