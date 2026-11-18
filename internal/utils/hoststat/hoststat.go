package hoststat

// Stats 主机资源快照。字段缺失用 -1 表示「当前平台不提供」。
type Stats struct {
	CPUs                int     `json:"cpus"`
	Load1               float64 `json:"load1"`
	DiskFreeBytes       uint64  `json:"diskFreeBytes"`
	DiskTotalBytes      uint64  `json:"diskTotalBytes"`
	DiskFreePercent     float64 `json:"diskFreePercent"`
	MemAvailablePercent float64 `json:"memAvailablePercent"`
}

// Warnings 按阈值给出人可读告警；空切片表示健康。
func Warnings(s Stats) []string {
	out := []string{}
	if s.DiskTotalBytes > 0 && s.DiskFreePercent < 10 {
		out = append(out, "disk_free_low")
	}
	if s.CPUs > 0 && s.Load1 > float64(s.CPUs)*2 {
		out = append(out, "load_high")
	}
	if s.MemAvailablePercent >= 0 && s.MemAvailablePercent < 10 {
		out = append(out, "mem_low")
	}
	return out
}
