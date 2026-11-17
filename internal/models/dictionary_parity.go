// models-------------------------------------
// @file      : dictionary_parity.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2026/11/18 02:05
// -------------------------------------------

package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 字典种类：对标 ARL-Next 的 dictionary.py / brute_dict.py / cdn_dict.py。
// 取值即 dictionary 集合里的 category 字段，历史数据（dir / subdomain）无需迁移。
const (
	DictKindDir       = "dir"
	DictKindSubdomain = "subdomain"
	DictKindBrute     = "brute"
	DictKindCDN       = "cdn"
	DictKindOther     = "other"
)

// DictionaryKinds 合法 kind 列表（顺序即前端展示顺序）
var DictionaryKinds = []string{
	DictKindDir,
	DictKindSubdomain,
	DictKindBrute,
	DictKindCDN,
	DictKindOther,
}

// DictionaryEntry 字典行。
// 字典内容以「行」为粒度落库：唯一索引 (dict_id, value) 负责去重，
// (dict_id, seq) 负责按写入顺序预览/导出，避免每次追加都全量读写文件。
type DictionaryEntry struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	DictID    string             `bson:"dict_id" json:"dictId"`
	Category  string             `bson:"category" json:"category"`
	Value     string             `bson:"value" json:"value"`
	Seq       int64              `bson:"seq" json:"seq"`
	CreatedAt time.Time          `bson:"created_at,omitempty" json:"createdAt"`
}

// DictionaryKindCount 每个 kind 下的字典数量
type DictionaryKindCount struct {
	Kind  string `json:"kind" bson:"_id"`
	Count int64  `json:"count" bson:"count"`
}

// DictionaryItem 列表行
type DictionaryItem struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Category  string    `json:"category"`
	Size      string    `json:"size"`
	LineCount int64     `json:"lineCount"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// DictionaryListRequest 字典列表查询
type DictionaryListRequest struct {
	// Kind 为空表示全部 kind
	Kind      string `json:"kind" form:"kind"`
	Search    string `json:"search" form:"search" binding:"max=128"`
	PageIndex int    `json:"pageIndex" form:"pageIndex"`
	PageSize  int    `json:"pageSize" form:"pageSize"`
}

// DictionaryListResult 列表返回
type DictionaryListResult struct {
	List      []DictionaryItem      `json:"list"`
	Total     int64                 `json:"total"`
	PageIndex int                   `json:"pageIndex"`
	PageSize  int                   `json:"pageSize"`
	Kinds     []DictionaryKindCount `json:"kinds"`
}

// DictionaryPreviewRequest 预览前 N 行
type DictionaryPreviewRequest struct {
	Kind  string `json:"kind" binding:"required"`
	Name  string `json:"name" binding:"required"`
	Limit int    `json:"limit"`
}

// DictionaryPreviewResult 预览返回
type DictionaryPreviewResult struct {
	Kind      string   `json:"kind"`
	Name      string   `json:"name"`
	Total     int64    `json:"total"`
	Lines     []string `json:"lines"`
	Limit     int      `json:"limit"`
	Truncated bool     `json:"truncated"`
}

// DictionarySearchRequest 行搜索
type DictionarySearchRequest struct {
	Kind    string `json:"kind" binding:"required"`
	Name    string `json:"name" binding:"required"`
	Keyword string `json:"keyword" binding:"required"`
	Limit   int    `json:"limit"`
}

// DictionaryMatch 命中的一行
type DictionaryMatch struct {
	Value string `json:"value"`
	Seq   int64  `json:"seq"`
}

// DictionarySearchResult 搜索返回
type DictionarySearchResult struct {
	Kind      string            `json:"kind"`
	Name      string            `json:"name"`
	Keyword   string            `json:"keyword"`
	Matches   []DictionaryMatch `json:"matches"`
	Returned  int               `json:"returned"`
	Limit     int               `json:"limit"`
	Truncated bool              `json:"truncated"`
}

// DictionaryAppendRequest 追加行（自动去重）
type DictionaryAppendRequest struct {
	Kind  string   `json:"kind" binding:"required"`
	Name  string   `json:"name" binding:"required"`
	Lines []string `json:"lines" binding:"required"`
}

// DictionaryAppendResult 追加结果
type DictionaryAppendResult struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Added      int64  `json:"added"`
	Duplicated int64  `json:"duplicated"`
	Total      int64  `json:"total"`
}

// DictionaryDeleteEntriesRequest 删除指定行
type DictionaryDeleteEntriesRequest struct {
	Kind    string   `json:"kind" binding:"required"`
	Name    string   `json:"name" binding:"required"`
	Entries []string `json:"entries" binding:"required"`
}

// DictionaryDeleteEntriesResult 删除行结果
type DictionaryDeleteEntriesResult struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Deleted int64  `json:"deleted"`
	Total   int64  `json:"total"`
}

// DictionaryDeleteFileRequest 删除整个字典
type DictionaryDeleteFileRequest struct {
	Kind string `json:"kind" binding:"required"`
	Name string `json:"name" binding:"required"`
}

// DictionaryDeleteFileResult 删除结果
type DictionaryDeleteFileResult struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Deleted bool   `json:"deleted"`
}

// DictionaryCreateRequest 新建字典
type DictionaryCreateRequest struct {
	Kind    string `json:"kind" binding:"required"`
	Name    string `json:"name" binding:"required"`
	Content string `json:"content"`
}

// DictionaryCreateResult 新建/覆盖结果
type DictionaryCreateResult struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	LineCount int64  `json:"lineCount"`
	Size      string `json:"size"`
	Replaced  bool   `json:"replaced"`
}

// DictionarySaveRequest dict_cdn / dict_brute 的 save：按 kind 保存整份内容
type DictionarySaveRequest struct {
	Name    string   `json:"name" binding:"required"`
	Content string   `json:"content"`
	Lines   []string `json:"lines"`
}

// 字典上传任务状态（pending → running → done / failed）
const (
	DictUploadStatusPending = "pending"
	DictUploadStatusRunning = "running"
	DictUploadStatusDone    = "done"
	DictUploadStatusFailed  = "failed"
)

// 上传模式
const (
	DictUploadModeCreate = "create"
	DictUploadModeAppend = "append"
)

// DictionaryUploadTask 大文件上传任务
type DictionaryUploadTask struct {
	TaskID         string     `json:"taskId" bson:"task_id"`
	Kind           string     `json:"kind" bson:"kind"`
	Name           string     `json:"name" bson:"name"`
	Mode           string     `json:"mode" bson:"mode"`
	Status         string     `json:"status" bson:"status"`
	TotalLines     int64      `json:"totalLines" bson:"total_lines"`
	ProcessedLines int64      `json:"processedLines" bson:"processed_lines"`
	Added          int64      `json:"added" bson:"added"`
	Duplicated     int64      `json:"duplicated" bson:"duplicated"`
	Progress       float64    `json:"progress" bson:"progress"`
	Error          string     `json:"error" bson:"error"`
	CreatedAt      time.Time  `json:"createdAt" bson:"created_at"`
	UpdatedAt      time.Time  `json:"updatedAt" bson:"updated_at"`
	FinishedAt     *time.Time `json:"finishedAt,omitempty" bson:"finished_at,omitempty"`
}
