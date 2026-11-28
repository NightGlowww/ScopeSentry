package models

// NodeData 表示节点数据
type NodeData struct {
	Name          string `json:"name"`
	Running       string `json:"running"`
	Finished      string `json:"finished"`
	State         string `json:"state"`
	CPUNum        string `json:"cpuNum"`
	MemNum        string `json:"memNum"`
	UpdateTime    string `json:"updateTime"`
	MaxTaskNum    string `json:"maxTaskNum"`
	Version       string `json:"version"`
	ModulesConfig string `json:"modulesConfig"`
	// QueueLen 该节点待领取的任务数（节点一次只跑一个，积压多说明排队严重）
	QueueLen int64 `json:"queueLen"`
	// CmdFail 外部工具失败累计（ksubdomain/rad/katana 等非零退出次数）
	CmdFail int64 `json:"cmdFail"`
	// CmdFail10m 最近 10 分钟内的工具失败次数：判断"这批任务产出可不可信"用它
	CmdFail10m int64 `json:"cmdFail10m"`
	// Stale 心跳刚过期但还没到「判定死亡」的时长：界面应显示「心跳延迟」而不是「离线」
	Stale bool `json:"stale,omitempty"`
	// ContainerMem 节点容器自身内存占用百分比（-1 或 0 表示节点版本不支持上报）
	// memNum 是宿主机占用，容器被 cgroup 限住时它看不出来（实测宿主 24% / 容器 42%）
	ContainerMem float64 `json:"containerMem"`
	// MemSoftLimitMB 节点进程的 Go 软内存上限（MiB）：0 表示节点版本不支持上报
	MemSoftLimitMB int64 `json:"memSoftLimitMB"`

	// 以下为「此刻在做什么」——队列不动的根因基本都在这几个字段里：
	// 节点在线、心跳正常、currentTask 指着一个任务，但那个任务已经 N 分钟没有产出。
	// 2026-11-29 实测：node-jp-01 卡在 VulnerabilityScan 五小时，队列 6 个任务全排在后面，
	// 而界面只有「在线 / 队列长度 6」，看不出该做什么。
	CurrentTaskID   string `json:"currentTaskId,omitempty"`
	CurrentTaskName string `json:"currentTaskName,omitempty"`
	CurrentModule   string `json:"currentModule,omitempty"`
	CurrentTarget   string `json:"currentTarget,omitempty"`
	CurrentSince    string `json:"currentSince,omitempty"`
	// IdleMinutes 当前任务最近一次进度写入距今多少分钟（无任务或未开始写进度为 0）
	IdleMinutes int `json:"idleMinutes"`
	// Stalled 超过巡检阈值仍无进度：节点该重启了（阈值与 watchdog 同一口径）
	Stalled bool `json:"stalled"`
	// QueueEtaMinutes 按最近任务平均耗时估算的排队时长（分钟，0 = 没有可用的历史样本）
	// 「队列 5」看不出严重程度，「≈3.2 小时」才让人知道今天要不要等（2026-11-29）
	QueueEtaMinutes int `json:"queueEtaMin"`
}

type Message struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
}

// NodeRuntime 节点此刻的运行态：在跑哪个任务的哪个模块、多久没产出。
// 由 Redis 的 node:<name>:currentTask 与 TaskInfo:progress:<id>:<target> 派生，
// 不复用任务列表的字段，因为节点视角要的是「谁堵住了队列」，不是任务详情。
type NodeRuntime struct {
	TaskID      string
	TaskName    string
	Module      string
	Target      string
	Since       string
	IdleMinutes int
}
