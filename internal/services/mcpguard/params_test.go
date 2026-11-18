package mcpguard

import (
	"errors"
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// 分页归一：默认值、负数、超上限报错（而不是静默截断）
func TestNormalizePaging(t *testing.T) {
	cases := []struct {
		name              string
		pageIndex         int
		pageSize          int
		defSize           int
		maxSize           int
		wantIndex, wantSz int
		wantErr           error
	}{
		{"全部缺省", 0, 0, DefaultPageSize, MaxPageSizeICP, 1, 20, nil},
		{"透传合法值", 3, 50, DefaultPageSize, MaxPageSizeICP, 3, 50, nil},
		{"pageIndex 负数归 1", -5, 10, DefaultPageSize, MaxPageSizeICP, 1, 10, nil},
		{"pageSize 负数回落默认", 2, -1, DefaultPageSize, MaxPageSizeICP, 2, 20, nil},
		{"defSize 非法回落常量", 0, 0, 0, MaxPageSizeICP, 1, DefaultPageSize, nil},
		{"等于上限放行", 1, 200, DefaultPageSize, MaxPageSizeICP, 1, 200, nil},
		{"超上限报错(ICP 200)", 1, 201, DefaultPageSize, MaxPageSizeICP, 0, 0, ErrPageSizeTooLarge},
		{"超上限报错(地契 500)", 1, 501, DefaultPageSize, MaxPageSizeAssetScope, 0, 0, ErrPageSizeTooLarge},
		{"maxSize=0 不设上限", 1, 5000, DefaultPageSize, 0, 1, 5000, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			idx, size, err := NormalizePaging(tc.pageIndex, tc.pageSize, tc.defSize, tc.maxSize)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("expected %v, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if idx != tc.wantIndex || size != tc.wantSz {
				t.Fatalf("got (%d, %d), want (%d, %d)", idx, size, tc.wantIndex, tc.wantSz)
			}
		})
	}
}

// ICP 状态闭集：nil 不过滤，0-3 透传，其余报错（避免「成功 + 空列表」）
func TestNormalizeICPTaskState(t *testing.T) {
	if got, err := NormalizeICPTaskState(nil); got != nil || err != nil {
		t.Fatalf("nil state should stay nil, got %v / %v", got, err)
	}
	for _, v := range []int{
		models.IcpTaskStateWaiting, models.IcpTaskStateRunning,
		models.IcpTaskStatePaused, models.IcpTaskStateDone,
	} {
		got, err := NormalizeICPTaskState(&v)
		if err != nil {
			t.Fatalf("state %d should pass: %v", v, err)
		}
		if got == nil || *got != v {
			t.Fatalf("state %d round-trip failed: %v", v, got)
		}
	}
	for _, bad := range []int{-1, 4, 9} {
		v := bad
		if _, err := NormalizeICPTaskState(&v); !errors.Is(err, ErrInvalidState) {
			t.Fatalf("state %d should be rejected, got %v", bad, err)
		}
	}
	// 返回值必须是副本：外部改入参不能影响已归一的结果
	src := models.IcpTaskStateDone
	got, _ := NormalizeICPTaskState(&src)
	src = models.IcpTaskStateWaiting
	if *got != models.IcpTaskStateDone {
		t.Fatal("NormalizeICPTaskState must return a copy, not alias the input")
	}
}

