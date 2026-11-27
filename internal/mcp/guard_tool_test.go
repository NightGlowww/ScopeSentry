//go:build integration

package mcp

import (
	"context"
	"testing"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// guardTool 是「一个工具 panic 不能打死进程」的唯一实现点。
// MCP 工具由 jsonrpc2 在自己的 goroutine 里调用，gin.Recovery 管不到，
// 所以这里必须自己兜住（2026-11-28：get_asset_detail 传未知 id 让生产容器重启 5 次）。
func TestGuardToolRecoversPanic(t *testing.T) {
	type in struct{}
	type out struct{}
	panicky := func(ctx context.Context, req *gomcp.CallToolRequest, i in) (*gomcp.CallToolResult, out, error) {
		var m map[string]string
		_ = m["boom"] // nil map read is fine; force a real nil deref instead
		var p *int
		_ = *p
		return nil, out{}, nil
	}
	guarded := guardTool("boom_tool", panicky)
	res, _, err := guarded(context.Background(), nil, in{})
	if err != nil {
		t.Fatalf("guardTool 应该吞掉 panic 并以工具错误返回，err=%v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("guardTool 应该返回 IsError=true 的工具结果，got %+v", res)
	}
	if len(res.Content) == 0 {
		t.Fatal("工具错误结果必须有文本内容")
	}

	// 正常路径不能被改写
	ok := func(ctx context.Context, req *gomcp.CallToolRequest, i in) (*gomcp.CallToolResult, out, error) {
		return &gomcp.CallToolResult{}, out{}, nil
	}
	res2, _, err2 := guardTool("ok_tool", ok)(context.Background(), nil, in{})
	if err2 != nil || res2 == nil || res2.IsError {
		t.Fatalf("正常工具被 guardTool 改坏了: res=%+v err=%v", res2, err2)
	}
}