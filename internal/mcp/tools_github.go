package mcp

// GitHub 情报的处置面：研判状态 + 周期任务生命周期。
//
// 只读的 list_github_results 能让 Agent 回答「有哪些泄露」，但答不了
// 「这条我确认了」「这个任务在乱跑，停掉」—— 处置链路在 MCP 侧断在第一步。

import (
	"context"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type triageGithubResultsInput struct {
	IDs      []string `json:"ids" jsonschema:"结果 id 列表（list_github_results 返回的 id），至少一个"`
	Status   string   `json:"status" jsonschema:"研判状态：new（重置）/ triaged（已确认）/ ignored（已忽略）"`
	Operator string   `json:"operator,omitempty" jsonschema:"研判人标识，默认 mcp；用于事后追责"`
}

// triageStatusAlias 允许自然语言式写法，但仍只落到三个受控值。
// 不直接透传字符串：库里 status 是筛选条件，写进一个拼错的值等于这条结果永远筛不到。
var triageStatusAlias = map[string]string{
	"new":     models.GithubResultStatusNew,
	"triaged": models.GithubResultStatusTriaged,
	"ignored": models.GithubResultStatusIgnored,
	"confirm": models.GithubResultStatusTriaged,
	"ignore":  models.GithubResultStatusIgnored,
}

func triageGithubResults(ctx context.Context, _ *mcp.CallToolRequest, input triageGithubResultsInput) (*mcp.CallToolResult, any, error) {
	if d.githubResultRepo == nil {
		return errorResult("GitHub 结果仓库未初始化", nil)
	}
	if len(input.IDs) == 0 {
		return errorResult("ids 不能为空", nil)
	}
	status, ok := triageStatusAlias[strings.ToLower(strings.TrimSpace(input.Status))]
	if !ok {
		return errorResult("status 只能是 new / triaged / ignored", nil)
	}
	operator := strings.TrimSpace(input.Operator)
	if operator == "" {
		operator = "mcp"
	}
	matched, modified, err := d.githubResultRepo.Triage(ctx, input.IDs, status, operator)
	if err != nil {
		return errorResult("研判失败", err)
	}
	// matched=0 → 一条都不存在。回 200 + updated:0 会让调用方以为「标好了」。
	if matched == 0 {
		return errorResult("没有匹配的结果（id 不存在或已删除）", nil)
	}
	return jsonToolResult(ginH{"matched": matched, "updated": modified, "status": status, "operator": operator})
}

type githubTaskIDsInput struct {
	IDs []string `json:"ids" jsonschema:"GitHub 任务 id 列表，至少一个"`
}

func stopGithubTasks(ctx context.Context, _ *mcp.CallToolRequest, input githubTaskIDsInput) (*mcp.CallToolResult, any, error) {
	if d.githubService == nil {
		return errorResult("GitHub 服务未初始化", nil)
	}
	if len(input.IDs) == 0 {
		return errorResult("ids 不能为空", nil)
	}
	matched, modified, err := d.githubService.StopTasks(ctx, input.IDs)
	if err != nil {
		return errorResult("暂停任务失败", err)
	}
	if matched == 0 {
		return errorResult("没有匹配的任务（id 不存在或已删除）", nil)
	}
	// modified=0 但 matched>0 是幂等成功：本来就是暂停态。
	return jsonToolResult(ginH{"matched": matched, "updated": modified, "state": models.GithubTaskStatePaused})
}

func recoverGithubTasks(ctx context.Context, _ *mcp.CallToolRequest, input githubTaskIDsInput) (*mcp.CallToolResult, any, error) {
	if d.githubService == nil {
		return errorResult("GitHub 服务未初始化", nil)
	}
	if len(input.IDs) == 0 {
		return errorResult("ids 不能为空", nil)
	}
	recovered, err := d.githubService.RecoverTasks(ctx, input.IDs)
	if err != nil {
		return errorResult("恢复任务失败", err)
	}
	return jsonToolResult(ginH{"recovered": recovered, "state": models.GithubTaskStateWaiting})
}

func deleteGithubTasks(ctx context.Context, _ *mcp.CallToolRequest, input githubTaskIDsInput) (*mcp.CallToolResult, any, error) {
	if d.githubService == nil {
		return errorResult("GitHub 服务未初始化", nil)
	}
	if len(input.IDs) == 0 {
		return errorResult("ids 不能为空", nil)
	}
	deleted, removed, err := d.githubService.DeleteTasks(ctx, input.IDs)
	if err != nil {
		return errorResult("删除任务失败", err)
	}
	if deleted == 0 {
		return errorResult("没有匹配的任务（id 不存在或已删除）", nil)
	}
	// 结果行跟着任务一起删（REST 同语义）；调用方要能从回执里看出删了多少条结果。
	return jsonToolResult(ginH{"deletedTasks": deleted, "removedResults": removed})
}
