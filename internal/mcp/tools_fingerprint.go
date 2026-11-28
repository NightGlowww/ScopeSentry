package mcp

// 模板库与情报处置的写入口。
//
// 这一组工具的共性：REST 早就有、MCP 一直只有读。对自动化来说「看得见但改不了」
// 等于闭环断在中间 —— 发现了泄露结果不能标研判、发现周期任务失控不能停、
// 指纹规则写错不能改。都是库内改动，风险与 REST 完全同级。

import (
	"context"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---------------------------------------------------------------------------
// 指纹规则
// ---------------------------------------------------------------------------

type fingerprintRuleInput struct {
	Name           string `json:"name" jsonschema:"规则名；同名已存在时按更新处理，不会产生重复规则"`
	Rule           string `json:"rule" jsonschema:"匹配规则（YAML 文本），不能为空"`
	FingerprintID  string `json:"fingerprintId,omitempty" jsonschema:"规则标识；留空自动生成 SAA_xxx"`
	Category       string `json:"category,omitempty" jsonschema:"分类，如 CMS / OA / 中间件"`
	ParentCategory string `json:"parentCategory,omitempty" jsonschema:"父分类"`
	Company        string `json:"company,omitempty" jsonschema:"厂商或产品归属"`
	State          *bool  `json:"state,omitempty" jsonschema:"是否启用；新增时不传按启用处理"`
}

func (in fingerprintRuleInput) toModel(state bool) models.FingerprintRule {
	return models.FingerprintRule{
		Name:           strings.TrimSpace(in.Name),
		Rule:           in.Rule,
		FingerprintId:  strings.TrimSpace(in.FingerprintID),
		Category:       strings.TrimSpace(in.Category),
		ParentCategory: strings.TrimSpace(in.ParentCategory),
		Company:        strings.TrimSpace(in.Company),
		State:          state,
	}
}

func addFingerprint(ctx context.Context, _ *mcp.CallToolRequest, input fingerprintRuleInput) (*mcp.CallToolResult, any, error) {
	if d.fingerprintService == nil {
		return errorResult("指纹服务未初始化", nil)
	}
	if strings.TrimSpace(input.Name) == "" {
		return errorResult("name 不能为空", nil)
	}
	if strings.TrimSpace(input.Rule) == "" {
		return errorResult("rule 不能为空", nil)
	}
	// 新增默认启用：漏传 state 反而建出一条不生效的规则，是更难发现的错。
	state := true
	if input.State != nil {
		state = *input.State
	}
	id, err := d.fingerprintService.Add(ctx, input.toModel(state))
	if err != nil {
		return errorResult("新增指纹失败", err)
	}
	return jsonToolResult(ginH{"id": id, "state": state})
}

type updateFingerprintInput struct {
	ID             string `json:"id" jsonschema:"指纹规则 id（list_fingerprints 返回的 id）"`
	Name           string `json:"name" jsonschema:"规则名"`
	Rule           string `json:"rule" jsonschema:"匹配规则（YAML 文本），不能为空"`
	State          *bool  `json:"state" jsonschema:"是否启用；必填 —— 这是全量更新，漏传会把规则静默关掉"`
	Category       string `json:"category,omitempty" jsonschema:"分类；不传会被清空（REST 同语义）"`
	ParentCategory string `json:"parentCategory,omitempty" jsonschema:"父分类；不传会被清空"`
	Company        string `json:"company,omitempty" jsonschema:"厂商；不传会被清空"`
}

func updateFingerprint(ctx context.Context, _ *mcp.CallToolRequest, input updateFingerprintInput) (*mcp.CallToolResult, any, error) {
	if d.fingerprintService == nil {
		return errorResult("指纹服务未初始化", nil)
	}
	if strings.TrimSpace(input.ID) == "" {
		return errorResult("id 不能为空", nil)
	}
	if strings.TrimSpace(input.Rule) == "" {
		return errorResult("rule 不能为空", nil)
	}
	if input.State == nil {
		return errorResult("state 必填：这是全量更新，漏传会把规则静默关掉", nil)
	}
	data := models.FingerprintRule{
		Name:           strings.TrimSpace(input.Name),
		Rule:           input.Rule,
		Category:       strings.TrimSpace(input.Category),
		ParentCategory: strings.TrimSpace(input.ParentCategory),
		Company:        strings.TrimSpace(input.Company),
		State:          *input.State,
	}
	if err := d.fingerprintService.Update(ctx, strings.TrimSpace(input.ID), data); err != nil {
		return errorResult("更新指纹失败", err)
	}
	return jsonToolResult(ginH{"id": strings.TrimSpace(input.ID), "state": *input.State})
}

type deleteFingerprintsInput struct {
	IDs []string `json:"ids" jsonschema:"要删除的指纹规则 id 列表，至少一个"`
}

func deleteFingerprints(ctx context.Context, _ *mcp.CallToolRequest, input deleteFingerprintsInput) (*mcp.CallToolResult, any, error) {
	if d.fingerprintService == nil {
		return errorResult("指纹服务未初始化", nil)
	}
	if len(input.IDs) == 0 {
		return errorResult("ids 不能为空", nil)
	}
	deleted, err := d.fingerprintService.Delete(ctx, input.IDs)
	if err != nil {
		return errorResult("删除指纹失败", err)
	}
	if deleted == 0 {
		return errorResult("没有匹配的指纹规则（id 不存在或已删除）", nil)
	}
	return jsonToolResult(ginH{"deleted": deleted})
}
