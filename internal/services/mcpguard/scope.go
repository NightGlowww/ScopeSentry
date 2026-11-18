package mcpguard

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// MCP 治理策略层：scope 语义、工具白名单、过期与限速判定。
//
// 这一层是纯函数 + 小状态，不碰 Mongo，方便单测；数据访问在 repositories/mcp，
// 装配在 Service（见 service.go）。

// ScopeAll 超级权限
const ScopeAll = "*"

// 权限面（与 ARL-Next 的 scope 命名对齐：<资源>:<动作>）
const (
	ScopeAssetRead     = "asset:read"
	ScopeAssetWrite    = "asset:write"
	ScopeTaskRead      = "task:read"
	ScopeTaskWrite     = "task:write"
	ScopeProjectRead   = "project:read"
	ScopeProjectWrite  = "project:write"
	ScopeTemplateRead  = "template:read"
	ScopeTemplateWrite = "template:write"
	ScopePluginRead    = "plugin:read"
	ScopePluginWrite   = "plugin:write"
	ScopeNodeRead      = "node:read"
	ScopeNodeWrite     = "node:write"
	// ScopeICPWrite ICP 主体任务写操作（创建 / 停止）。
	//
	// 独立成家族而不是并进 asset:write 的理由：ICP 任务的副作用是「对外部 ICP 数据源
	// 发起查询 + 落库 + 后台 goroutine 跑全流程」，与「给资产打标签」不在一个风险面；
	// 只想给自动化开「资产写」的 key 不应该顺带获得对外查询能力。
	ScopeICPWrite = "icp:write"
)

// ToolScopes 工具名 -> 所需 scope 的唯一真相源。
//
// 挂接位置在中间件（internal/api/middleware/apikey.go）：JSON-RPC 的
// tools/call 参数里带工具名，先在这里查所需 scope，再拿 key 的 scope 做匹配。
// 新增 MCP 工具必须同步登记，否则持有受限 scope 的 key 会被拒（默认拒绝），
// 这一点由 internal/mcp 的漂移测试守住。
var ToolScopes = map[string]string{
	"list_projects":        ScopeProjectRead,
	"list_projects_data":   ScopeProjectRead,
	"get_project":          ScopeProjectRead,
	"create_project":       ScopeProjectWrite,
	"list_tasks":           ScopeTaskRead,
	"get_task":             ScopeTaskRead,
	"create_scan_task":     ScopeTaskWrite,
	"list_scan_templates":  ScopeTemplateRead,
	"get_scan_template":    ScopeTemplateRead,
	"create_scan_template": ScopeTemplateWrite,
	"list_plugin_modules":  ScopePluginRead,
	"list_plugins":         ScopePluginRead,
	"list_assets":          ScopeAssetRead,
	"count_assets":         ScopeAssetRead,
	"get_asset_detail":     ScopeAssetRead,
	"add_asset_tag":        ScopeAssetWrite,
	"list_nodes":           ScopeNodeRead,
	// 只读扩展：POC / 指纹 / 字典 / 看板，统一复用 asset:read，
	// 避免为了新增只读工具再改前端 scope 选择项
	"list_pocs":               ScopeAssetRead,
	"get_poc":                 ScopeAssetRead,
	"list_fingerprints":       ScopeAssetRead,
	"list_dictionaries":       ScopeAssetRead,
	"preview_dictionary":      ScopeAssetRead,
	"search_dictionary_lines": ScopeAssetRead,
	"get_dashboard_stats":     ScopeAssetRead,
	"get_console_info":        ScopeAssetRead,
	// 写/控：任务、扫描策略、敏感信息规则
	"get_task_progress":        ScopeTaskRead,
	"start_task":               ScopeTaskWrite,
	"stop_task":                ScopeTaskWrite,
	"delete_task":              ScopeTaskWrite,
	"list_scan_policies":       ScopeTaskRead,
	"create_scan_policy":       ScopeTaskWrite,
	"delete_scan_policies":     ScopeTaskWrite,
	"list_sensitive_rules":     ScopeAssetRead,
	"set_sensitive_rule_state": ScopeAssetWrite,
	// 节点运维 + 字典维护
	"list_node_plugins":         ScopeNodeRead,
	"get_node_logs":             ScopeNodeRead,
	"restart_node":              ScopeNodeWrite,
	"delete_nodes":              ScopeNodeWrite,
	"create_dictionary":         ScopeAssetWrite,
	"append_dictionary_lines":   ScopeAssetWrite,
	"delete_dictionary":         ScopeAssetWrite,
	"delete_dictionary_entries": ScopeAssetWrite,
	"list_export_records":       ScopeAssetRead,
	"get_export_fields":         ScopeAssetRead,
	"create_export_task":        ScopeAssetWrite,
	"delete_export_files":       ScopeAssetWrite,
	// 插件安装 / 重检 / 卸载
	"reinstall_plugin": ScopePluginWrite,
	"recheck_plugin":   ScopePluginWrite,
	"uninstall_plugin": ScopePluginWrite,
	// 资产组 / 地契 / ICP 任务 / 服务端操作日志（v2.0.87 只读扩展）
	//
	// 全部复用 asset:read，不新增 scope 家族。理由：
	//  1. 生产受限 key 的授予集是 {asset:read, task:read, plugin:read, node:read}，
	//     新增 icp:read / audit:read 会让这些 key 立刻调不到新工具，等于白补；
	//  2. 新家族还要同步前端 ApiKey.vue 的 SCOPE_OPTIONS 与 docs/MCP.md，
	//     属于授权面变更，应由「只给情报不给资产」的真实诉求驱动，不是顺手扩。
	// 语义上这四项都是「资产模型 / 资产来源 / 资产操作痕迹」，归 asset 面成立。
	// 若将来确需把审计日志单独授权（它能读到其他账号的路径与 IP），
	// 再加 audit:read 并在 ApiKey.vue 里补选项，见 docs/audit/MCP_TOOLS_V2.md。
	"list_asset_groups": ScopeAssetRead,
	"list_asset_scopes": ScopeAssetRead,
	"list_icp_tasks":    ScopeAssetRead,
	"list_audit_logs":   ScopeAssetRead,

	// 写侧第一批（v2.0.88）：只做 5 个，其余写操作（资产组编辑/重排、地契改删移动、
	// ICP 重启/恢复/删除、结果入库与地契同步）仍需人工走 Web 端。
	//
	// 与只读侧的差别：这里**不复用** asset:read，因为写操作的破坏面完全不同 ——
	// 「只读 key」不该因为补了工具就意外获得删除能力（MCP_GAPS §9 记录过现网
	// 只读 key 试图调 start_task 被 scope 拦下的案例，那次拦截是对的）。
	"create_asset_group":  ScopeAssetWrite,
	"delete_asset_groups": ScopeAssetWrite,
	"create_asset_scope":  ScopeAssetWrite,
	"create_icp_task":     ScopeICPWrite,
	"stop_icp_tasks":      ScopeICPWrite,
}

