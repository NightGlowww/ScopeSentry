//go:build darwin

package hoststat

import "syscall"

// Stat macOS 实现：只提供磁盘与 CPU 数；负载与内存可用占比保持 -1（不引入额外依赖）。
func Stat(path string) Stats {
	s := Stats{CPUs: numCPU(), Load1: -1, MemAvailablePercent: -1}
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err == nil {
		free := uint64(st.Bavail) * uint64(st.Bsize)
		total := uint64(st.Blocks) * uint64(st.Bsize)
		s.DiskFreeBytes, s.DiskTotalBytes = free, total
		if total > 0 {
			s.DiskFreePercent = float64(free) / float64(total) * 100
		}
	}
	return s
}
