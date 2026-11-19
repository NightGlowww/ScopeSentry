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
}

type Message struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
}
