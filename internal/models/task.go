// Package models -----------------------------
// @file      : task.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2025/5/4 22:04
// -------------------------------------------
package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Task 任务模型
type Task struct {
	ID             primitive.ObjectID       `bson:"_id,omitempty" json:"id"`
	TaskID         string                   `bson:"id"` // 关联的任务ID
	Name           string                   `bson:"name" json:"name"`
	Node           []string                 `bson:"node" json:"node"`
	AllNode        bool                     `bson:"allNode" json:"allNode"`
	ScheduledTasks bool                     `bson:"scheduledTasks" json:"scheduledTasks"`
	Target         string                   `bson:"target" json:"target"`
	Ignore         string                   `bson:"ignore" json:"ignore"`
	Template       string                   `bson:"template" json:"template"`
	Project        []string                 `bson:"project" json:"project"`
	Search         string                   `bson:"search" json:"search"`
	CycleType      string                   `bson:"cycleType" json:"cycleType"`
	Hour           int                      `bson:"hour" json:"hour"`
	Minute         int                      `bson:"minute" json:"minute"`
	Day            int                      `bson:"day" json:"day"`
	Week           int                      `bson:"week" json:"week"`
	Status         int                      `bson:"status" json:"status"`
	Progress       int                      `bson:"progress" json:"progress"`
	CreatTime      string                   `bson:"creatTime" json:"creatTime"`
	EndTime        string                   `bson:"endTime" json:"endTime"`
	TaskNum        int                      `bson:"taskNum" json:"taskNum"`
	Duplicates     string                   `bson:"duplicates" json:"duplicates"`
	Filter         map[string][]interface{} `bson:"filter" json:"filter"`
	TargetNumber   int                      `bson:"targetNumber" json:"targetNumber"`
	TargetIds      []string                 `bson:"targetIds" json:"targetIds"`
	TargetTp       string                   `bson:"targetTp" json:"targetTp"`
	TargetSource   string                   `bson:"targetSource" json:"targetSource"`
	BindProject    string                   `bson:"bindProject" json:"bindProject"`
	Type           string                   `bson:"type" json:"type"`
	IsStart        bool                     `bson:"isStart" json:"isStart"`
	LastTime       string                   `bson:"lastTime"  json:"lastTime"`
	NextTime       string                   `bson:"nextTime" json:"nextTime"`
	Cycle          string                   `bson:"-" json:"cycle"` // 周期描述（计算字段）

	// PolicyID 建任务时选中的扫描策略（scan_policy._id）；空 = 不套用策略。
	// 只作「选哪个策略」的引用，参数消费在 services/task/taskpolicy（见 docs/audit/SCAN_POLICY.md）。
	PolicyID string `bson:"policyId,omitempty" json:"policyId,omitempty"`

	// Stats 终态产出快照（对齐 ARL-Next 1.5.2「终态统计补齐」）。
	// 任务完成那一刻统计一次并冻结：之后哪怕有清理/去重/新任务覆盖，历史任务的战果也不会变。
	// 空 = 尚未冻结（运行中，或历史数据），此时接口回退到实时统计。
	Stats map[string]int64 `bson:"stats,omitempty" json:"stats,omitempty"`
	// StatsFrozenAt 冻结时间（YYYY-MM-DD HH:MM:SS）
	StatsFrozenAt string `bson:"statsFrozenAt,omitempty" json:"statsFrozenAt,omitempty"`

	// PolicyWarnings 策略套用过程中的提示（策略不存在/已停用、模板冲突、插件未命中、
	// 速率/超时/字典等无承载字段）。落库是为了让前端能展示，而不是只留在服务端日志里。
	PolicyWarnings []string `bson:"policyWarnings,omitempty" json:"policyWarnings,omitempty"`

	// 当前活动（仅列表接口填充，不落库）：
	// 让列表里的「运行中 8%」变成「正在目录扫描 xxx，已 7 分钟」。
	CurrentModule string `bson:"-" json:"currentModule,omitempty"`
	CurrentTarget string `bson:"-" json:"currentTarget,omitempty"`
	CurrentSince  string `bson:"-" json:"currentSince,omitempty"`
	// Queued 表示任务还在队列里等节点来取（尚未真正开跑）
	Queued bool `bson:"-" json:"queued,omitempty"`
	// CmdFail 本次任务期间外部工具失败次数（节点任务结束时上报）
	CmdFail int64 `bson:"-" json:"cmdFail,omitempty"`
	// QueuePos 在节点待领取队列中的位置（1 起，0 表示不在队列里）
	QueuePos int `bson:"-" json:"queuePos,omitempty"`
	// FailReason 失败原因：巡检判定失败时写入，界面直接显示（否则只看到「失败」两个字）
	FailReason string `bson:"failReason,omitempty" json:"failReason,omitempty"`
	// FailCode / FailParams 是同一句话的机器可读形态。
	// 原因：failReason 是 Go 侧拼出来的中文句子，英文界面里直接显示就是一行中文。
	// 有了 code + params，前端就能按当前语言渲染，句子还能保持和后端一致的措辞。
	FailCode   string            `bson:"failCode,omitempty" json:"failCode,omitempty"`
	FailParams map[string]string `bson:"failParams,omitempty" json:"failParams,omitempty"`
	// StartedAt 本次启动/恢复的时间：用来算「已用时间」，暂停期间不该计入（2026-11-21）
	StartedAt string `bson:"startedAt,omitempty" json:"startedAt,omitempty"`
	// AutoResumeCount 因「节点重启/发布打断」被自动重投的次数。
	// 2026-11-24：以前巡检直接把这类任务判失败，用户只能手动重开；
	// 现在先自动重投，超过上限才落 failed，避免死循环。
	AutoResumeCount int `bson:"autoResumeCount,omitempty" json:"autoResumeCount,omitempty"`
}

