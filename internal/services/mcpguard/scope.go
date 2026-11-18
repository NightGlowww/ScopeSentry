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
	ScopeNodeRead      = "node:read"
	ScopeNodeWrite     = "node:write"
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
// 空授予列表 = 历史 key（治理字段上线前创建），按向后兼容等同于 *。
func ScopeAllowed(granted []string, required string) bool {
	if len(granted) == 0 {
		return true
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
