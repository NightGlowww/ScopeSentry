package mcp

// 噪声仓库黑名单（GitHub 泄露结果的 ignored_repos）。
//
// 两个问题叠在一起：这块以前没有 MCP 入口；后端还「只存不用」（加了忽略也不生效）。
// 现在采集链路（任务与威胁源）都会查它，这里补上自动化能用的三个动作。

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func listIgnoredRepos(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	if d.githubService == nil {
		return errorResult("GitHub 服务未初始化", nil)
	}
	res, err := d.githubService.IgnoredRepos(ctx, "list", "", "", false)
	if err != nil {
		return errorResult("获取忽略仓库列表失败", err)
	}
	return jsonToolResult(res)
}

type addIgnoredRepoInput struct {
	Repo   string `json:"repo" jsonschema:"仓库全名 owner/repo；也支持 owner/* 忽略该 owner 下全部仓库"`
	Reason string `json:"reason,omitempty" jsonschema:"忽略原因，便于以后回顾"`
	Purge  bool   `json:"purge,omitempty" jsonschema:"是否同时删除已入库的同仓库结果（不可恢复，默认 false）"`
}

func addIgnoredRepo(ctx context.Context, _ *mcp.CallToolRequest, input addIgnoredRepoInput) (*mcp.CallToolResult, any, error) {
	if d.githubService == nil {
		return errorResult("GitHub 服务未初始化", nil)
	}
	repo := strings.TrimSpace(input.Repo)
	if repo == "" {
		return errorResult("repo 不能为空", nil)
	}
	res, err := d.githubService.IgnoredRepos(ctx, "add", repo, strings.TrimSpace(input.Reason), input.Purge)
	if err != nil {
		return errorResult("添加忽略仓库失败", err)
	}
	return jsonToolResult(res)
}

type removeIgnoredRepoInput struct {
	Repo string `json:"repo" jsonschema:"要移除的仓库全名"`
}

func removeIgnoredRepo(ctx context.Context, _ *mcp.CallToolRequest, input removeIgnoredRepoInput) (*mcp.CallToolResult, any, error) {
	if d.githubService == nil {
		return errorResult("GitHub 服务未初始化", nil)
	}
	repo := strings.TrimSpace(input.Repo)
	if repo == "" {
		return errorResult("repo 不能为空", nil)
	}
	res, err := d.githubService.IgnoredRepos(ctx, "remove", repo, "", false)
	if err != nil {
		return errorResult("移除忽略仓库失败", err)
	}
	// removed=false 说明名单里本来就没有它：回「成功」会让调用方以为改对了
	if removed, ok := res["removed"].(bool); ok && !removed {
		return errorResult("黑名单里没有这个仓库："+repo, nil)
	}
	return jsonToolResult(res)
}
