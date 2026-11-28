package models

import (
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// GitHub 泄露 / 威胁情报（对标 ARL-Next backend/app/routes/github_*.py）。
//
// 三条主线：
//  1. github_task   —— 采集任务（code_leak / cve / hacker），带状态机与累计统计；
//  2. github_result —— 采集命中（唯一键 taskId+repo+path+commit 去重），含研判状态；
//  3. github_threat_config —— 威胁情报单例配置（cve / hacker / tools 三来源开关、关键词、间隔、token 引用）。
//
// 说明：github_threat_history 是威胁情报来源的「发现历史」集合（cve_history 等端点需要落库），
// 与 github_result 分开存，避免情报噪声混进泄露命中总览。

const (
	// GithubTaskStateWaiting 待开始（含被限速打断、等待 nextRunAt 再跑）
	GithubTaskStateWaiting = 0
	// GithubTaskStateRunning 运行中
	GithubTaskStateRunning = 1
	// GithubTaskStatePaused 已暂停（调度器跳过）
	GithubTaskStatePaused = 2
	// GithubTaskStateDone 已完成
	GithubTaskStateDone = 3
)

// 任务类型
const (
	GithubTypeCodeLeak = "code_leak"
	GithubTypeCVE      = "cve"
	GithubTypeHacker   = "hacker"
)

// 结果研判状态
const (
	GithubResultStatusNew     = "new"
	GithubResultStatusTriaged = "triaged"
	GithubResultStatusIgnored = "ignored"
)

// 结果来源：task = 任务采集；monitor = 外部监控写入
const (
	GithubSourceTask    = "task"
	GithubSourceMonitor = "monitor"
)

// 威胁情报来源
const (
	GithubThreatKindCVE    = "cve"
	GithubThreatKindHacker = "hacker"
	GithubThreatKindTools  = "tools"
)

const (
	// GithubMaxPageSize 列表接口分页上限，超过直接 400（与 MCP 治理面同一口径）
	GithubMaxPageSize = 200
	// GithubThreatConfigID github_threat_config 单例文档主键
	GithubThreatConfigID = "global"
	// GithubDefaultPageSize 未传 pageSize 时的默认值
	GithubDefaultPageSize = 10
	// GithubDefaultIntervalHours 威胁情报默认间隔（小时）
	GithubDefaultIntervalHours = 6
	// GithubDefaultTaskIntervalMinutes 周期任务的默认间隔（分钟）
	GithubDefaultTaskIntervalMinutes = 60
	// GithubDefaultMaxPages 单次采集最多翻多少页（Search API 单页上限 100 条）
	GithubDefaultMaxPages = 10
)

// GithubTaskStats 任务累计统计（跨多次运行累加，不是单次快照）
type GithubTaskStats struct {
	Scanned  int64 `bson:"scanned" json:"scanned"`
	NewFound int64 `bson:"newFound" json:"newFound"`
}

// GithubTaskConfig 任务级配置
//
// tokenRef 指向系统配置集合 config 里的一条文档名（name 字段），用于取 GitHub token；
// 留空则回落到环境变量 GITHUB_TOKEN。intervalMinutes > 0 表示该任务进入周期调度视图。
type GithubTaskConfig struct {
	TokenRef        string `bson:"tokenRef,omitempty" json:"tokenRef,omitempty"`
	IntervalMinutes int    `bson:"intervalMinutes,omitempty" json:"intervalMinutes,omitempty"`
	MaxPages        int    `bson:"maxPages,omitempty" json:"maxPages,omitempty"`
}

// GithubTask 采集任务
type GithubTask struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Name      string             `bson:"name" json:"name"`
	Query     string             `bson:"query" json:"query"`
	Keywords  []string           `bson:"keywords,omitempty" json:"keywords,omitempty"`
	Type      string             `bson:"type" json:"type"`
	State     int                `bson:"state" json:"state"`
	Project   string             `bson:"project,omitempty" json:"project,omitempty"`
	CreatedAt time.Time          `bson:"createdAt" json:"createdAt"`
	LastRunAt *time.Time         `bson:"lastRunAt,omitempty" json:"lastRunAt,omitempty"`
	NextRunAt *time.Time         `bson:"nextRunAt,omitempty" json:"nextRunAt,omitempty"`
	Stats     GithubTaskStats    `bson:"stats" json:"stats"`
	Config    GithubTaskConfig   `bson:"config" json:"config"`

	// 运行期字段（不落库）：最近一次运行的结束原因，便于前端提示
	LastError string `bson:"lastError,omitempty" json:"lastError,omitempty"`
}

