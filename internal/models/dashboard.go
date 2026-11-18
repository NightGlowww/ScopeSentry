package models

// 仪表盘 / 控制台读模型（对标 ARL-Next dashboard.py 与 console.py）。
//
// 所有请求都是只读查询：不做任何写入，不落任何中间集合。
// pageSize 上限统一 200：handler 的 binding 把 >200 直接拒成 400，
// service 再做一次兜底归一化（防内部调用绕过 binding）。

// DashboardStatsRequest 总量统计请求。Project 为空表示全库。
type DashboardStatsRequest struct {
	Project string `json:"project" binding:"max=255"`
}

// DashboardStats 七类总量，与 ARL-Next /dashboard/stats 的卡片一一对应。
//
// Node / NodeOnline 来自 Redis 的节点心跳键（Redis 是节点状态的唯一来源），
// 其余计数都走 Mongo 的 countDocuments（空条件走集合元数据，不做全量扫描）。
type DashboardStats struct {
	Project       string `json:"project,omitempty"`
	Asset         int64  `json:"asset"`
	Subdomain     int64  `json:"subdomain"`
	URL           int64  `json:"url"`
	Vulnerability int64  `json:"vulnerability"`
	Sensitive     int64  `json:"sensitive"`
	Task          int64  `json:"task"`
	TaskRunning   int64  `json:"taskRunning"`
	TaskFinished  int64  `json:"taskFinished"`
	Node          int64  `json:"node"`
	NodeOnline    int64  `json:"nodeOnline"`
	GeneratedAt   string `json:"generatedAt"`
}

// DashboardTrendDays 允许的趋势窗口（天）。ARL-Next 只有 7 / 30 两档。
const (
	DashboardTrendDays7  = 7
	DashboardTrendDays30 = 30
)

// DashboardTrendRequest 趋势请求：days 只接受 7 或 30（binding 负责 400）。
type DashboardTrendRequest struct {
	Days    int    `json:"days" binding:"required,oneof=7 30"`
	Project string `json:"project" binding:"max=255"`
}

// DashboardTrendPoint 一天的新增/完成量。没有数据的日期返回 0（补零在服务层做）。
type DashboardTrendPoint struct {
	Date          string `json:"date"`          // 2006-01-02
	Asset         int64  `json:"asset"`         // 当天新增资产
	Vulnerability int64  `json:"vulnerability"` // 当天新增漏洞
	Task          int64  `json:"task"`          // 当天完成的任务
}

// DashboardTrendTotals 窗口内合计（没有 date 字段，避免前端把它当一天渲染）。
type DashboardTrendTotals struct {
	Asset         int64 `json:"asset"`
	Vulnerability int64 `json:"vulnerability"`
	Task          int64 `json:"task"`
}

// DashboardTrend 趋势响应：Points 长度恒等于 Days，按日期升序。
//
// 同一份数据给了两种视图：
//   - Points：按天一行（date 自带），适合表格/直接渲染；
//   - Labels + Assets/Vulns/Tasks：列式（与 ARL-Next /dashboard/trend 的
//     days/assets/vulns 同形状），适合直接把序列喂给折线图，前端不用再 map。
//
// 两者由同一次聚合生成，长度一致、下标对齐。
type DashboardTrend struct {
	Days     int                   `json:"days"`
	Timezone string                `json:"timezone"`
	Start    string                `json:"start"` // 首日 00:00:00
	End      string                `json:"end"`   // 末日 23:59:59
	Labels   []string              `json:"labels"`
	Assets   []int64               `json:"assets"`
	Vulns    []int64               `json:"vulns"`
	Tasks    []int64               `json:"tasks"`
	Points   []DashboardTrendPoint `json:"points"`
	Totals   DashboardTrendTotals  `json:"totals"`
}

// DashboardLogsRequest 服务端日志查询。
//
// Level 为空表示不过滤；keyword 是 message/caller 的子串（不区分大小写）。
type DashboardLogsRequest struct {
	Level     string `json:"level" binding:"omitempty,oneof=debug info warn error dpanic panic fatal"`
	Keyword   string `json:"keyword" binding:"max=255"`
	PageIndex int    `json:"pageIndex" binding:"omitempty,min=0"`
	PageSize  int    `json:"pageSize" binding:"omitempty,min=0,max=200"`
}

// DashboardLogEntry 一条服务端日志。等待时间是 "2006-01-02 15:04:05"（配置时区）。
type DashboardLogEntry struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Caller  string `json:"caller,omitempty"`
	Message string `json:"message"`
}

// DashboardLogsResult 日志分页结果。
//
// Source: memory（进程内环形缓冲）/ file（logs.file_name 配置的日志文件）/ none（都没有）。
// Truncated: 读取窗口被截断（文件只读了尾部窗口，或环形缓冲已覆盖最早的条目）。
type DashboardLogsResult struct {
	List      []DashboardLogEntry `json:"list"`
	Total     int64               `json:"total"`
	Page      int                 `json:"page"`
	PageSize  int                 `json:"pageSize"`
	Source    string              `json:"source"`
	Truncated bool                `json:"truncated"`
}

