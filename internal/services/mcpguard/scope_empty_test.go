package mcpguard

import "testing"

// 历史 key（没有 scope 字段）只能读，不能借新工具拿到写权限。
func TestEmptyScopeIsReadOnly(t *testing.T) {
	if !ScopeAllowed(nil, "asset:read") {
		t.Fatal("empty scope should still allow read")
	}
	if ScopeAllowed(nil, "asset:write") {
		t.Fatal("empty scope must NOT allow write")
	}
	if ScopeAllowed([]string{}, "icp:write") {
		t.Fatal("empty slice must NOT allow write")
	}
	// 显式授予仍然生效
	if !ScopeAllowed([]string{"asset:write"}, "asset:write") {
		t.Fatal("explicit write scope should be allowed")
	}
	if !ScopeAllowed([]string{"*"}, "asset:write") {
		t.Fatal("* should allow everything")
	}
	if !ScopeAllowed([]string{"asset:*"}, "asset:write") {
		t.Fatal("asset:* should cover asset:write")
	}
}