// ToolNames 返回登记过的全部工具名（供漂移测试与工具过滤使用）
func ToolNames() []string {
	names := make([]string, 0, len(ToolScopes))
	for name := range ToolScopes {
		names = append(names, name)
	}
	return names
}

// RequiredScope 查工具所需 scope；未登记的工具返回 ok=false
func RequiredScope(tool string) (string, bool) {
	scope, ok := ToolScopes[tool]
	return scope, ok
}

// ScopeMatches 单个授予项是否能覆盖所需 scope。
//
//	"*"         -> 覆盖一切
//	"asset:*"   -> 覆盖 asset:read / asset:write
//	"asset:read"-> 仅覆盖 asset:read
//
// 空授予项不覆盖任何 scope（空 scope 列表的历史 key 由 ScopeAllowed 单独放行）。
func ScopeMatches(granted, required string) bool {
	granted = strings.TrimSpace(granted)
	required = strings.TrimSpace(required)
	if granted == "" {
		return false
	}
	if granted == ScopeAll {
		return true
	}
	if required == "" {
		return false
	}
	if granted == required {
		return true
	}
	// 家族通配：asset:* 覆盖 asset:xxx
	if strings.HasSuffix(granted, ":*") {
		prefix := strings.TrimSuffix(granted, "*")
		if strings.HasPrefix(required, prefix) {
			return true
		}
	}
	// 也接受只写资源名（asset）表示该资源面的全部动作
	if !strings.Contains(granted, ":") && strings.HasPrefix(required, granted+":") {
		return true
	}
	return false
}