// DashboardCPU CPU 指标。UsagePercent 是两次采样之间的占用率；
// 进程内第一次调用没有上一次采样，回退为「开机以来的平均占用率」。
type DashboardCPU struct {
	Cores        int     `json:"cores"`
	UsagePercent float64 `json:"usagePercent"`
	Load1        float64 `json:"load1"`
	Load5        float64 `json:"load5"`
	Load15       float64 `json:"load15"`
}

// DashboardMemory 内存指标（字节）。Linux 用 /proc/meminfo 的 MemAvailable 算可用内存。
type DashboardMemory struct {
	Total        uint64  `json:"total"`
	Used         uint64  `json:"used"`
	Available    uint64  `json:"available"`
	UsagePercent float64 `json:"usagePercent"`
}

// DashboardDisk 磁盘指标（字节）。Path 是被统计的挂载点（默认 "/"）。
type DashboardDisk struct {
	Path         string  `json:"path"`
	Total        uint64  `json:"total"`
	Used         uint64  `json:"used"`
	Free         uint64  `json:"free"`
	UsagePercent float64 `json:"usagePercent"`
}

// DashboardUptime 运行时长时间。Process* 是服务进程的；System* 是整机 uptime（仅 Linux）。
type DashboardUptime struct {
	ProcessSeconds uint64 `json:"processSeconds"`
	ProcessHuman   string `json:"processHuman"`
	StartedAt      string `json:"startedAt"`
	SystemSeconds  uint64 `json:"systemSeconds"`
	SystemHuman    string `json:"systemHuman"`
}

// DashboardRuntime Go 运行时信息。
type DashboardRuntime struct {
	GoVersion  string `json:"goVersion"`
	Goroutines int    `json:"goroutines"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	NumCPU     int    `json:"numCPU"`
	MemAlloc   uint64 `json:"memAlloc"`
	MemSys     uint64 `json:"memSys"`
	GCCount    uint32 `json:"gcCount"`
}

// DashboardSysInfo 系统信息。
//
// Available=false 表示当前平台没有 /proc（例如 macOS 本地开发）：
// 此时只返回 runtime、进程 uptime 与磁盘，CPU/内存/系统 uptime 留空，
// Reason 说明缺什么。容器里的 Linux 内核仍然提供 /proc，所以容器可用。
type DashboardSysInfo struct {
	Available   bool             `json:"available"`
	Reason      string           `json:"reason,omitempty"`
	CPU         DashboardCPU     `json:"cpu"`
	Memory      DashboardMemory  `json:"memory"`
	Disk        DashboardDisk    `json:"disk"`
	Uptime      DashboardUptime  `json:"uptime"`
	Runtime     DashboardRuntime `json:"runtime"`
	CollectedAt string           `json:"collectedAt"`
}

// DashboardWidgetsRequest 一次拉多个卡片。
//
// Days 省略时按 7 天；IncludeSysInfo 省略时返回 sysinfo（卡片默认要）。
type DashboardWidgetsRequest struct {
	Days           int    `json:"days" binding:"omitempty,oneof=7 30"`
	Project        string `json:"project" binding:"max=255"`
	IncludeSysInfo *bool  `json:"includeSysInfo"`
}

// DashboardWidgets 多卡片聚合结果：stats + trend + sysinfo，一次返回，避免前端三个请求。
type DashboardWidgets struct {
	Stats       *DashboardStats   `json:"stats"`
	Trend       *DashboardTrend   `json:"trend"`
	SysInfo     *DashboardSysInfo `json:"sysInfo,omitempty"`
	GeneratedAt string            `json:"generatedAt"`
}

// ConsoleDependency 依赖（Mongo / Redis）的连接状态。
type ConsoleDependency struct {
	Connected bool    `json:"connected"`
	LatencyMs float64 `json:"latencyMs"`
	Error     string  `json:"error,omitempty"`
}

// ConsoleInfo 控制台信息（对标 ARL-Next console/info）。
type ConsoleInfo struct {
	Version       string            `json:"version"`
	StartTime     string            `json:"startTime"`
	UptimeSeconds uint64            `json:"uptimeSeconds"`
	UptimeHuman   string            `json:"uptimeHuman"`
	Timezone      string            `json:"timezone"`
	NodeCount     int64             `json:"nodeCount"`
	NodeOnline    int64             `json:"nodeOnline"`
	TaskCount     int64             `json:"taskCount"`
	TaskRunning   int64             `json:"taskRunning"`
	Mongo         ConsoleDependency `json:"mongo"`
	Redis         ConsoleDependency `json:"redis"`
	// HostStats 主机资源快照（磁盘/负载/内存），由 console handler 填充
	HostStats    *HostStats `json:"hostStats,omitempty"`
	HostWarnings []string   `json:"hostWarnings,omitempty"`
	CollectedAt  string     `json:"collectedAt"`
}

// HostStats 主机资源快照（与 utils/hoststat.Stats 字段一致，models 不反向依赖 utils）。
type HostStats struct {
	CPUs                int     `json:"cpus"`
	Load1               float64 `json:"load1"`
	DiskFreeBytes       uint64  `json:"diskFreeBytes"`
	DiskTotalBytes      uint64  `json:"diskTotalBytes"`
	DiskFreePercent     float64 `json:"diskFreePercent"`
	MemAvailablePercent float64 `json:"memAvailablePercent"`
}