// TaskProgress 任务进度模型
type TaskProgress struct {
	ID                  primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TargetHandler       int                `bson:"targetHandler" json:"targetHandler"`             // 目标处理
	SubdomainScan       int                `bson:"subdomainScan" json:"subdomainScan"`             // 子域名扫描
	SubdomainSecurity   int                `bson:"subdomainSecurity" json:"subdomainSecurity"`     // 子域名安全
	PortScanPreparation int                `bson:"portScanPreparation" json:"portScanPreparation"` // 端口扫描准备
	PortScan            int                `bson:"portScan" json:"portScan"`                       // 端口扫描
	PortFingerprint     int                `bson:"portFingerprint" json:"portFingerprint"`         // 端口指纹
	AssetMapping        int                `bson:"assetMapping" json:"assetMapping"`               // 资产映射
	AssetHandle         int                `bson:"assetHandle" json:"assetHandle"`                 // 资产处理
	URLScan             int                `bson:"urlScan" json:"urlScan"`                         // URL扫描
	WebCrawler          int                `bson:"webCrawler" json:"webCrawler"`                   // Web爬虫
	URLSecurity         int                `bson:"urlSecurity" json:"urlSecurity"`                 // URL安全
	DirScan             int                `bson:"dirScan" json:"dirScan"`                         // 目录扫描
	VulnerabilityScan   int                `bson:"vulnerabilityScan" json:"vulnerabilityScan"`     // 漏洞扫描
	All                 int                `bson:"all" json:"all"`                                 // 总进度
	Target              string             `bson:"target" json:"target"`                           // 目标
	Node                string             `bson:"node" json:"node"`                               // 节点
	Children            []TaskProgress     `bson:"children" json:"children"`                       // 子任务进度
}

// TaskProgressDetail 任务进度详情（用于API响应）
type TaskProgressDetail struct {
	ID                  primitive.ObjectID `bson:"_id,omitempty" json:"_id"`
	TargetHandler       []string           `json:"TargetHandler"`       // [start, end]
	SubdomainScan       []string           `json:"SubdomainScan"`       // [start, end]
	SubdomainSecurity   []string           `json:"SubdomainSecurity"`   // [start, end]
	PortScanPreparation []string           `json:"PortScanPreparation"` // [start, end]
	PortScan            []string           `json:"PortScan"`            // [start, end]
	PortFingerprint     []string           `json:"PortFingerprint"`     // [start, end]
	AssetMapping        []string           `json:"AssetMapping"`        // [start, end]
	AssetHandle         []string           `json:"AssetHandle"`         // [start, end]
	URLScan             []string           `json:"URLScan"`             // [start, end]
	WebCrawler          []string           `json:"WebCrawler"`          // [start, end]
	URLSecurity         []string           `json:"URLSecurity"`         // [start, end]
	DirScan             []string           `json:"DirScan"`             // [start, end]
	VulnerabilityScan   []string           `json:"VulnerabilityScan"`   // [start, end]
	All                 []string           `json:"All"`                 // [start, end]
	Target              string             `json:"target"`
	Node                string             `json:"node"`
	// Paused 该目标被用户暂停/跳过（issue #277）
	Paused   bool                 `json:"paused,omitempty"`
	Children []TaskProgressDetail `json:"children,omitempty"`
}

