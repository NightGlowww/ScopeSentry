package icp

// 集合名（与 ARL-Next icp_task / icp_asset 对齐，icp_result 是本案的读模型名）
const (
	// CollectionTask 查询任务
	CollectionTask = "icp_task"
	// CollectionResult 备案记录（唯一键 domain+company+icpNo）
	CollectionResult = "icp_result"
	// CollectionLog 任务日志（log 端点取最近 200 行）
	CollectionLog = "icp_task_log"
)
