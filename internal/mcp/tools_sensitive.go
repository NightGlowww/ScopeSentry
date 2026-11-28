package mcp

// 敏感信息规则（WIH 命中规则）的治理面。
//
// 和 POC / 指纹是同一个模式的第三种：MCP 早就有 list_sensitive_rules 与
// set_sensitive_rule_state，但增删改一直没有 —— 规则写错了只能去界面点，
// 自动化想「按配置反推规则」就只能读。规则是扫描输入，改不了等于模板侧断链。

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type addSensitiveRuleInput struct {
	Name    string `json:"name" jsonschema:"规则名（用于识别命中来源）"`
	Regular string `json:"regular" jsonschema:"匹配用的正则表达式，不能为空"`
	Color   string `json:"color,omitempty" jsonschema:"界面配色标记，可留空"`
	State   *bool  `json:"state,omitempty" jsonschema:"是否启用；新增时不传按启用处理"`
}

func addSensitiveRule(ctx context.Context, _ *mcp.CallToolRequest, input addSensitiveRuleInput) (*mcp.CallToolResult, any, error) {
	if d.sensitiveRuleService == nil {
		return errorResult("敏感信息规则服务未初始化", nil)
	}
	if strings.TrimSpace(input.Regular) == "" {
		return errorResult("regular 不能为空", nil)
	}
	state := true
	if input.State != nil {
		state = *input.State
	}
	id, err := d.sensitiveRuleService.RuleAdd(ginContext(ctx), strings.TrimSpace(input.Name), input.Regular, strings.TrimSpace(input.Color), state)
	if err != nil {
		return errorResult("新增敏感信息规则失败", err)
	}
	return jsonToolResult(ginH{"id": id, "state": state})
}

type updateSensitiveRuleInput struct {
	ID      string `json:"id" jsonschema:"规则 id（list_sensitive_rules 返回的 id）"`
	Name    string `json:"name" jsonschema:"规则名"`
	Regular string `json:"regular" jsonschema:"正则表达式，不能为空"`
	Color   string `json:"color,omitempty" jsonschema:"界面配色标记；不传会被清空（REST 同语义）"`
	State   *bool  `json:"state" jsonschema:"是否启用；必填 —— 漏传会把规则静默关掉"`
}

func updateSensitiveRule(ctx context.Context, _ *mcp.CallToolRequest, input updateSensitiveRuleInput) (*mcp.CallToolResult, any, error) {
	if d.sensitiveRuleService == nil {
		return errorResult("敏感信息规则服务未初始化", nil)
	}
	if strings.TrimSpace(input.ID) == "" {
		return errorResult("id 不能为空", nil)
	}
	if strings.TrimSpace(input.Regular) == "" {
		return errorResult("regular 不能为空", nil)
	}
	if input.State == nil {
		return errorResult("state 必填：漏传会把规则静默关掉", nil)
	}
	if err := d.sensitiveRuleService.RuleUpdate(ginContext(ctx), strings.TrimSpace(input.ID), strings.TrimSpace(input.Name), input.Regular, strings.TrimSpace(input.Color), *input.State); err != nil {
		return errorResult("更新敏感信息规则失败", err)
	}
	return jsonToolResult(ginH{"id": strings.TrimSpace(input.ID), "state": *input.State})
}

type deleteSensitiveRulesInput struct {
	IDs []string `json:"ids" jsonschema:"要删除的规则 id 列表，至少一个"`
}

func deleteSensitiveRules(ctx context.Context, _ *mcp.CallToolRequest, input deleteSensitiveRulesInput) (*mcp.CallToolResult, any, error) {
	if d.sensitiveRuleService == nil {
		return errorResult("敏感信息规则服务未初始化", nil)
	}
	if len(input.IDs) == 0 {
		return errorResult("ids 不能为空", nil)
	}
	deleted, err := d.sensitiveRuleService.RuleDelete(ginContext(ctx), input.IDs)
	if err != nil {
		return errorResult("删除敏感信息规则失败", err)
	}
	if deleted == 0 {
		return errorResult("没有匹配的规则（id 不存在或已删除）", nil)
	}
	return jsonToolResult(ginH{"deleted": deleted})
}
