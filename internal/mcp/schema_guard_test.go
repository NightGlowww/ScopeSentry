//go:build integration

package mcp

import "testing"

// 防回归：tools.go 里任何 struct tag 以 "WORD=" 开头都会被 go-sdk 判为非法，
// AddTool 直接 panic —— 表现为 /mcp 全站 500（v2.0.125 的真实事故）。
// 这里在注册阶段把所有工具建一遍，标签非法就会在这里炸掉而不是上线后炸。
func TestRegisterToolsSchemaTagsValid(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("registerTools panicked (invalid jsonschema tag?): %v", r)
		}
	}()
	srv := newServer(nil)
	if srv == nil {
		t.Fatal("newServer returned nil")
	}
}