// JoinKeywords 把关键词列表拼成 GitHub 搜索表达式（去空、去重、保序）
func JoinKeywords(keywords []string) string {
	parts := make([]string, 0, len(keywords))
	seen := make(map[string]struct{}, len(keywords))
	for _, kw := range keywords {
		kw = strings.TrimSpace(kw)
		if kw == "" {
			continue
		}
		if _, ok := seen[kw]; ok {
			continue
		}
		seen[kw] = struct{}{}
		parts = append(parts, kw)
	}
	return strings.Join(parts, " ")
}

// EffectiveQuery 查询串：query 优先，否则把 keywords 拼成搜索表达式
func (t *GithubTask) EffectiveQuery() string {
	if t == nil {
		return ""
	}
	if t.Query != "" {
		return t.Query
	}
	return JoinKeywords(t.Keywords)
}

// GithubResult 采集命中
type GithubResult struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TaskID    string             `bson:"taskId" json:"taskId"`
	Type      string             `bson:"type" json:"type"`
	Repo      string             `bson:"repo" json:"repo"`
	Path      string             `bson:"path" json:"path"`
	URL       string             `bson:"url" json:"url"`
	Commit    string             `bson:"commit" json:"commit"`
	Title     string             `bson:"title" json:"title"`
	Snippet   string             `bson:"snippet" json:"snippet"`
	Severity  string             `bson:"severity" json:"severity"`
	Status    string             `bson:"status" json:"status"`
	TriagedBy string             `bson:"triagedBy,omitempty" json:"triagedBy,omitempty"`
	TriagedAt *time.Time         `bson:"triagedAt,omitempty" json:"triagedAt,omitempty"`
	Source    string             `bson:"source" json:"source"`
	Project   string             `bson:"project,omitempty" json:"project,omitempty"`
	Keyword   string             `bson:"keyword,omitempty" json:"keyword,omitempty"`
	CreatedAt time.Time          `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time          `bson:"updatedAt" json:"updatedAt"`
}

// GithubThreatSource 单个威胁情报来源的配置与运行状态
type GithubThreatSource struct {
	Enabled       bool       `bson:"enabled" json:"enabled"`
	Keywords      []string   `bson:"keywords" json:"keywords"`
	IntervalHours int        `bson:"intervalHours" json:"intervalHours"`
	TokenRef      string     `bson:"tokenRef,omitempty" json:"tokenRef,omitempty"`
	Targets       []string   `bson:"targets" json:"targets"`
	LastRunAt     *time.Time `bson:"lastRunAt,omitempty" json:"lastRunAt,omitempty"`
	NextRunAt     *time.Time `bson:"nextRunAt,omitempty" json:"nextRunAt,omitempty"`
	LastRunStats  string     `bson:"lastRunStats,omitempty" json:"lastRunStats,omitempty"`
}

// GithubIgnoredRepo 结果噪声仓库黑名单条目
type GithubIgnoredRepo struct {
	Repo      string    `bson:"repo" json:"repo"`
	Reason    string    `bson:"reason,omitempty" json:"reason,omitempty"`
	CreatedAt time.Time `bson:"createdAt" json:"createdAt"`
}

// GithubThreatConfig 威胁情报单例配置
type GithubThreatConfig struct {
	ID           string              `bson:"_id" json:"-"`
	CVE          GithubThreatSource  `bson:"cve" json:"cve"`
	Hacker       GithubThreatSource  `bson:"hacker" json:"hacker"`
	Tools        GithubThreatSource  `bson:"tools" json:"tools"`
	IgnoredRepos []GithubIgnoredRepo `bson:"ignoredRepos" json:"ignoredRepos"`
	UpdatedAt    time.Time           `bson:"updatedAt" json:"updatedAt"`
	UpdatedBy    string              `bson:"updatedBy,omitempty" json:"updatedBy,omitempty"`
}

// GithubThreatHistory 威胁情报发现记录（cve_history / hackers_history / tools_history）
type GithubThreatHistory struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Kind         string             `bson:"kind" json:"kind"`
	Key          string             `bson:"key" json:"key"`
	Title        string             `bson:"title" json:"title"`
	URL          string             `bson:"url" json:"url"`
	Repo         string             `bson:"repo,omitempty" json:"repo,omitempty"`
	Description  string             `bson:"description,omitempty" json:"description,omitempty"`
	Severity     string             `bson:"severity,omitempty" json:"severity,omitempty"`
	Keyword      string             `bson:"keyword,omitempty" json:"keyword,omitempty"`
	DiscoveredAt time.Time          `bson:"discoveredAt" json:"discoveredAt"`
}

// ---------- 请求 / 查询 ----------

// GithubTaskCreateRequest 新建任务
type GithubTaskCreateRequest struct {
	Name            string   `json:"name"`
	Query           string   `json:"query"`
	Keywords        []string `json:"keywords"`
	Type            string   `json:"type"`
	IntervalMinutes int      `json:"intervalMinutes"`
	MaxPages        int      `json:"maxPages"`
	TokenRef        string   `json:"tokenRef"`
	Project         string   `json:"project"`
}

// GithubIDListRequest 批量 ID 操作（stop / delete / run / recover）
type GithubIDListRequest struct {
	IDs []string `json:"ids"`
	// UnderscoreIDs 兼容 ARL-Next 的 _id 字段
	UnderscoreIDs []string `json:"_id"`
}

// AllIDs 合并 ids 与 _id
func (r GithubIDListRequest) AllIDs() []string {
	out := make([]string, 0, len(r.IDs)+len(r.UnderscoreIDs))
	out = append(out, r.IDs...)
	out = append(out, r.UnderscoreIDs...)
	return out
}

// GithubSchedulerUpdateRequest 更新周期任务（scheduler/update）
type GithubSchedulerUpdateRequest struct {
	ID              string   `json:"id"`
	UnderscoreID    string   `json:"_id"`
	Name            *string  `json:"name"`
	Query           *string  `json:"query"`
	Keywords        []string `json:"keywords"`
	IntervalMinutes *int     `json:"intervalMinutes"`
	MaxPages        *int     `json:"maxPages"`
	TokenRef        *string  `json:"tokenRef"`
}

// TaskID 兼容两种 ID 字段
func (r GithubSchedulerUpdateRequest) TaskID() string {
	if r.ID != "" {
		return r.ID
	}
	return r.UnderscoreID
}

// GithubTaskListQuery 任务列表查询
type GithubTaskListQuery struct {
	State   *int   `form:"state"`
	Type    string `form:"type"`
	Keyword string `form:"keyword"`
	// ScheduledOnly 只看带周期配置的任务（scheduler/list 用）
	ScheduledOnly bool `form:"scheduled"`
	PageIndex     int  `form:"pageIndex"`
	PageSize      int  `form:"pageSize"`
}

// GithubResultListQuery 结果列表查询（result/list 与 monitor_result/list 共用）
type GithubResultListQuery struct {
	TaskID    string `form:"taskId"`
	Type      string `form:"type"`
	Status    string `form:"status"`
	Project   string `form:"project"`
	Keyword   string `form:"keyword"`
	Source    string `form:"-"`
	PageIndex int    `form:"pageIndex"`
	PageSize  int    `form:"pageSize"`
}

// GithubResultTriageRequest 研判
type GithubResultTriageRequest struct {
	IDs    []string `json:"ids"`
	Status string   `json:"status"`
	Note   string   `json:"note"`
}

// GithubIgnoredReposRequest 黑名单仓库操作
type GithubIgnoredReposRequest struct {
	Action string `json:"action"`
	Repo   string `json:"repo"`
	Reason string `json:"reason"`
	// Purge 加黑名单时同时删除已入库的同仓库结果。
	// 单独给开关而不是默认清：清库是不可逆动作，得由调用方明确要求。
	Purge bool `json:"purge"`
}

// GithubThreatConfigRequest 威胁情报来源配置
type GithubThreatConfigRequest struct {
	Enabled       *bool    `json:"enabled"`
	Keywords      []string `json:"keywords"`
	IntervalHours *int     `json:"intervalHours"`
	TokenRef      *string  `json:"tokenRef"`
	Targets       []string `json:"targets"`
}

// GithubTargetRequest 监控目标增删（hackers_target / tools_target）
type GithubTargetRequest struct {
	Action  string   `json:"action"`
	Targets []string `json:"targets"`
	Target  string   `json:"target"`
}

// AllTargets 合并单值与数组
func (r GithubTargetRequest) AllTargets() []string {
	out := make([]string, 0, len(r.Targets)+1)
	if r.Target != "" {
		out = append(out, r.Target)
	}
	out = append(out, r.Targets...)
	return out
}

// GithubCVEHistoryQuery 威胁情报历史查询
type GithubCVEHistoryQuery struct {
	Keyword   string `form:"keyword"`
	Limit     int    `form:"limit"`
	PageIndex int    `form:"pageIndex"`
	PageSize  int    `form:"pageSize"`
}

// GithubResultOverview 按 type/status 的聚合行
type GithubResultOverviewRow struct {
	Type   string `json:"type"`
	Status string `json:"status"`
	Count  int64  `json:"count"`
}

// GithubDailyStat 按天趋势
type GithubDailyStat struct {
	Date    string `json:"date"`
	Total   int64  `json:"total"`
	New     int64  `json:"new"`
	Triaged int64  `json:"triaged"`
	Ignored int64  `json:"ignored"`
}
