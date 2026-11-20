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
}

type Message struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
}
