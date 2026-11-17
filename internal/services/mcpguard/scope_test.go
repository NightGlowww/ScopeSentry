package mcpguard

import (
	"errors"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// scope 匹配：精确 / 家族通配 / 全通配 / 资源名简写
func TestScopeMatches(t *testing.T) {
	cases := []struct {
		name     string
		granted  string
		required string
		want     bool
	}{
		{"全通配", "*", "asset:read", true},
		{"全通配对任意字符串", "*", "whatever:goes", true},
		{"精确命中", "asset:read", "asset:read", true},
		{"精确不命中", "asset:read", "asset:write", false},
		{"家族通配 read", "asset:*", "asset:read", true},
		{"家族通配 write", "asset:*", "asset:write", true},
		{"家族通配不跨资源", "asset:*", "task:read", false},
		{"资源名简写", "asset", "asset:read", true},
		{"资源名简写不跨资源", "asset", "task:read", false},
		{"空授予项不匹配", "", "asset:read", false},
		{"需求为空且非通配", "asset:read", "", false},
		{"全通配对空需求", "*", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ScopeMatches(tc.granted, tc.required); got != tc.want {
				t.Fatalf("ScopeMatches(%q, %q) = %v, want %v", tc.granted, tc.required, got, tc.want)
			}
		})
	}
}

// 空 scope 列表 = 治理字段上线前的历史 key，按向后兼容放行
func TestScopeAllowed(t *testing.T) {
	if !ScopeAllowed(nil, "asset:read") {
		t.Fatal("empty scope list should be treated as legacy full access")
	}
	if !ScopeAllowed([]string{"asset:read", "task:write"}, "task:write") {
		t.Fatal("expected task:write granted")
	}
	if ScopeAllowed([]string{"asset:read"}, "task:write") {
		t.Fatal("task:write should be denied")
	}
	if !ScopeAllowed([]string{"asset:*"}, "asset:write") {
		t.Fatal("asset:* should cover asset:write")
	}
}

// 工具白名单：空=全部、精确、星号、前缀通配
func TestToolAllowed(t *testing.T) {
	cases := []struct {
		name    string
		allowed []string
		tool    string
		want    bool
	}{
		{"空列表=全部", nil, "list_assets", true},
		{"精确命中", []string{"list_assets"}, "list_assets", true},
		{"精确未命中", []string{"list_assets"}, "create_project", false},
		{"星号=全部", []string{"*"}, "anything", true},
		{"前缀通配", []string{"list_*"}, "list_plugins", true},
		{"前缀通配未命中", []string{"list_*"}, "create_project", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ToolAllowed(tc.allowed, tc.tool); got != tc.want {
				t.Fatalf("ToolAllowed(%v, %q) = %v, want %v", tc.allowed, tc.tool, got, tc.want)
			}
		})
	}
}

func baseKey() *models.ApiKey {
	return &models.ApiKey{
		Name:    "k",
		Enabled: true,
		Scope:   models.StringList{ScopeAssetRead},
		Tools:   models.StringList{"list_assets"},
	}
}

