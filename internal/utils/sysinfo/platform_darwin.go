//go:build darwin

package sysinfo

import (
	"fmt"
	"syscall"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// macOS 没有 /proc：CPU / 内存 / 系统 uptime 都拿不到，
// 返回 Available=false + Reason（由 sysinfo.go 统一填），这里只把错误传上去。
// 磁盘 syscall.Statfs 在 darwin 上可用（字段类型与 linux 不同，所以 statfs 按平台实现）。

func readCPU() (cpuSample, error) {
	return cpuSample{}, fmt.Errorf("sysinfo: /proc/stat is not available on darwin")
}

func readMemory() (models.DashboardMemory, error) {
	return models.DashboardMemory{}, fmt.Errorf("sysinfo: /proc/meminfo is not available on darwin")
}

func readSystemUptime() (uint64, error) {
	return 0, fmt.Errorf("sysinfo: /proc/uptime is not available on darwin")
}

func readLoad() ([3]float64, error) {
	return [3]float64{}, fmt.Errorf("sysinfo: /proc/loadavg is not available on darwin")
}

// readDisk 用 Statfs 读挂载点容量
func readDisk(path string) (models.DashboardDisk, error) {
	total, free, avail, err := statfs(path)
	if err != nil {
		return models.DashboardDisk{}, err
	}
	used := uint64(0)
	if total > free {
		used = total - free
	}
	return models.DashboardDisk{
		Path:         path,
		Total:        total,
		Used:         used,
		Free:         avail,
		UsagePercent: percent(used, total),
	}, nil
}

// statfs 返回 (总容量, 空闲, 非 root 可用, error)，单位字节。
// darwin 的 Bsize 是 uint32，Blocks/Bfree/Bavail 是 uint64。
func statfs(path string) (total, free, available uint64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, 0, err
	}
	blockSize := uint64(st.Bsize)
	return st.Blocks * blockSize, st.Bfree * blockSize, st.Bavail * blockSize, nil
}
