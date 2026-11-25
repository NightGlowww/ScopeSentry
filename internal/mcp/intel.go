package mcp

import (
	"context"

	"github.com/Autumn-27/ScopeSentry/internal/services/intel"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const intelForAssetsDesc = "漏洞情报（CVE Enrichment）：按项目/主机/技术名产出「这条 CVE 在不在我们资产上成立」的情报。" +
	"注意：本工具产出的是情报，不是漏洞——拿不到版本时 applicability 一律为 unknown，绝不降级成 likely。" +
	"返回归一化结构（不透传 NVD 原始 JSON），含 stats.with_version / without_version，用于判断情报可用性。" +
	"参数：project（项目 ObjectID 或名称，可选）；hosts（主机列表，可选）；techs（技术名列表，可选）；" +
	"since_days（只看这段时间内发布的 CVE，默认 90）；limit（条数上限，默认 20，上限 100）。"

type intelForAssetsInput struct {
	Project   string   `json:"project,omitempty" jsonschema:"ScopeSentry 项目 ObjectID 或名称；给了就只查该项目资产"`
	Hosts     []string `json:"hosts,omitempty" jsonschema:"主机列表（域名或 IP）；与 project 二选一，也可都不给（全库）"`
	Techs     []string `json:"techs,omitempty" jsonschema:"限定技术名，如 Tengine / istio-envoy / Okta OIDC"`
	SinceDays int      `json:"since_days,omitempty" jsonschema:"只看这段时间内新发布的 CVE，默认 90 天"`
	Limit     int      `json:"limit,omitempty" jsonschema:"返回条数上限，默认 20，最多 100"`
}

func intelForAssets(ctx context.Context, _ *mcp.CallToolRequest, input intelForAssetsInput) (*mcp.CallToolResult, any, error) {
	report, err := intel.BuildReport(ctx, intel.Options{
		Project:   input.Project,
		Hosts:     input.Hosts,
		Techs:     input.Techs,
		SinceDays: input.SinceDays,
		Limit:     input.Limit,
	})
	if err != nil {
		return errorResult(err.Error(), nil)
	}
	return jsonToolResult(report)
}
