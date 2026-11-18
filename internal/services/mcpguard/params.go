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

// ---------------------------------------------------------------------------
// 写侧入参归一（v2.0.88：资产组 / 地契 / ICP 任务）
//
// 与只读侧同一套理由（见文件头）：MCP 不经 gin 的 ShouldBindJSON，handler 里的
// binding:"required"、长度上限、闭集枚举都不会生效。写操作的「静默失败」比读更贵 ——
// 少一个参数就建出一个空壳地契，ids 里混一个非法值就可能删错/半删。
// 所以这一批一律「先归一再落库」，越界一律报错。
// ---------------------------------------------------------------------------

const (
	// MaxDeleteAssetGroupIDs delete_asset_groups 单次删除的集团数上限。
	// 删除是逐个集团调 service.DeleteGroup（该服务没有批量接口，每次都要搬地契），
	// 批量放大后单次请求耗时线性上升，20 是「能用且不会挂住连接」的折中。
	MaxDeleteAssetGroupIDs = 20
	// MaxStopICPIDs stop_icp_tasks 单次停止的任务数上限（仓库层一条 UpdateMany，可放大）
	MaxStopICPIDs = 50
	// MaxAssetGroupNameRunes 集团名上限，对齐 assetgroup.CreateGroup 的 1-64
	MaxAssetGroupNameRunes = 64
	// MaxAssetScopeNameRunes 地契名上限，对齐 assetgroup.buildScope 的 1-128
	MaxAssetScopeNameRunes = 128
	// MaxICPTaskNameRunes ICP 任务名上限，对齐 icp.CreateTask 的 maxTaskNameRunes
	MaxICPTaskNameRunes = 128
)

// 资产集团删除策略（对齐 assetgroup.DeleteGroup 的 action 取值）
const (
	// AssetGroupActionDissolve 地契移入未分组后再删集团（默认）
	AssetGroupActionDissolve = "dissolve"
	// AssetGroupActionTransfer 地契划转到 targetGroupId 后再删集团
	AssetGroupActionTransfer = "transfer"
)

// 地契类型（对齐 assetgroup.ScopeType* 常量，service.buildScope 只认这三个字面量）
const (
	// AssetScopeTypeDomain 纯域名
	AssetScopeTypeDomain = "domain"
	// AssetScopeTypeIP 纯 IP
	AssetScopeTypeIP = "ip"
	// AssetScopeTypeMixed 域名 + IP 混合
	AssetScopeTypeMixed = "mixed"
)

var (
	// ErrConfirmRequired 高危工具缺少 confirm=true
	ErrConfirmRequired = errors.New("confirm is required")
	// ErrIDsRequired ids 缺失或全是空串
	ErrIDsRequired = errors.New("ids is required")
	// ErrTooManyIDs ids 超过单次上限
	ErrTooManyIDs = errors.New("too many ids")
	// ErrInvalidObjectID 不是 24 位十六进制 ObjectID
	ErrInvalidObjectID = errors.New("invalid object id")
	// ErrEmptyField 必填字段为空
	ErrEmptyField = errors.New("required field is empty")
	// ErrFieldTooLong 字段超长
	ErrFieldTooLong = errors.New("field too long")
	// ErrInvalidAction 删除策略非法
	ErrInvalidAction = errors.New("invalid action")
	// ErrTargetGroupRequired transfer 缺目标集团
	ErrTargetGroupRequired = errors.New("targetGroupId is required")
	// ErrEmptyQueries ICP 任务没有任何查询目标
	ErrEmptyQueries = errors.New("queries is required")
)

// RequireConfirm 高危（不可逆）工具的确认闸。
//
// 只在 MCP 侧加，不改服务层：REST 有自己的前端二次确认（ElMessageBox），
// MCP 的调用方是 Agent，没有 UI 兜底，必须由协议参数显式确认。
// 缺 confirm 一律报错而不是「默认执行」—— 写错一次的代价由调用方承担，不是由数据承担。
func RequireConfirm(confirm bool, tool string) error {
	if confirm {
		return nil
	}
	return fmt.Errorf("%w: %s 是不可逆操作，确认执行请在参数里显式传 \"confirm\": true", ErrConfirmRequired, tool)
}