// TaskProgressSnapshot 任务结束后落库的逐目标进度明细（issue #278）。
//
// 节点把进度写进 Redis 的 TaskInfo:progress:<taskID>:<target> 哈希；任务完成时服务端
// 会删掉 tmp/time，Redis 数据也可能被清理或重启丢掉。完成时把每个目标的哈希原样快照
// 到这里，接口在 Redis 读不到数据时回放同一份明细。
type TaskProgressSnapshot struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"_id"`
	TaskID    string             `bson:"taskID" json:"taskID"`
	Target    string             `bson:"target" json:"target"`
	Data      map[string]string  `bson:"data" json:"data"`
	UpdatedAt time.Time          `bson:"updatedAt" json:"updatedAt"`
}

// ListRequest 任务列表分页查询请求
type ListRequest struct {
	Search    string `json:"search" binding:"omitempty" example:"测试"`
	PageIndex int    `json:"pageIndex" binding:"required,min=1" example:"1"`
	PageSize  int    `json:"pageSize" binding:"required,min=1" example:"10"`
	// Status 过滤：-1/不传=全部，0未开始 1运行中 2暂停 3完成 4失败
	Status *int `json:"status" binding:"omitempty,min=-1,max=4" example:"1"`
	// Node 按节点筛选（任务 node 是数组，匹配包含即命中）
	Node string `json:"node" binding:"omitempty" example:"node-sh-01"`
	// BindProject 按「绑定项目」筛选：项目详情页要列出这个项目下的所有任务。
	// 注意区分 task.project（资产归属，数组）和 bindProject（任务归属，字符串）——
	// 项目页看的是后者：这条任务是为哪个项目跑的。
	BindProject string `json:"bindProject" binding:"omitempty" example:"507f1f77bcf86cd799439011"`
	// Sort 表头点击排序："<字段>:<asc|desc>"。字段白名单在 service 里校验，
	// 只允许 name/status/progress/creatTime/endTime/taskNum —— 不能让前端传任意字段进 $sort。
	Sort string `json:"sort" binding:"omitempty" example:"creatTime:desc"`
}

// ScheduledTask 计划任务模型
type ScheduledTask struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"_id"`
	TaskID         string             `bson:"id" json:"id"`                         // 关联的任务ID
	Name           string             `bson:"name" json:"name"`                     // 任务名称
	Type           string             `bson:"type" json:"type"`                     // 任务类型
	LastTime       string             `bson:"lastTime" json:"lastTime"`             // 上次执行时间
	NextTime       string             `bson:"nextTime" json:"nextTime"`             // 下次执行时间
	State          bool               `bson:"scheduledTasks" json:"state"`          // 是否启用
	Node           []string           `bson:"node" json:"node"`                     // 节点列表
	Cycle          string             `bson:"-" json:"cycle"`                       // 周期描述（计算字段）
	AllNode        bool               `bson:"allNode" json:"allNode"`               // 是否所有节点
	RunnerID       string             `bson:"runner_id" json:"runner_id"`           // 运行器ID
	Project        []string           `bson:"project" json:"project"`               // 项目列表
	TargetSource   string             `bson:"targetSource" json:"targetSource"`     // 目标来源
	Day            int                `bson:"day" json:"day"`                       // 天数
	Minute         int                `bson:"minute" json:"minute"`                 // 分钟
	Hour           int                `bson:"hour" json:"hour"`                     // 小时
	Search         string             `bson:"search" json:"search"`                 // 搜索条件
	CycleType      string             `bson:"cycleType" json:"cycleType"`           // 周期类型
	ScheduledTasks bool               `bson:"scheduledTasks" json:"scheduledTasks"` // 是否计划任务
	Target         string             `bson:"target" json:"target"`                 // 目标
	Ignore         string             `bson:"ignore" json:"ignore"`                 // 忽略规则
	Template       string             `bson:"template" json:"template"`             // 模板
	Duplicates     string             `bson:"duplicates" json:"duplicates"`         // 去重规则
	Week           int                `bson:"week" json:"week"`                     // 星期
}

// ScheduledTaskListRequest 计划任务列表请求
type ScheduledTaskListRequest struct {
	Search    string `json:"search" binding:"omitempty" example:"测试"`
	PageIndex int    `json:"pageIndex" binding:"omitempty,min=1" example:"1"`
	PageSize  int    `json:"pageSize" binding:"omitempty,min=1" example:"10"`
}

// ScheduledTaskDetailRequest 计划任务详情请求
type ScheduledTaskDetailRequest struct {
	ID string `json:"id" binding:"required" example:"507f1f77bcf86cd799439011"`
}

// ScheduledTaskDeleteRequest 计划任务删除请求
type ScheduledTaskDeleteRequest struct {
	IDs []string `json:"ids" binding:"required" example:"[\"507f1f77bcf86cd799439011\"]"`
}

