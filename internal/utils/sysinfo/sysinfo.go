// Package sysinfo 采集本机 CPU / 内存 / 磁盘 / uptime / Go 运行时指标。
//
// 设计约束（对标 ARL-Next dashboard 的 sysinfo 卡片，但不引 gopsutil）：
//   - 不引重型依赖：Linux 走 /proc（stat / meminfo / uptime / loadavg），
//     磁盘走 syscall.Statfs，运行时走 runtime；
//   - 非 Linux 平台（macOS 本地开发）没有 /proc：返回 Available=false + Reason，
//     仍然给出 runtime、进程 uptime 与磁盘，其余字段留零值，不报 500；
//   - CPU 占用率是「两次采样之间」的差值；进程内第一次调用没有上一次采样，
//     回退为开机以来的平均占用率（在 UsagePercent 的注释里写清楚）。
package sysinfo

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/utils/helper"
)

// DiskPath 被统计的挂载点。容器里 "/" 就是镜像所在的可写层。
const DiskPath = "/"

// procUnavailableReason 非 Linux 平台的说明（macOS 本地跑测试时会出现在 reason 里）
const procUnavailableReason = "/proc is not available on this platform; cpu/memory/system-uptime are omitted"

// processStart 进程启动时间（包加载即开始计时，近似等于进程启动时刻）
var processStart = time.Now()

// ProcessStart 进程启动时间
func ProcessStart() time.Time { return processStart }

// ProcessUptime 进程已运行时长
func ProcessUptime() time.Duration { return time.Since(processStart) }

// cpuSample 一次 CPU 时间采样（jiffies 累计值）
type cpuSample struct {
	idle  uint64
	total uint64
}

// sampler 保存上一次采样，用来算区间占用率；并发安全。
var sampler struct {
	mu   sync.Mutex
	last *cpuSample
}

// usagePercentSince 用两次采样的差值算占用率。
//
// 没有上一次采样时用「开机以来」的累计值：
// total-idle 是开机以来累积的忙时间，除以 total 就是平均占用率。
func usagePercentSince(prev, cur cpuSample) float64 {
	if cur.total <= prev.total {
		return 0
	}
	totalDelta := cur.total - prev.total
	var idleDelta uint64
	if cur.idle > prev.idle {
		idleDelta = cur.idle - prev.idle
	}
	if idleDelta > totalDelta {
		idleDelta = totalDelta
	}
	return round2(float64(totalDelta-idleDelta) / float64(totalDelta) * 100)
}

// Collect 采集一次系统信息。任何单项失败都只让该字段留空，不影响整体返回：
// 仪表盘不该因为读不到 /proc/meminfo 就整块 500。
func Collect() *models.DashboardSysInfo {
	info := &models.DashboardSysInfo{
		CollectedAt: helper.FormatTime(time.Now()),
	}

	// 运行时：所有平台都有
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	info.Runtime = models.DashboardRuntime{
		GoVersion:  runtime.Version(),
		Goroutines: runtime.NumGoroutine(),
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		NumCPU:     runtime.NumCPU(),
		MemAlloc:   ms.Alloc,
		MemSys:     ms.Sys,
		GCCount:    ms.NumGC,
	}

	// 进程 uptime：所有平台都有
	uptime := ProcessUptime()
	info.Uptime = models.DashboardUptime{
		ProcessSeconds: uint64(uptime.Seconds()),
		ProcessHuman:   HumanDuration(uptime),
		StartedAt:      helper.FormatTime(ProcessStart()),
	}

	// 磁盘：Statfs 在 linux/darwin 都有，实现按平台分文件
	if disk, err := readDisk(DiskPath); err == nil {
		info.Disk = disk
	}

	// /proc 三件套：仅 Linux
	cpu, cpuErr := readCPU()
	if cpuErr != nil {
		info.Available = false
		info.Reason = procUnavailableReason
		return info
	}
	info.Available = true

	// CPU：把采样交给 sampler，下一次调用就能算区间占用率
	sampler.mu.Lock()
	prev := sampler.last
	cur := cpu
	sampler.last = &cur
	sampler.mu.Unlock()

	// 没有上一次采样时用零值作基线：差值退化成「开机以来」的累计值。
	base := cpuSample{}
	if prev != nil {
		base = *prev
	}
	info.CPU = models.DashboardCPU{
		Cores:        runtime.NumCPU(),
		UsagePercent: usagePercentSince(base, cur),
	}
	if load, err := readLoad(); err == nil {
		info.CPU.Load1, info.CPU.Load5, info.CPU.Load15 = load[0], load[1], load[2]
	}

	if mem, err := readMemory(); err == nil {
		info.Memory = mem
	}
	if sysSeconds, err := readSystemUptime(); err == nil {
		info.Uptime.SystemSeconds = sysSeconds
		info.Uptime.SystemHuman = HumanDuration(time.Duration(sysSeconds) * time.Second)
	}

	return info
}

// HumanDuration 把时长渲染成「3d4h5m6s」这种一眼能读的形式。
func HumanDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	days := int64(d / (24 * time.Hour))
	d -= time.Duration(days) * 24 * time.Hour
	hours := int64(d / time.Hour)
	d -= time.Duration(hours) * time.Hour
	minutes := int64(d / time.Minute)
	seconds := int64((d - time.Duration(minutes)*time.Minute) / time.Second)

	var b strings.Builder
	if days > 0 {
		fmt.Fprintf(&b, "%dd", days)
	}
	if days > 0 || hours > 0 {
		fmt.Fprintf(&b, "%dh", hours)
	}
	if days > 0 || hours > 0 || minutes > 0 {
		fmt.Fprintf(&b, "%dm", minutes)
	}
	fmt.Fprintf(&b, "%ds", seconds)
	return b.String()
}

// round2 保留两位小数，避免 JSON 里出现 12.345678901234
func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}

// percent 计算占用率，total=0 时返回 0（容器里 cgroup 限制可能让 total 读成 0）
func percent(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return round2(float64(used) / float64(total) * 100)
}
