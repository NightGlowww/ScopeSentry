//go:build linux

package sysinfo

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// 容器里读的是宿主机视角的 /proc/stat、/proc/meminfo：
//   - CPU 占用率因此是宿主机整机口径（cgroup cpu.max 未换算，见下）；
//   - 内存优先用 cgroup 限额（v2 memory.max/memory.current，v1 memory.limit_in_bytes/
//     memory.usage_in_bytes）覆盖 /proc/meminfo，否则容器里会显示宿主机总内存。
const (
	procStat   = "/proc/stat"
	procMem    = "/proc/meminfo"
	procUptime = "/proc/uptime"
	procLoad   = "/proc/loadavg"

	cgroupV2MemoryMax     = "/sys/fs/cgroup/memory.max"
	cgroupV2MemoryCurrent = "/sys/fs/cgroup/memory.current"
	cgroupV1MemoryMax     = "/sys/fs/cgroup/memory/memory.limit_in_bytes"
	cgroupV1MemoryCurrent = "/sys/fs/cgroup/memory/memory.usage_in_bytes"
)

// readCPU 读 /proc/stat 首行，返回累计的 idle 与 total（jiffies）。
//
// idle 取 idle+iowait：iowait 期间 CPU 并没有在跑用户代码，算进 busy 会让
// 空闲机器看起来在忙。
func readCPU() (cpuSample, error) {
	f, err := os.Open(procStat)
	if err != nil {
		return cpuSample{}, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)[1:]
		var total, idle uint64
		for i, raw := range fields {
			v, err := strconv.ParseUint(raw, 10, 64)
			if err != nil {
				continue
			}
			total += v
			// 第 4、5 个字段（0 基 3、4）分别是 idle 与 iowait
			if i == 3 || i == 4 {
				idle += v
			}
		}
		if total == 0 {
			return cpuSample{}, fmt.Errorf("sysinfo: %s cpu line has zero total", procStat)
		}
		return cpuSample{idle: idle, total: total}, nil
	}
	if err := scanner.Err(); err != nil {
		return cpuSample{}, err
	}
	return cpuSample{}, fmt.Errorf("sysinfo: cpu line not found in %s", procStat)
}

// readLoad 读 /proc/loadavg 的前三个值
func readLoad() ([3]float64, error) {
	var out [3]float64
	raw, err := os.ReadFile(procLoad)
	if err != nil {
		return out, err
	}
	fields := strings.Fields(string(raw))
	if len(fields) < 3 {
		return out, fmt.Errorf("sysinfo: unexpected %s content %q", procLoad, string(raw))
	}
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return out, err
		}
		out[i] = v
	}
	return out, nil
}

// readMemory 优先用 cgroup 限额（容器口径），拿不到再退回 /proc/meminfo（整机口径）。
func readMemory() (models.DashboardMemory, error) {
	hostTotal, hostAvail, err := readProcMeminfo()
	if err != nil {
		return models.DashboardMemory{}, err
	}

	if limit, current, ok := readCgroupMemory(); ok && limit > 0 && limit < hostTotal {
		used := current
		if used > limit {
			used = limit
		}
		return models.DashboardMemory{
			Total:        limit,
			Used:         used,
			Available:    limit - used,
			UsagePercent: percent(used, limit),
		}, nil
	}

	used := uint64(0)
	if hostTotal > hostAvail {
		used = hostTotal - hostAvail
	}
	return models.DashboardMemory{
		Total:        hostTotal,
		Used:         used,
		Available:    hostAvail,
		UsagePercent: percent(used, hostTotal),
	}, nil
}

// readProcMeminfo 解析 /proc/meminfo 的 MemTotal / MemAvailable（kB -> 字节）
func readProcMeminfo() (total, available uint64, err error) {
	f, err := os.Open(procMem)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	var haveTotal, haveAvail bool
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		value, convErr := strconv.ParseUint(fields[1], 10, 64)
		if convErr != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total, haveTotal = value*1024, true
		case "MemAvailable:":
			available, haveAvail = value*1024, true
		}
		if haveTotal && haveAvail {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}
	if !haveTotal {
		return 0, 0, fmt.Errorf("sysinfo: MemTotal not found in %s", procMem)
	}
	return total, available, nil
}

// readCgroupMemory 读 cgroup v2 / v1 的内存限额与当前用量。
// 老内核上可能是超大值（表示不限额），交给调用方判断。
func readCgroupMemory() (limit, current uint64, ok bool) {
	if l, okL := readUintFile(cgroupV2MemoryMax); okL {
		if c, okC := readUintFile(cgroupV2MemoryCurrent); okC {
			return l, c, true
		}
	}
	if l, okL := readUintFile(cgroupV1MemoryMax); okL {
		if c, okC := readUintFile(cgroupV1MemoryCurrent); okC {
			return l, c, true
		}
	}
	return 0, 0, false
}

// readUintFile 读一个只包含十进制数字的文件（cgroup v2 的 "max" 会读失败，符合预期）
func readUintFile(path string) (uint64, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	v, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// readSystemUptime 读 /proc/uptime 的第一列（秒）
func readSystemUptime() (uint64, error) {
	raw, err := os.ReadFile(procUptime)
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return 0, fmt.Errorf("sysinfo: empty %s", procUptime)
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, err
	}
	if seconds < 0 {
		return 0, nil
	}
	return uint64(seconds), nil
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

// statfs 返回 (总容量, 空闲, 非 root 可用, error)，单位字节
func statfs(path string) (total, free, available uint64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, 0, err
	}
	blockSize := uint64(st.Bsize)
	return st.Blocks * blockSize, st.Bfree * blockSize, st.Bavail * blockSize, nil
}
