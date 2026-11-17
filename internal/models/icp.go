package models

import (
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ICP 企业主体 ↔ 域名查询（对标 ARL-Next backend/app/routes/icp.py）。
//
// 三层数据结构：
//  1. icp_task   —— 查询任务（company=按企业名查域名 / domain=按域名查备案主体），
//     带状态机（0 待开始 / 1 运行中 / 2 暂停 / 3 完成）与「本轮运行」统计；
//  2. icp_result —— 备案记录，唯一键 (domain, company, icpNo) 跨任务去重；
//  3. icp_task_log —— 任务日志，log 端点取最近 200 行。
//
// 任务与数据来源解耦：provider 名落在任务上（manual / none / http）。
// 没有外部凭据时用 manual 读本地已导入记录，配置了 icp.provider_url 时走 HTTP provider。

// 任务状态机
const (
	// IcpTaskStateWaiting 待开始
	IcpTaskStateWaiting = 0
	// IcpTaskStateRunning 运行中
	IcpTaskStateRunning = 1
	// IcpTaskStatePaused 已暂停（provider 不可用 / 重试耗尽 / 人工停止），可 resume
	IcpTaskStatePaused = 2
	// IcpTaskStateDone 已完成
	IcpTaskStateDone = 3
)

// 任务类型
const (
	// IcpTaskTypeCompany 按企业主体查其备案域名
	IcpTaskTypeCompany = "company"
	// IcpTaskTypeDomain 按域名查备案主体
	IcpTaskTypeDomain = "domain"
)

// provider 名称
const (
	// IcpProviderManual 本地 icp_result 回读（永远可用）
	IcpProviderManual = "manual"
	// IcpProviderNone 未配置 / 显式关闭，调用时返回明确原因
	IcpProviderNone = "none"
	// IcpProviderHTTP 外部 ICP 服务（icp.provider_url / icp.token）
	IcpProviderHTTP = "http"
)

const (
	// IcpMaxPageSize 列表接口分页上限，超过直接 400（与 GitHub / MCP 治理面同一口径）
	IcpMaxPageSize = 200
	// IcpDefaultPageSize 未传 pageSize 时的默认值
	IcpDefaultPageSize = 20
	// IcpMaxLogLines log 端点返回的最大行数
	IcpMaxLogLines = 200
	// IcpRecordTypeWeb 备案记录默认类型（web 备案）
	IcpRecordTypeWeb = "web"
	// IcpScopeTypeDomain 同步到 asset_scope 时使用的地契类型
	IcpScopeTypeDomain = "domain"
)

// IcpTaskStats 任务统计（一次运行内累计：restart 清零，resume 延续）
//
// total/done 按 provider 返回的记录条数计（一条记录 = 一个 done），
// found 只统计「真正新增入库」的条数（去重命中的不算），failed 记录失败的查询/补全条目数。
type IcpTaskStats struct {
	Total  int64 `bson:"total" json:"total"`
	Done   int64 `bson:"done" json:"done"`
	Found  int64 `bson:"found" json:"found"`
	Failed int64 `bson:"failed" json:"failed"`
}

// IcpTask 查询任务
type IcpTask struct {
	ID      primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Name    string             `bson:"name" json:"name"`
	Type    string             `bson:"type" json:"type"`
	Queries []string           `bson:"queries" json:"queries"`
	State   int                `bson:"state" json:"state"`
	Stats   IcpTaskStats       `bson:"stats" json:"stats"`
	// Provider 创建任务时解析出的数据来源（manual / none / http）
	Provider string `bson:"provider" json:"provider"`
	// Cursor 下一个待处理的 queries 下标；resume 从这里继续，保证「已处理的查询不重跑」
	Cursor    int        `bson:"cursor" json:"cursor"`
	LastError string     `bson:"lastError,omitempty" json:"lastError,omitempty"`
	CreatedAt time.Time  `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time  `bson:"updatedAt" json:"updatedAt"`
	NextRunAt *time.Time `bson:"nextRunAt,omitempty" json:"nextRunAt,omitempty"`
}

// IcpTaskLog 任务日志行
type IcpTaskLog struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TaskID    string             `bson:"taskId" json:"taskId"`
	Level     string             `bson:"level" json:"level"`
	Line      string             `bson:"line" json:"line"`
	CreatedAt time.Time          `bson:"createdAt" json:"createdAt"`
}

// IcpResult 备案记录（唯一键 domain + company + icpNo）
type IcpResult struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TaskID    string             `bson:"taskId" json:"taskId"`
	Domain    string             `bson:"domain" json:"domain"`
	Company   string             `bson:"company" json:"company"`
	ICPNo     string             `bson:"icpNo" json:"icpNo"`
	SiteName  string             `bson:"siteName" json:"siteName"`
	Type      string             `bson:"type" json:"type"`
	Source    string             `bson:"source" json:"source"`
	CreatedAt time.Time          `bson:"createdAt" json:"createdAt"`
}

// ICPRecord provider 返回的单条备案记录（未落库）。
// bson tag 与 json tag 同时给出：同一个结构既用于 provider 响应解码，
// 也用于 manual provider 从 icp_result 回读（projection 直接 decode 成它）。
type ICPRecord struct {
	Domain   string `bson:"domain" json:"domain"`
	Company  string `bson:"company" json:"company"`
	ICPNo    string `bson:"icpNo" json:"icpNo"`
	SiteName string `bson:"siteName" json:"siteName"`
	Type     string `bson:"type" json:"type"`
}

// IcpTaskCreateRequest 新建任务
type IcpTaskCreateRequest struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Queries  []string `json:"queries"`
	Target   string   `json:"target"`
	Provider string   `json:"provider"`
}

// IcpTaskListQuery 任务列表查询
type IcpTaskListQuery struct {
	State     *int
	Type      string
	Keyword   string
	Provider  string
	PageIndex int
	PageSize  int
}

// IcpTaskIDRequest 单任务 / 批量任务操作的统一入参（兼容 id / taskId / ids / task_id）
type IcpTaskIDRequest struct {
	ID         string   `json:"id"`
	TaskID     string   `json:"taskId"`
	Underscore string   `json:"_id"`
	IDs        []string `json:"ids"`
	TaskIDList []string `json:"task_id"`
}

// Single 取单个任务 ID（id > taskId > _id）
func (r IcpTaskIDRequest) Single() string {
	for _, v := range []string{r.ID, r.TaskID, r.Underscore} {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// All 取全部任务 ID（合并 ids / task_id，去空保序）
func (r IcpTaskIDRequest) All() []string {
	out := make([]string, 0, len(r.IDs)+len(r.TaskIDList)+1)
	if single := r.Single(); single != "" {
		out = append(out, single)
	}
	out = append(out, r.IDs...)
	out = append(out, r.TaskIDList...)
	seen := make(map[string]struct{}, len(out))
	result := make([]string, 0, len(out))
	for _, id := range out {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

// IcpAssetQuery 结果列表查询
type IcpAssetQuery struct {
	TaskID    string
	PageIndex int
	PageSize  int
}

// IcpSyncRequest 同步结果到资产组
type IcpSyncRequest struct {
	TaskID  string `json:"taskId"`
	GroupID string `json:"groupId"`
}

// IcpBackfillRequest 补全详情
type IcpBackfillRequest struct {
	TaskID string `json:"taskId"`
}

// IcpTaskLogRequest 任务日志
type IcpTaskLogRequest struct {
	TaskID string `json:"taskId"`
	Limit  int    `json:"limit"`
}

// IcpStatTaskIDRequest 统计入参（taskId 可选）
type IcpStatTaskIDRequest struct {
	TaskID string `json:"taskId"`
}

// NormalizeIcpRecord 归一化一条备案记录：
// domain 小写去尾点，company/icpNo/siteName 去首尾空白，type 缺省 web。
func NormalizeIcpRecord(rec ICPRecord) ICPRecord {
	rec.Domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(rec.Domain), "."))
	rec.Company = strings.TrimSpace(rec.Company)
	rec.ICPNo = strings.Join(strings.Fields(rec.ICPNo), "")
	rec.SiteName = strings.TrimSpace(rec.SiteName)
	rec.Type = strings.ToLower(strings.TrimSpace(rec.Type))
	if rec.Type == "" {
		rec.Type = IcpRecordTypeWeb
	}
	return rec
}

// IsUsable 记录是否值得入库（至少要有域名或企业名）
func (rec ICPRecord) IsUsable() bool {
	return rec.Domain != "" || rec.Company != ""
}

// NormalizeIcpQueries 任务查询目标归一：去空、去重、域名小写去尾点（企业名保持原样）。
func NormalizeIcpQueries(taskType string, queries []string) []string {
	out := make([]string, 0, len(queries))
	seen := make(map[string]struct{}, len(queries))
	for _, q := range queries {
		q = strings.TrimSpace(q)
		if taskType == IcpTaskTypeDomain {
			q = strings.ToLower(strings.TrimSuffix(q, "."))
		}
		if q == "" {
			continue
		}
		if _, ok := seen[q]; ok {
			continue
		}
		seen[q] = struct{}{}
		out = append(out, q)
	}
	return out
}

// IcpStateName 状态数字 → 语义名（overview 返回用）
func IcpStateName(state int) string {
	switch state {
	case IcpTaskStateWaiting:
		return "waiting"
	case IcpTaskStateRunning:
		return "running"
	case IcpTaskStatePaused:
		return "paused"
	case IcpTaskStateDone:
		return "done"
	default:
		return "unknown"
	}
}
