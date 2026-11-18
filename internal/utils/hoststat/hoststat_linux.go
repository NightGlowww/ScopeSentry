//go:build linux

package hoststat

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// Stat 采集主机资源：磁盘（/ 所在分区）、1 分钟负载、可用内存占比。
// 任何一项失败只让对应字段保持 -1/0，不影响 /healthz 主流程。
func Stat(path string) Stats {
	s := Stats{CPUs: numCPU(), Load1: -1, MemAvailablePercent: -1}
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err == nil {
		free := st.Bavail * uint64(st.Bsize)
		total := st.Blocks * uint64(st.Bsize)
		s.DiskFreeBytes, s.DiskTotalBytes = free, total
		if total > 0 {
			s.DiskFreePercent = float64(free) / float64(total) * 100
		}
	}
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			if v, err := strconv.ParseFloat(f[0], 64); err == nil {
				s.Load1 = v
			}
		}
	}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		var total, avail float64 = -1, -1
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) < 2 {
				continue
			}
			switch f[0] {
			case "MemTotal:":
				total, _ = strconv.ParseFloat(f[1], 64)
			case "MemAvailable:":
				avail, _ = strconv.ParseFloat(f[1], 64)
			}
		}
		if total > 0 && avail >= 0 {
			s.MemAvailablePercent = avail / total * 100
		}
	}
	return s
}