// ICP 类型 / provider 闭集（icp_task.type 只存 company/domain）
func TestNormalizeICPTaskType(t *testing.T) {
	ok := map[string]string{
		"":         "",
		"company":  models.IcpTaskTypeCompany,
		"Company":  models.IcpTaskTypeCompany,
		" DOMAIN ": models.IcpTaskTypeDomain,
		"domain":   models.IcpTaskTypeDomain,
	}
	for in, want := range ok {
		got, err := NormalizeICPTaskType(in)
		if err != nil || got != want {
			t.Fatalf("NormalizeICPTaskType(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := NormalizeICPTaskType("enterprise"); !errors.Is(err, ErrInvalidEnum) {
		t.Fatalf("unknown type should be rejected, got %v", err)
	}
}

func TestNormalizeICPProvider(t *testing.T) {
	ok := map[string]string{
		"":       "",
		"manual": models.IcpProviderManual,
		"MANUAL": models.IcpProviderManual,
		"none ":  models.IcpProviderNone,
		"http":   models.IcpProviderHTTP,
	}
	for in, want := range ok {
		got, err := NormalizeICPProvider(in)
		if err != nil || got != want {
			t.Fatalf("NormalizeICPProvider(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := NormalizeICPProvider("tyc"); !errors.Is(err, ErrInvalidEnum) {
		t.Fatalf("unknown provider should be rejected, got %v", err)
	}
}

// 审计：method 大写（仓库层等值匹配），未知方法报错
func TestNormalizeAuditMethod(t *testing.T) {
	ok := map[string]string{
		"":        "",
		"get":     "GET",
		" Post ":  "POST",
		"DELETE":  "DELETE",
		"patch":   "PATCH",
		"options": "OPTIONS",
	}
	for in, want := range ok {
		got, err := NormalizeAuditMethod(in)
		if err != nil || got != want {
			t.Fatalf("NormalizeAuditMethod(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := NormalizeAuditMethod("FETCH"); !errors.Is(err, ErrInvalidEnum) {
		t.Fatalf("unknown method should be rejected, got %v", err)
	}
}

// 审计：0 = 不过滤，其余必须是 100-599
func TestNormalizeAuditStatusMin(t *testing.T) {
	for _, v := range []int{0, 100, 200, 404, 500, 599} {
		got, err := NormalizeAuditStatusMin(v)
		if err != nil || got != v {
			t.Fatalf("statusMin %d should pass, got %d / %v", v, got, err)
		}
	}
	for _, v := range []int{-1, -200, 1, 99, 600, 9999} {
		if _, err := NormalizeAuditStatusMin(v); !errors.Is(err, ErrInvalidStatusMin) {
			t.Fatalf("statusMin %d should be rejected, got %v", v, err)
		}
	}
}

// 地契 groupId：未分组标记放行，其余必须 24 位 ObjectID
func TestNormalizeScopeGroupID(t *testing.T) {
	ok := map[string]string{
		"":                           "",
		"0":                          "0",
		"unassigned":                 "unassigned",
		"507f1f77bcf86cd799439011":   "507f1f77bcf86cd799439011",
		" 507f1f77bcf86cd799439011 ": "507f1f77bcf86cd799439011",
	}
	for in, want := range ok {
		got, err := NormalizeScopeGroupID(in)
		if err != nil || got != want {
			t.Fatalf("NormalizeScopeGroupID(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	// 大小写变体 ("Unassigned") 既不是 service 认识的标记，也不是 ObjectID → 拒绝
	for _, bad := range []string{"Unassigned", "507f1f77bcf86cd79943901", "507f1f77bcf86cd7994390111", "not-an-id", "集团A"} {
		if _, err := NormalizeScopeGroupID(bad); !errors.Is(err, ErrInvalidGroupID) {
			t.Fatalf("groupId %q should be rejected, got %v", bad, err)
		}
	}
}

// 新增只读工具必须登记 scope，且复用 asset:read（生产受限 key 无需改动即可用）
func TestReadOnlyV2ToolsScopes(t *testing.T) {
	want := map[string]string{
		"list_asset_groups": ScopeAssetRead,
		"list_asset_scopes": ScopeAssetRead,
		"list_icp_tasks":    ScopeAssetRead,
		"list_audit_logs":   ScopeAssetRead,
	}
	restricted := []string{ScopeAssetRead, ScopeTaskRead, ScopePluginRead, ScopeNodeRead}
	for tool, scope := range want {
		got, registered := RequiredScope(tool)
		if !registered {
			t.Fatalf("%s is not registered in ToolScopes", tool)
		}
		if got != scope {
			t.Fatalf("%s scope = %q, want %q", tool, got, scope)
		}
		if !ScopeAllowed(restricted, got) {
			t.Fatalf("%s should be reachable for the restricted production key", tool)
		}
	}
	if !ScopeAllowed([]string{ScopeAll}, ScopeAssetRead) {
		t.Fatal("full-scope key must be allowed")
	}
}
