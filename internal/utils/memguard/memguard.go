// Package memguard 在启动扫描前检查主机可用内存。
//
// 背景：2026-11-19 生产事故——两个 nuclei 全量模板扫描并发把 3.6GB 内存的
// 机器打到 159% commit，swap 抖动 4.5 小时，sshd 全程无响应。
// 扫描容器已有 cgroup 限载（mem 2g），本闸是第二道防线：主机整体内存紧张时
// 不再叠加新扫描负载（Mongo/应用自身增长仍可能把主机吃到临界）。
package memguard

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// MinAvailablePercent 允许启动扫描的最低可用内存百分比。
// 容器限载 2g / 主机 3.6g ≈ 55%——留出 15% 给系统与已有进程，
// 新扫描至少需要 2g/3.6g ≈ 55%，两者取交集后 15% 是保守下限。
const MinAvailablePercent = 15.0

// AvailablePercent 可注入，便于测试；默认读真实 /proc/meminfo。
var AvailablePercent = func() float64 { return readMemAvailable() }

// Check 在可用内存低于阈值时返回错误；拿不到内存信息（-1）时不拦。
func Check() error {
	p := AvailablePercent()
	if p >= 0 && p < MinAvailablePercent {
		return fmt.Errorf("主机可用内存仅 %.1f%%（阈值 %.0f%%），已拒绝启动扫描任务；请等待运行中扫描结束或扩容内存", p, MinAvailablePercent)
	}
	return nil
}

// readMemAvailable 读 /proc/meminfo 的 MemAvailable/MemTotal。
// 非 Linux（darwin）返回 -1，由 Check 的调用方按"未知不拦"处理。
func readMemAvailable() float64 {
	if runtime.GOOS != "linux" {
		return -1
	}
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return -1
	}
	var total, avail float64 = -1, -1
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		v, err := strconv.ParseFloat(f[1], 64)
		if err != nil {
			continue
		}
		switch f[0] {
		case "MemTotal:":
			total = v
		case "MemAvailable:":
			avail = v
		}
	}
	if total <= 0 || avail < 0 {
		return -1
	}
	return avail / total * 100
}