// NormalizeBatchIDs 归一 ObjectID 批量入参：去空白 → 小写 → 去重（保序）→ 校验数量与格式。
//
// 顺序有意为之：先去重再判上限，这样「传了 21 个但其中 2 个重复」不会被误拒。
func NormalizeBatchIDs(field string, ids []string, max int) ([]string, error) {
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, raw := range ids {
		id := strings.ToLower(strings.TrimSpace(raw))
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: %s 至少给一个 ObjectID", ErrIDsRequired, field)
	}
	if max > 0 && len(out) > max {
		return nil, fmt.Errorf("%w: %s 单次最多 %d 个（去重后收到 %d）", ErrTooManyIDs, field, max, len(out))
	}
	for _, id := range out {
		if !objectIDPattern.MatchString(id) {
			return nil, fmt.Errorf("%w: %s 里的 %q 不是 24 位 ObjectID", ErrInvalidObjectID, field, id)
		}
	}
	return out, nil
}

// NormalizeDeleteAssetGroupIDs delete_asset_groups 的 ids 归一（上限 20）
func NormalizeDeleteAssetGroupIDs(ids []string) ([]string, error) {
	return NormalizeBatchIDs("ids", ids, MaxDeleteAssetGroupIDs)
}

// NormalizeStopICPIDs stop_icp_tasks 的 ids 归一（上限 50）
func NormalizeStopICPIDs(ids []string) ([]string, error) {
	return NormalizeBatchIDs("ids", ids, MaxStopICPIDs)
}

// NormalizeWriteName 写操作的名称字段归一：去空白、非空、长度上限（按 rune 计，与 service 一致）。
//
// 字符数用 rune 而不是 len()：中文集团名按字节算会被误判超长。
func NormalizeWriteName(field, value string, maxRunes int) (string, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return "", fmt.Errorf("%w: %s 不能为空", ErrEmptyField, field)
	}
	if n := len([]rune(v)); maxRunes > 0 && n > maxRunes {
		return "", fmt.Errorf("%w: %s 不能超过 %d 个字符（收到 %d）", ErrFieldTooLong, field, maxRunes, n)
	}
	return v, nil
}

// NormalizeAssetGroupAction 归一集团删除策略，返回 (action, targetGroupID, err)。
//
// 语义对齐 assetgroup.DeleteGroup：dissolve 把地契移入未分组，transfer 划转到目标集团。
// 两处收紧：
//  1. action 传空按 dissolve（与服务层一致），但显式传了 targetGroupId 又选 dissolve 时报错，
//     避免调用方以为「指定了目标集团就不会丢归类」；
//  2. transfer 的目标必须是被删集合之外的合法 ObjectID —— 服务层的检查是
//     targetGroupID == req.ID，批量场景下只挡了「自己转自己」，挡不住「A 转给同批的 B、B 转给 A」。
func NormalizeAssetGroupAction(action, targetGroupID string, ids []string) (string, string, error) {
	v := strings.ToLower(strings.TrimSpace(action))
	target := strings.ToLower(strings.TrimSpace(targetGroupID))
	if v == "" {
		v = AssetGroupActionDissolve
	}
	switch v {
	case AssetGroupActionDissolve:
		if target != "" {
			return "", "", fmt.Errorf("%w: action=dissolve 不接受 targetGroupId（地契会移入未分组）；要划转请用 action=transfer", ErrInvalidAction)
		}
		return v, "", nil
	case AssetGroupActionTransfer:
		if target == "" {
			return "", "", fmt.Errorf("%w: action=transfer 必须同时给 targetGroupId", ErrTargetGroupRequired)
		}
		if !objectIDPattern.MatchString(target) {
			return "", "", fmt.Errorf("%w: targetGroupId %q 不是 24 位 ObjectID", ErrInvalidObjectID, targetGroupID)
		}
		for _, id := range ids {
			if id == target {
				return "", "", fmt.Errorf("%w: targetGroupId 不能是本批要删除的集团之一（%s）", ErrInvalidAction, target)
			}
		}
		return v, target, nil
	default:
		return "", "", fmt.Errorf("%w: action 只能是 %s / %s，收到 %q",
			ErrInvalidAction, AssetGroupActionDissolve, AssetGroupActionTransfer, action)
	}
}