// ScheduledTaskAddRequest 计划任务添加请求
type ScheduledTaskAddRequest struct {
	Name           string   `json:"name" binding:"required" example:"定时扫描任务"`
	Type           string   `json:"type" binding:"omitempty" example:"scan"`
	Node           []string `json:"node" binding:"required" example:"[\"node1\", \"node2\"]"`
	AllNode        bool     `json:"allNode" binding:"omitempty" example:"true"`
	Project        []string `json:"project" binding:"omitempty" example:"[\"project1\"]"`
	TargetSource   string   `json:"targetSource" binding:"omitempty" example:"general"`
	Day            int      `json:"day" binding:"omitempty" example:"1"`
	Minute         int      `json:"minute" binding:"omitempty" example:"0"`
	Hour           int      `json:"hour" binding:"omitempty" example:"1"`
	Search         string   `json:"search" binding:"omitempty" example:""`
	CycleType      string   `json:"cycleType" binding:"omitempty" example:"nhours"`
	ScheduledTasks bool     `json:"scheduledTasks" binding:"omitempty" example:"true"`
	Target         string   `json:"target" binding:"omitempty" example:"example.com"`
	Ignore         string   `json:"ignore" binding:"omitempty" example:""`
	Template       string   `json:"template" binding:"omitempty" example:"default"`
	Duplicates     string   `json:"duplicates" binding:"omitempty" example:"true"`
	Week           int      `json:"week" binding:"omitempty" example:"1"`
}

// ScheduledTaskUpdateRequest 计划任务更新请求
type ScheduledTaskUpdateRequest struct {
	ID             string   `json:"id" binding:"required" example:"507f1f77bcf86cd799439011"`
	Name           string   `json:"name" binding:"omitempty" example:"定时扫描任务"`
	Type           string   `json:"type" binding:"omitempty" example:"scan"`
	Node           []string `json:"node" binding:"omitempty" example:"[\"node1\", \"node2\"]"`
	AllNode        bool     `json:"allNode" binding:"omitempty" example:"true"`
	Project        []string `json:"project" binding:"omitempty" example:"[\"project1\"]"`
	TargetSource   string   `json:"targetSource" binding:"omitempty" example:"general"`
	Day            int      `json:"day" binding:"omitempty" example:"1"`
	Minute         int      `json:"minute" binding:"omitempty" example:"0"`
	Hour           int      `json:"hour" binding:"omitempty" example:"1"`
	Search         string   `json:"search" binding:"omitempty" example:""`
	CycleType      string   `json:"cycleType" binding:"omitempty" example:"nhours"`
	ScheduledTasks bool     `json:"scheduledTasks" binding:"omitempty" example:"true"`
	Target         string   `json:"target" binding:"omitempty" example:"example.com"`
	Ignore         string   `json:"ignore" binding:"omitempty" example:""`
	Template       string   `json:"template" binding:"omitempty" example:"default"`
	Duplicates     string   `json:"duplicates" binding:"omitempty" example:"true"`
	Week           int      `json:"week" binding:"omitempty" example:"1"`
}

// PageMonitoringTask 页面监控任务模型
type PageMonitoringTask struct {
	ID      primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	URL     string             `bson:"url" json:"url"`
	Hash    []string           `bson:"hash" json:"hash"`
	MD5     string             `bson:"md5" json:"md5"`
	State   int                `bson:"state" json:"state"`
	Project string             `bson:"project" json:"project"`
	Time    string             `bson:"time" json:"time"`
}

// PageMonitoringListRequest 页面监控列表请求
type PageMonitoringListRequest struct {
	Search    string `json:"search" binding:"omitempty" example:"example.com"`
	PageIndex int    `json:"pageIndex" binding:"omitempty,min=1" example:"1"`
	PageSize  int    `json:"pageSize" binding:"omitempty,min=1" example:"10"`
}

// PageMonitoringAddRequest 页面监控添加请求
type PageMonitoringAddRequest struct {
	URL string `json:"url" binding:"required" example:"https://example.com"`
}

// PageMonitoringDeleteRequest 页面监控删除请求
type PageMonitoringDeleteRequest struct {
	IDs []string `json:"ids" binding:"required" example:"[\"507f1f77bcf86cd799439011\"]"`
}

// PageMonitoringUpdateRequest 页面监控更新请求
type PageMonitoringUpdateRequest struct {
	Hour    int      `json:"hour" binding:"omitempty" example:"24"`
	Node    []string `json:"node" binding:"omitempty" example:"[\"node1\"]"`
	AllNode bool     `json:"allNode" binding:"omitempty" example:"true"`
	State   bool     `json:"state" binding:"omitempty" example:"true"`
}
