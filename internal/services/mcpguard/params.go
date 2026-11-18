package mcpguard

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// MCP 工具入参归一（纯函数，不碰 Mongo/Redis，便于单测）。
//
// 为什么这一层需要它：MCP handler 直接调 service / repository，绕过了 gin 的
// ShouldBindJSON 与各 HTTP handler 里的校验分支（分页上限、状态取值、闭集枚举）。
// 少了这层会出现两类问题：
//  1. 资源型问题：pageSize 传 100000 会把整个集合拉进内存；
//  2. 静默错误：state=9 / type="Company" / method="get" 这类值在 Mongo 里匹配不到任何文档，
//     返回「成功 + 空列表」，调用方无法与「确实没有数据」区分（MCP_GAPS §5.1 同类问题）。
//
// 放在 mcpguard 的理由：本仓库只有这一层是「无 DB 依赖 + 纯函数 + internal/mcp 已依赖」，
// 放到 internal/mcp 里做单测会连带跑 bootstrap/mongodb 的 init（需要真实 Mongo），
// 导致参数归一逻辑无法在无库环境验证。

// 分页口径。三个上限分别对齐各自后端：
//   - ICP：internal/api/handlers/icp parsePageParams，pageSize > 200 → 400；
//   - 审计：internal/api/handlers/audit，pageSize > 200 → 400；
//   - 地契：repositories/assets/assetgroup.ListScopes，pageSize > 500 → 回落 20。
const (
	// DefaultPageSize 未传 pageSize 时的默认条数（与三个 handler 一致）
	DefaultPageSize = 20
	// MaxPageSizeICP ICP 任务列表上限
	MaxPageSizeICP = models.IcpMaxPageSize
	// MaxPageSizeAudit 服务端操作日志上限
	MaxPageSizeAudit = 200
	// MaxPageSizeAssetScope 地契列表上限
	MaxPageSizeAssetScope = 500
)

// ErrPageSizeTooLarge pageSize 超过该接口上限（对应 handler 的 400）
var ErrPageSizeTooLarge = errors.New("pageSize too large")

// ErrInvalidState 状态值不在任务状态机闭集内
var ErrInvalidState = errors.New("invalid state")

// ErrInvalidEnum 枚举值不在后端闭集内
var ErrInvalidEnum = errors.New("invalid enum value")

// ErrInvalidStatusMin statusMin 不是合法 HTTP 状态码下界
var ErrInvalidStatusMin = errors.New("invalid statusMin")

// ErrInvalidGroupID 集团 ID 既不是 ObjectID，也不是未分组标记
var ErrInvalidGroupID = errors.New("invalid groupId")

// objectIDPattern Mongo ObjectID 的十六进制形态
var objectIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

// NormalizePaging 归一 pageIndex / pageSize。
//
//   - pageIndex <= 0 → 1
//   - pageSize <= 0  → defSize（defSize <= 0 时用 DefaultPageSize）
//   - maxSize > 0 且 pageSize > maxSize → ErrPageSizeTooLarge
//
// 超上限选择报错而不是静默截断：静默截断会让调用方以为「就要到这么多条」，
// 而 handler 侧本来就是 400，报错与 REST 语义一致。
func NormalizePaging(pageIndex, pageSize, defSize, maxSize int) (int, int, error) {
	if defSize <= 0 {
		defSize = DefaultPageSize
	}
	if maxSize > 0 && pageSize > maxSize {
		return 0, 0, fmt.Errorf("%w: 最大 %d（收到 %d）", ErrPageSizeTooLarge, maxSize, pageSize)
	}
	if pageIndex <= 0 {
		pageIndex = 1
	}
	if pageSize <= 0 {
		pageSize = defSize
	}
	return pageIndex, pageSize, nil
}

// NormalizeICPTaskState 归一 ICP 任务状态过滤。
//
// nil = 不按状态过滤；闭集来自 models：0 待开始 / 1 运行中 / 2 已暂停 / 3 已完成。
// 闭集外的值在 Mongo 里恒不命中，必须挡在入口。
func NormalizeICPTaskState(state *int) (*int, error) {
	if state == nil {
		return nil, nil
	}
	v := *state
	switch v {
	case models.IcpTaskStateWaiting, models.IcpTaskStateRunning,
		models.IcpTaskStatePaused, models.IcpTaskStateDone:
		out := v
		return &out, nil
	default:
		return nil, fmt.Errorf("%w: state 只能是 0(待开始)/1(运行中)/2(已暂停)/3(已完成)，收到 %d",
			ErrInvalidState, v)
	}
}

