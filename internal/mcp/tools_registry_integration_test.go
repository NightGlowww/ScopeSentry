//go:build integration

package mcp

import (
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/services/mcpguard"
)

// 漂移防护：mcpguard.ToolScopes 是 scope 判定的唯一真相源，
// 新增 MCP 工具却忘了登记 scope，会在受限 key 上被默认拒绝（很难排查）。
//
// 为什么打 integration tag：internal/mcp 经 services/assets 间接 import
// internal/bootstrap，其 init() 会连库并 log.Fatalf，没有真实 Mongo 就跑不起来；
// 这个断言必须在有数据库的集成环境里执行。
func TestEveryRegisteredToolHasScope(t *testing.T) {
	_ = newServer(nil)
	names := ToolNames()
	if len(names) == 0 {
		t.Fatal("no tools registered")
	}
	for _, name := range names {
		if _, ok := mcpguard.RequiredScope(name); !ok {
			t.Fatalf("tool %q is registered but missing from mcpguard.ToolScopes", name)
		}
	}
}

// 反向检查：治理表里登记的工具必须真的存在，否则白名单会写成空话
func TestNoOrphanScopeEntries(t *testing.T) {
	_ = newServer(nil)
	registered := make(map[string]bool, len(ToolNames()))
	for _, name := range ToolNames() {
		registered[name] = true
	}
	for _, name := range mcpguard.ToolNames() {
		if !registered[name] {
			t.Fatalf("mcpguard.ToolScopes has %q but no such MCP tool is registered", name)
		}
	}
}

// 工具裁剪不会破坏注册表
func TestNewServerFiltersTools(t *testing.T) {
	_ = newServer(map[string]bool{"list_assets": true})
	if len(ToolNames()) == 0 {
		t.Fatal("tool names should still be recorded when filtering")
	}
}
