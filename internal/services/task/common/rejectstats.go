package common

import (
	"sync"
)

// 调度拒绝计数：四道闸各自拒了多少次。
// 目的：外部集成反复撞墙时能一眼看出来，而不是翻日志（2026-11-20）。
// 进程内存计数，重启归零——只用于"最近有没有人在撞闸"，不做审计（审计在 audit_logs）。
var (
	rejectMu     sync.Mutex
	rejectCounts = map[string]int64{}
)

const (
	RejectQueue      = "queue_full"    // 单节点排队满
	RejectMemory     = "container_mem" // 容器内存接近上限
	RejectPrefix     = "prefix_quota"  // 同前缀占满
	RejectRateLimit  = "rate_limited"  // 每分钟创建过多
	RejectNameExists = "name_exists"   // 任务名重复
)

func bumpReject(kind string) {
	rejectMu.Lock()
	rejectCounts[kind]++
	rejectMu.Unlock()
}

// RejectStats 返回各闸门的拒绝次数（进程内累计）。
func RejectStats() map[string]int64 {
	rejectMu.Lock()
	defer rejectMu.Unlock()
	out := make(map[string]int64, len(rejectCounts))
	for k, v := range rejectCounts {
		out[k] = v
	}
	return out
}