// ScopeAllowed 判断一组授予项是否覆盖所需 scope。
//
// 空授予列表 = 治理字段上线前创建的历史 key。过去按向后兼容等同于 *，
// 这会连带放行新增的写工具（例如 delete_asset_groups），等于历史 key 静默获得删除权。
// 现在改为：空 scope 只保留读权限（*:read），写权限必须在 key 上显式授予。
func ScopeAllowed(granted []string, required string) bool {
	if len(granted) == 0 {
		return strings.HasSuffix(strings.TrimSpace(required), ":read")
	}
	for _, g := range granted {
		if ScopeMatches(g, required) {
			return true
		}
	}
	return false
}

// HasFullScope 是否持有 *
func HasFullScope(granted []string) bool {
	for _, g := range granted {
		if strings.TrimSpace(g) == ScopeAll {
			return true
		}
	}
	return false
}

// ToolAllowed 判断工具是否在白名单内；空列表 = 全部允许
func ToolAllowed(allowed []string, tool string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, name := range allowed {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if name == ScopeAll || name == tool {
			return true
		}
		if strings.HasSuffix(name, "*") && strings.HasPrefix(tool, strings.TrimSuffix(name, "*")) {
			return true
		}
	}
	return false
}

// Check 判定一次 MCP 调用是否放行（纯函数，时间外部注入，便于单测）。
//
// 顺序：全局开关 -> key 状态 -> 过期 -> 工具白名单（key 与全局取交集）
//
//	-> scope -> 未知工具（默认拒绝）
func Check(record *models.ApiKey, cfg *models.McpConfig, tool string, now time.Time) error {
	if cfg != nil && !cfg.Enabled {
		return ErrMCPDisabled
	}
	if record == nil {
		return ErrInvalidKey
	}
	if !record.Enabled {
		return ErrKeyDisabled
	}
	if record.IsExpired(now) {
		return ErrKeyExpired
	}
	// tools/call 以外的 JSON-RPC 方法（initialize / tools/list / ping）没有工具名，
	// 只要 key 有效就放行，避免 MCP 握手被治理字段卡死。
	if tool == "" {
		return nil
	}
	if !ToolAllowed(record.Tools, tool) {
		return fmt.Errorf("%w: %s", ErrToolNotAllowed, tool)
	}
	if cfg != nil && !ToolAllowed(cfg.AllowedTools, tool) {
		return fmt.Errorf("%w: %s (globally blocked)", ErrToolNotAllowed, tool)
	}
	required, ok := RequiredScope(tool)
	if !ok {
		// 未登记的工具默认拒绝：只放行持 * 的 key，防止新工具悄悄绕过治理
		if HasFullScope(record.Scope) {
			return nil
		}
		return fmt.Errorf("%w: %s", ErrToolNotAllowed, tool)
	}
	if !ScopeAllowed(record.Scope, required) {
		return fmt.Errorf("%w: need %s", ErrScopeDenied, required)
	}
	return nil
}

// PermittedTools 计算某 key 实际可用的工具集合。
// 返回 nil 表示「不限制」（key 持 * 且 key/全局都没有工具白名单）。
// 该集合会塞进请求 context，供 internal/mcp 过滤 tools/list，避免列出无权调用的工具。
func PermittedTools(record *models.ApiKey, cfg *models.McpConfig) map[string]bool {
	if record == nil {
		return nil
	}
	if HasFullScope(record.Scope) && len(record.Tools) == 0 && (cfg == nil || len(cfg.AllowedTools) == 0) {
		return nil
	}
	now := time.Now()
	permitted := make(map[string]bool, len(ToolScopes))
	for tool := range ToolScopes {
		if Check(record, cfg, tool, now) == nil {
			permitted[tool] = true
		}
	}
	return permitted
}

type permittedToolsCtxKey struct{}

// WithPermittedTools 把允许工具集合写进请求 context（nil = 不限制）
func WithPermittedTools(ctx context.Context, permitted map[string]bool) context.Context {
	return context.WithValue(ctx, permittedToolsCtxKey{}, permitted)
}

// PermittedToolsFrom 从 context 取允许工具集合；没有该值时返回 nil（= 不限制）
func PermittedToolsFrom(ctx context.Context) map[string]bool {
	if ctx == nil {
		return nil
	}
	if v, ok := ctx.Value(permittedToolsCtxKey{}).(map[string]bool); ok {
		return v
	}
	return nil
}
