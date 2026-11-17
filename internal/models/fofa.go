package models

import (
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// FOFA 批量建任务（对标 ARL-Next backend/app/routes/taskFofa.py）。
//
// 与 ARL 的差异：ARL 的 taskFofa 一次只建一个任务（把 fofa 查到的 IP 列表塞进 celery），
// 这里把「拉 host」和「建任务」拆成两个动作，支持两种落库方式：
//   - per-host：一个 host 一个任务（把一个查询命中的资产拆到不同任务/模板）；
//   - single：一个任务 N 个目标（等价 ARL 的原始行为）。
//
// fofa_import 记录每次导入的 query / hosts / taskIds，用来支撑
// 「同一 query 的 host 已存在于最近一次导入记录就跳过」的去重语义。

// provider 名称
const (
	// FofaProviderHTTP FOFA 官方 API（fofa.key / FOFA_BASE_URL）
	FofaProviderHTTP = "http"
	// FofaProviderNone 未配置 / 显式关闭，调用时返回明确原因
	FofaProviderNone = "none"
)

// 建任务模式
const (
	// FofaCreateModePerHost 每个 host 建一个任务
	FofaCreateModePerHost = "per-host"
	// FofaCreateModeSingle 一个任务包含全部 host
	FofaCreateModeSingle = "single"
)

const (
	// FofaMaxPageSize 导入历史分页上限，超过直接 400（与 ICP / GitHub 同一口径）
	FofaMaxPageSize = 200
	// FofaDefaultPageSize 导入历史默认分页
	FofaDefaultPageSize = 20

	// FofaMaxPreviewSize search 预览单页上限（任务契约：size ≤ 100）
	FofaMaxPreviewSize = 100
	// FofaDefaultPreviewSize search 预览默认条数
	FofaDefaultPreviewSize = 20

	// FofaMaxImportSize 一次导入最多拉取多少个 host（任务契约：size 上限 1000）
	FofaMaxImportSize = 1000
	// FofaDefaultImportSize 建任务时 size 未填的默认值
	FofaDefaultImportSize = 100

	// FofaMaxTasksPerImport 单次最多建多少个任务（任务契约：per-host 最多 50）
	FofaMaxTasksPerImport = 50

	// FofaHostPageSize 向 provider 拉 host 的单页条数（FOFA search 单页上限 100）
	FofaHostPageSize = 100

	// FofaMaxTaskNameRunes 任务名长度上限（与 ICP 任务名同口径）
	FofaMaxTaskNameRunes = 120

	// FofaMaxErrors 响应体里 errors[] 的保留上限：
	// 上千条失败全量回传会把响应打爆，只留前 N 条，failed 计数仍然完整
	FofaMaxErrors = 200
)

// FofaBalance provider 返回的余额（字段对齐 FOFA /api/v1/info/my）
type FofaBalance struct {
	Username        string   `json:"username,omitempty"`
	FofaPoint       *float64 `json:"fofaPoint,omitempty"`
	RemainFreePoint *float64 `json:"remainFreePoint,omitempty"`
	RemainAPIQuery  *float64 `json:"remainApiQuery,omitempty"`
	VIPLevel        int      `json:"vipLevel,omitempty"`
	IsVIP           bool     `json:"isVip,omitempty"`
}

// FofaImport 一次批量导入记录（去重 + 历史都靠它）
type FofaImport struct {
	ID primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	// Query 原始 FOFA 查询语句（不做 base64，保持可读；去重按原样字符串比较）
	Query string `bson:"query" json:"query"`
	// Hosts 这个 query 累计已建过任务的 host（归一后的 host[:port]）：
	// 本次建成功的 + 上一次记录里的（含当时跳过的）。失败的不写入，
	// 保证同名冲突 / provider 抖动之后还能重试；下一次同 query 导入按这个集合去重。
	Hosts     []string `bson:"hosts" json:"hosts"`
	TaskIDs   []string `bson:"taskIds" json:"taskIds"`
	TaskNames []string `bson:"taskNames" json:"taskNames"`
	// CreateMode per-host / single
	CreateMode string `bson:"createMode" json:"createMode"`
	// Size 本次实际拉到的 host 数
	Size int `bson:"size" json:"size"`
	// Project / TemplateID 建任务时使用的参数（便于回查）
	Project    string `bson:"project,omitempty" json:"project,omitempty"`
	TemplateID string `bson:"templateId,omitempty" json:"templateId,omitempty"`
	// Created / Skipped / Failed 与响应体同口径
	Created int `bson:"created" json:"created"`
	Skipped int `bson:"skipped" json:"skipped"`
	Failed  int `bson:"failed" json:"failed"`
	// Errors 逐条失败记录（最多 FofaMaxErrors 条）
	Errors    []FofaImportError `bson:"errors,omitempty" json:"errors,omitempty"`
	CreatedAt time.Time         `bson:"createdAt" json:"createdAt"`
}

// FofaImportError 单条失败记录（逐条记录，不中断整批）
type FofaImportError struct {
	Host  string `bson:"host" json:"host"`
	Error string `bson:"error" json:"error"`
}

// FofaImportResult 建任务响应
type FofaImportResult struct {
	ImportID   string            `json:"importId"`
	Query      string            `json:"query"`
	CreateMode string            `json:"createMode"`
	Size       int               `json:"size"`
	Created    int               `json:"created"`
	Skipped    int               `json:"skipped"`
	Failed     int               `json:"failed"`
	TaskIDs    []string          `json:"taskIds"`
	TaskNames  []string          `json:"taskNames"`
	Hosts      []string          `json:"hosts"`
	Errors     []FofaImportError `json:"errors"`
}

// FofaSearchRequest search 入参（字段名与任务契约一致：query/page/size）
type FofaSearchRequest struct {
	Query string `json:"query"`
	Page  int    `json:"page"`
	Size  int    `json:"size"`
}

// FofaTaskRequest 建任务入参
//
// createMode 必填：per-host（一 host 一任务，最多 50）或 single（一任务 N 目标，N ≤ 1000）。
type FofaTaskRequest struct {
	Query      string `json:"query"`
	Size       int    `json:"size"`
	TaskName   string `json:"taskName"`
	Project    string `json:"project"`
	TemplateID string `json:"templateId"`
	CreateMode string `json:"createMode"`
}

// FofaImportListQuery 导入历史查询
type FofaImportListQuery struct {
	Query     string
	PageIndex int
	PageSize  int
}

// NormalizeFofaHost 归一 FOFA 返回的 host。
//
// 规则（任务契约）：去协议、去 userinfo、去路径/查询/片段、去尾点，
// 只保留 host[:port]；IPv6 保留方括号形式（[2001:db8::1]:443）。
// 明显非法（空、含空白/引号/逗号等、超长）返回空串，调用方跳过。
func NormalizeFofaHost(raw string) string {
	host := strings.ToLower(strings.TrimSpace(raw))
	if host == "" {
		return ""
	}
	// 去协议（http:// / https:// / ftp:// / 任意 scheme）
	if idx := strings.Index(host, "://"); idx >= 0 {
		host = host[idx+3:]
	}
	// 去路径 / 查询 / 片段
	if idx := strings.IndexAny(host, "/?#"); idx >= 0 {
		host = host[:idx]
	}
	// 去 userinfo（最后一个 @ 之后是 authority）
	if idx := strings.LastIndex(host, "@"); idx >= 0 {
		host = host[idx+1:]
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}

	name, port := host, ""
	if strings.HasPrefix(host, "[") {
		// IPv6 字面量：[::1] 或 [::1]:8443
		if end := strings.Index(host, "]"); end > 0 {
			name = host[:end+1]
			rest := host[end+1:]
			if strings.HasPrefix(rest, ":") {
				port = rest[1:]
			} else if rest != "" {
				return ""
			}
		}
	} else if strings.Count(host, ":") == 1 {
		name, port, _ = strings.Cut(host, ":")
	}
	// 域名尾点（a.com. -> a.com）
	name = strings.TrimSuffix(name, ".")
	port = strings.TrimSpace(port)
	if name == "" || name == "." || len(name) > 300 || !validFofaHostRunes(name) {
		return ""
	}
	if port == "" {
		return name
	}
	for _, r := range port {
		if r < '0' || r > '9' {
			// 端口不是数字：丢掉端口而不是丢掉整个 host
			return name
		}
	}
	return name + ":" + port
}

// NormalizeFofaHosts 批量归一 + 去重保序，返回归一后的 host 与被丢弃的条数
func NormalizeFofaHosts(raw []string) ([]string, int) {
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	dropped := 0
	for _, item := range raw {
		host := NormalizeFofaHost(item)
		if host == "" {
			dropped++
			continue
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		out = append(out, host)
	}
	return out, dropped
}

// validFofaHostRunes host[:port] 允许的字符集（域名 / IPv4 / 带方括号的 IPv6）
func validFofaHostRunes(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '-' || r == ':' || r == '[' || r == ']' || r == '_':
		default:
			return false
		}
	}
	return true
}