// NormalizeScopeOwnerGroupID 归一地契的归属集团（写侧）。
//
// 与只读侧 NormalizeScopeGroupID 的差别：写侧不接受 "0" / "unassigned" 这两个
// 「未分组」标记 —— 它们在库里是历史数据形态，不是可以写进去的归属值；
// 服务层 ensureGroup 会把它们当非法 ObjectID 拒掉，但那是在开建之后才报错。
// 写侧约定：空字符串 = 不归属任何集团（未分组）。
func NormalizeScopeOwnerGroupID(groupID string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(groupID))
	if v == "" {
		return "", nil
	}
	if _, ok := ungroupedMarkers[v]; ok {
		return "", fmt.Errorf("%w: groupId 写侧只接受 24 位 ObjectID 或留空（未分组）；%q 是只读过滤用的标记", ErrInvalidGroupID, groupID)
	}
	if !objectIDPattern.MatchString(v) {
		return "", fmt.Errorf("%w: groupId 需为 24 位 ObjectID，或留空表示未分组，收到 %q", ErrInvalidGroupID, groupID)
	}
	return v, nil
}

// NormalizeAssetScopeType 归一地契类型；空串表示让服务端按内容推断（domain / ip / mixed）。
func NormalizeAssetScopeType(scopeType string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(scopeType))
	switch v {
	case "":
		return "", nil
	case AssetScopeTypeDomain, AssetScopeTypeIP, AssetScopeTypeMixed:
		return v, nil
	default:
		return "", fmt.Errorf("%w: scopeType 只能是 %s / %s / %s，或留空自动推断，收到 %q",
			ErrInvalidEnum, AssetScopeTypeDomain, AssetScopeTypeIP, AssetScopeTypeMixed, scopeType)
	}
}

// NormalizeAssetScopeRange 归一地契范围文本。
//
// 只做「非空 + 至少一个条目」；域名/IP 的逐条校验留在 service.buildScope ——
// 那里的报错带具体条目（"%q is not a valid domain or ip"），比在这层重写一遍更准。
func NormalizeAssetScopeRange(scope string) (string, error) {
	v := strings.TrimSpace(scope)
	if v == "" {
		return "", fmt.Errorf("%w: scope 不能为空，需给至少一个域名或 IP（逗号/换行/分号分隔）", ErrEmptyField)
	}
	fields := strings.FieldsFunc(v, func(r rune) bool {
		switch r {
		case '\n', '\r', '\t', ',', ';', ' ':
			return true
		}
		return false
	})
	if len(fields) == 0 {
		return "", fmt.Errorf("%w: scope 里没有可解析的条目（只有分隔符）", ErrEmptyField)
	}
	return v, nil
}

// NormalizeICPTypeForCreate 归一 ICP 任务类型（创建用）：空值按服务层语义回落 domain。
func NormalizeICPTypeForCreate(taskType string) (string, error) {
	v, err := NormalizeICPTaskType(taskType)
	if err != nil {
		return "", err
	}
	if v == "" {
		return models.IcpTaskTypeDomain, nil
	}
	return v, nil
}

// NormalizeICPQueriesForCreate 归一 ICP 任务的查询目标。
//
// 完全复用 models.NormalizeIcpQueries（去空、去重、domain 类型小写去尾点），
// 只在外面补两件服务层不会替调用方做的事：
//  1. queries 为空且给了 target 时，用 target 当唯一查询（与 icp.CreateTask 同一规则）；
//  2. 归一后为空直接报错 —— 否则会走到服务层 ErrEmptyQueries，错误信息里没有参数名，
//     Agent 只能靠猜是 queries 还是 target 的问题。
func NormalizeICPQueriesForCreate(taskType string, queries []string, target string) ([]string, error) {
	raw := queries
	if len(raw) == 0 {
		if t := strings.TrimSpace(target); t != "" {
			raw = []string{t}
		}
	}
	out := models.NormalizeIcpQueries(taskType, raw)
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: queries 或 target 至少给一个（type=company 填企业名，type=domain 填域名）", ErrEmptyQueries)
	}
	return out, nil
}