// Check：过期 / 禁用 / 全局开关 / 工具白名单 / scope / 未登记工具
func TestCheck(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	enabledCfg := &models.McpConfig{Enabled: true}
	disabledCfg := &models.McpConfig{Enabled: false}

	t.Run("未过期放行", func(t *testing.T) {
		key := baseKey()
		key.ExpiresAt = &future
		if err := Check(key, enabledCfg, "list_assets", now); err != nil {
			t.Fatalf("expected pass, got %v", err)
		}
	})

	t.Run("已过期拒绝", func(t *testing.T) {
		key := baseKey()
		key.ExpiresAt = &past
		err := Check(key, enabledCfg, "list_assets", now)
		if !errors.Is(err, ErrKeyExpired) {
			t.Fatalf("expected ErrKeyExpired, got %v", err)
		}
	})

	t.Run("无过期时间放行", func(t *testing.T) {
		key := baseKey()
		key.ExpiresAt = nil
		if err := Check(key, enabledCfg, "list_assets", now); err != nil {
			t.Fatalf("expected pass, got %v", err)
		}
	})

	t.Run("禁用拒绝", func(t *testing.T) {
		key := baseKey()
		key.Enabled = false
		if err := Check(key, enabledCfg, "list_assets", now); !errors.Is(err, ErrKeyDisabled) {
			t.Fatalf("expected ErrKeyDisabled, got %v", err)
		}
	})

	t.Run("全局关闭拒绝", func(t *testing.T) {
		if err := Check(baseKey(), disabledCfg, "list_assets", now); !errors.Is(err, ErrMCPDisabled) {
			t.Fatalf("expected ErrMCPDisabled, got %v", err)
		}
	})

	t.Run("工具不在 key 白名单", func(t *testing.T) {
		if err := Check(baseKey(), enabledCfg, "create_project", now); !errors.Is(err, ErrToolNotAllowed) {
			t.Fatalf("expected ErrToolNotAllowed, got %v", err)
		}
	})

	t.Run("工具被全局白名单挡住", func(t *testing.T) {
		cfg := &models.McpConfig{Enabled: true, AllowedTools: models.StringList{"count_assets"}}
		if err := Check(baseKey(), cfg, "list_assets", now); !errors.Is(err, ErrToolNotAllowed) {
			t.Fatalf("expected ErrToolNotAllowed, got %v", err)
		}
	})

	t.Run("scope 不足", func(t *testing.T) {
		key := baseKey()
		key.Tools = nil // 放开工具白名单，单看 scope
		if err := Check(key, enabledCfg, "create_project", now); !errors.Is(err, ErrScopeDenied) {
			t.Fatalf("expected ErrScopeDenied, got %v", err)
		}
	})

	t.Run("未登记工具默认拒绝", func(t *testing.T) {
		key := baseKey()
		key.Tools = nil
		if err := Check(key, enabledCfg, "not_registered_tool", now); !errors.Is(err, ErrToolNotAllowed) {
			t.Fatalf("expected ErrToolNotAllowed, got %v", err)
		}
	})

	t.Run("未登记工具持星号放行", func(t *testing.T) {
		key := &models.ApiKey{Enabled: true, Scope: models.StringList{ScopeAll}}
		if err := Check(key, enabledCfg, "not_registered_tool", now); err != nil {
			t.Fatalf("expected pass, got %v", err)
		}
	})

	t.Run("握手方法无工具名放行", func(t *testing.T) {
		key := baseKey()
		key.Scope = models.StringList{} // 即便 scope 为空也放行 initialize
		if err := Check(key, enabledCfg, "", now); err != nil {
			t.Fatalf("expected pass, got %v", err)
		}
	})
}

// PermittedTools：全权限返回 nil（不裁剪），受限 key 只返回允许的工具
func TestPermittedTools(t *testing.T) {
	full := &models.ApiKey{Enabled: true, Scope: models.StringList{ScopeAll}}
	if got := PermittedTools(full, &models.McpConfig{Enabled: true}); got != nil {
		t.Fatalf("full-scope key should return nil (no filter), got %v", got)
	}

	restricted := baseKey()
	permitted := PermittedTools(restricted, &models.McpConfig{Enabled: true})
	if permitted == nil {
		t.Fatal("restricted key should return an explicit set")
	}
	if !permitted["list_assets"] {
		t.Fatal("list_assets should be permitted")
	}
	if permitted["create_project"] {
		t.Fatal("create_project should not be permitted")
	}
}

// ToolScopes 表基本健康度：非空、scope 非空、无重复登记
func TestToolScopesRegistry(t *testing.T) {
	if len(ToolScopes) == 0 {
		t.Fatal("ToolScopes must not be empty")
	}
	seen := make(map[string]struct{}, len(ToolScopes))
	for name, scope := range ToolScopes {
		if name == "" {
			t.Fatal("empty tool name in registry")
		}
		if scope == "" {
			t.Fatalf("tool %q has empty scope", name)
		}
		if _, dup := seen[name]; dup {
			t.Fatalf("duplicate tool %q", name)
		}
		seen[name] = struct{}{}
	}
	for _, name := range ToolNames() {
		if _, ok := ToolScopes[name]; !ok {
			t.Fatalf("ToolNames returned unregistered tool %q", name)
		}
	}
}