// NormalizeICPTaskType 归一任务类型（company / domain），空串表示不过滤。
func NormalizeICPTaskType(tp string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(tp))
	switch v {
	case "":
		return "", nil
	case models.IcpTaskTypeCompany, models.IcpTaskTypeDomain:
		return v, nil
	default:
		// 允许传 "Company" / "DOMAIN"（上面的 ToLower 已处理），但拒绝未知类型：
		// icp_task.type 只存这两个字面量，传别的会返回空列表且不报错。
		return "", fmt.Errorf("%w: type 只能是 %s / %s，收到 %q",
			ErrInvalidEnum, models.IcpTaskTypeCompany, models.IcpTaskTypeDomain, tp)
	}
}

// NormalizeICPProvider 归一 provider（manual / none / http），空串表示不过滤。
func NormalizeICPProvider(provider string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(provider))
	switch v {
	case "":
		return "", nil
	case models.IcpProviderManual, models.IcpProviderNone, models.IcpProviderHTTP:
		return v, nil
	default:
		return "", fmt.Errorf("%w: provider 只能是 %s / %s / %s，收到 %q",
			ErrInvalidEnum, models.IcpProviderManual, models.IcpProviderNone, models.IcpProviderHTTP, provider)
	}
}

// auditMethods 审计日志可能出现的 HTTP 方法闭集。
// 仓库层会把 method 原样 ToUpper 后做等值匹配（repositories/audit.List），
// 传 "get" 不会报错但恒不命中，所以这里先大写再校验。
var auditMethods = map[string]struct{}{
	"GET": {}, "POST": {}, "PUT": {}, "PATCH": {}, "DELETE": {}, "HEAD": {}, "OPTIONS": {},
}

// NormalizeAuditMethod 大写并校验 HTTP 方法；空串表示不过滤。
func NormalizeAuditMethod(method string) (string, error) {
	v := strings.ToUpper(strings.TrimSpace(method))
	if v == "" {
		return "", nil
	}
	if _, ok := auditMethods[v]; !ok {
		return "", fmt.Errorf("%w: method 只能是 GET/POST/PUT/PATCH/DELETE/HEAD/OPTIONS，收到 %q",
			ErrInvalidEnum, method)
	}
	return v, nil
}

// NormalizeAuditStatusMin 归一状态码下界。
//
// 0 = 不过滤（仓库层 statusMin > 0 才加 $gte）；否则必须是 100-599 的 HTTP 状态码。
// 允许 1-99 或 600+ 会让过滤恒不命中（$gte 600 无意义），与「不过滤」难以区分。
func NormalizeAuditStatusMin(statusMin int) (int, error) {
	if statusMin == 0 {
		return 0, nil
	}
	if statusMin < 100 || statusMin > 599 {
		return 0, fmt.Errorf("%w: statusMin 需为 0(不过滤) 或 100-599 的状态码下界，收到 %d",
			ErrInvalidStatusMin, statusMin)
	}
	return statusMin, nil
}

// ungroupedMarkers 地契里表示「未分组」的取值。
// repositories: assetgroup.DissolveGroup 写空串，历史数据有 "0"，service.ListScopes 用 "unassigned" 表达同一语义。
var ungroupedMarkers = map[string]struct{}{
	"":           {},
	"0":          {},
	"unassigned": {},
}

// NormalizeScopeGroupID 归一地契列表的 groupId 过滤。
//
// asset_scope.group_id 存的是集团 ObjectID 的 hex 字符串（assetgroup.CreateScope）或空串，
// 因此：空 / "0" / "unassigned" 表示未分组，其余必须是 24 位 hex，否则过滤恒不命中。
func NormalizeScopeGroupID(groupID string) (string, error) {
	v := strings.TrimSpace(groupID)
	if _, ok := ungroupedMarkers[v]; ok {
		return v, nil
	}
	if !objectIDPattern.MatchString(v) {
		return "", fmt.Errorf("%w: groupId 需为 24 位 ObjectID，或用 \"unassigned\" 查未分组，收到 %q",
			ErrInvalidGroupID, groupID)
	}
	return v, nil
}
