//go:build !linux && !darwin

package sysinfo

import (
	"fmt"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// 其它平台（Windows 等）：既没有 /proc，也没有 syscall.Statfs。
// 只保留 runtime 与进程 uptime，其余字段留空并标 Available=false。
// 本项目部署目标是 Linux 容器，这条分支存在只是为了 go build ./... 在任意平台都能过。

func readCPU() (cpuSample, error) {
	return cpuSample{}, fmt.Errorf("sysinfo: cpu sampling is not supported on this platform")
}

func readMemory() (models.DashboardMemory, error) {
	return models.DashboardMemory{}, fmt.Errorf("sysinfo: memory sampling is not supported on this platform")
}

func readSystemUptime() (uint64, error) {
	return 0, fmt.Errorf("sysinfo: system uptime is not supported on this platform")
}

func readLoad() ([3]float64, error) {
	return [3]float64{}, fmt.Errorf("sysinfo: load average is not supported on this platform")
}

func readDisk(path string) (models.DashboardDisk, error) {
	return models.DashboardDisk{}, fmt.Errorf("sysinfo: disk sampling is not supported on this platform")
}
