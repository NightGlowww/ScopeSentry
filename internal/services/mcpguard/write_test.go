package mcpguard

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// objectIDForIndex 造一个确定性的合法 ObjectID（24 位 hex），用于批量上限测试。
func objectIDForIndex(i int) string {
	const hexDigits = "0123456789abcdef"
	buf := []byte("000000000000000000000000")
	// 末尾 6 位承载下标，前面全 0；下标落在 hex 闭集内时必然合法
	for pos := 0; pos < 6; pos++ {
		buf[len(buf)-1-pos] = hexDigits[(i>>(4*pos))&0xF]
	}
	return string(buf)
}

// 写侧第一批（v2.0.88）的 scope 登记：5 个工具、2 个 scope 家族。
//
// 这里守两件事：
//  1. 工具登记齐全且 scope 正确（漏登记 = 受限 key 默认拒绝；集成测试需要真库才跑）；
//  2. 写 scope 不被只读 key 覆盖 —— 这是本批改动的安全底线：补工具不能顺手把
//     「只读 key」升级成「可删除 key」（MCP_GAPS §9 记录过现网只读 key 调 start_task
//     被 scope 拦下的案例，那次拦截是对的）。
func TestWriteToolsScopes(t *testing.T) {
	want := map[string]string{
		"create_asset_group":  ScopeAssetWrite,
		"delete_asset_groups": ScopeAssetWrite,
		"create_asset_scope":  ScopeAssetWrite,
		"create_icp_task":     ScopeICPWrite,
		"stop_icp_tasks":      ScopeICPWrite,
	}
	if ScopeAssetWrite != "asset:write" {
		t.Fatalf("ScopeAssetWrite = %q, want asset:write", ScopeAssetWrite)
	}
	if ScopeICPWrite != "icp:write" {
		t.Fatalf("ScopeICPWrite = %q, want icp:write", ScopeICPWrite)
	}

	// 生产受限 key 的授予集（只读）
	readOnly := []string{ScopeAssetRead, ScopeTaskRead, ScopePluginRead, ScopeNodeRead}
	for tool, scope := range want {
		got, registered := RequiredScope(tool)
		if !registered {
			t.Fatalf("%s is not registered in ToolScopes", tool)
		}
		if got != scope {
			t.Fatalf("%s scope = %q, want %q", tool, got, scope)
		}
		if ScopeAllowed(readOnly, got) {
			t.Fatalf("%s must NOT be reachable for a read-only key (scope %q)", tool, got)
		}
		if !ScopeAllowed([]string{scope}, got) {
			t.Fatalf("%s must be reachable for a key holding %q", tool, scope)
		}
		if !ScopeAllowed([]string{ScopeAll}, got) {
			t.Fatalf("%s must be reachable for a full-scope key", tool)
		}
	}

	// 家族通配：asset:* / icp:* 只覆盖各自家族，不跨界
	if !ScopeMatches("asset:*", ScopeAssetWrite) {
		t.Fatal("asset:* should cover asset:write")
	}
	if !ScopeMatches("icp:*", ScopeICPWrite) {
		t.Fatal("icp:* should cover icp:write")
	}
	if ScopeMatches("asset:*", ScopeICPWrite) {
		t.Fatal("asset:* must not cover icp:write")
	}
	if ScopeMatches("icp:*", ScopeAssetWrite) {
		t.Fatal("icp:* must not cover asset:write")
	}
	if !ScopeMatches("icp", ScopeICPWrite) {
		t.Fatal("bare resource name icp should cover icp:write")
	}
}

// Check() 端到端：只读 key 被拒（ErrScopeDenied），写 key 放行。
func TestCheckWriteToolsScopeDenied(t *testing.T) {
	now := time.Now()
	cfg := &models.McpConfig{Enabled: true}

	readOnlyKey := &models.ApiKey{
		Name:    "readonly",
		Enabled: true,
		Scope:   models.StringList{ScopeAssetRead, ScopeTaskRead},
	}
	for _, tool := range []string{
		"create_asset_group", "delete_asset_groups", "create_asset_scope",
		"create_icp_task", "stop_icp_tasks",
	} {
		if err := Check(readOnlyKey, cfg, tool, now); !errors.Is(err, ErrScopeDenied) {
			t.Fatalf("%s should be denied for read-only key, got %v", tool, err)
		}
	}

	writeKey := &models.ApiKey{
		Name:    "writer",
		Enabled: true,
		Scope:   models.StringList{ScopeAssetWrite, ScopeICPWrite},
	}
	for _, tool := range []string{
		"create_asset_group", "delete_asset_groups", "create_asset_scope",
		"create_icp_task", "stop_icp_tasks",
	} {
		if err := Check(writeKey, cfg, tool, now); err != nil {
			t.Fatalf("%s should be allowed for write key, got %v", tool, err)
		}
	}
}

// confirm 闸：删除类工具没显式确认必须报错（而不是默认执行）。
func TestRequireConfirm(t *testing.T) {
	if err := RequireConfirm(true, "delete_asset_groups"); err != nil {
		t.Fatalf("confirm=true should pass, got %v", err)
	}
	err := RequireConfirm(false, "delete_asset_groups")
	if !errors.Is(err, ErrConfirmRequired) {
		t.Fatalf("confirm=false should be rejected, got %v", err)
	}
	// 错误信息要能被 Agent 直接照做：带上工具名与正确写法
	if !strings.Contains(err.Error(), "delete_asset_groups") {
		t.Fatalf("error should name the tool, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "confirm") {
		t.Fatalf("error should mention confirm, got %q", err.Error())
	}
}

// ids 归一：去重/去空/小写、缺参、超限、非法 ObjectID 全部报错而不是静默处理。
func TestNormalizeDeleteAssetGroupIDs(t *testing.T) {
	upper := "507F1F77BCF86CD799439011"
	ids, err := NormalizeDeleteAssetGroupIDs([]string{
		" 507f1f77bcf86cd799439011 ", upper, "", "   ", "507f1f77bcf86cd799439012",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("dedupe/trim failed: %v", ids)
	}
	if ids[0] != "507f1f77bcf86cd799439011" || ids[1] != "507f1f77bcf86cd799439012" {
		t.Fatalf("ids not normalized to lowercase in order: %v", ids)
	}

	if _, err := NormalizeDeleteAssetGroupIDs(nil); !errors.Is(err, ErrIDsRequired) {
		t.Fatalf("empty ids should be rejected, got %v", err)
	}
	if _, err := NormalizeDeleteAssetGroupIDs([]string{"", "  "}); !errors.Is(err, ErrIDsRequired) {
		t.Fatalf("blank-only ids should be rejected, got %v", err)
	}
	if _, err := NormalizeDeleteAssetGroupIDs([]string{"not-an-id"}); !errors.Is(err, ErrInvalidObjectID) {
		t.Fatalf("invalid ObjectID should be rejected, got %v", err)
	}

	// 上限 20：恰好 20 放行，21 报错（先去重再判，重复项不占额度）
	many := make([]string, 0, MaxDeleteAssetGroupIDs+1)
	for i := 0; i <= MaxDeleteAssetGroupIDs; i++ {
		many = append(many, objectIDForIndex(i+1))
	}
	if _, err := NormalizeDeleteAssetGroupIDs(many[:MaxDeleteAssetGroupIDs]); err != nil {
		t.Fatalf("exactly %d ids should pass, got %v", MaxDeleteAssetGroupIDs, err)
	}
	if _, err := NormalizeDeleteAssetGroupIDs(many); !errors.Is(err, ErrTooManyIDs) {
		t.Fatalf("%d ids should be rejected, got %v", len(many), err)
	}
	dup := append(append([]string{}, many[:MaxDeleteAssetGroupIDs]...), many[0])
	if _, err := NormalizeDeleteAssetGroupIDs(dup); err != nil {
		t.Fatalf("duplicate should not count toward the cap, got %v", err)
	}
}

// stop_icp_tasks 上限 50，与删除分离（停止是改状态，量级可以更大）
func TestNormalizeStopICPIDs(t *testing.T) {
	if MaxStopICPIDs <= MaxDeleteAssetGroupIDs {
		t.Fatalf("MaxStopICPIDs (%d) should be larger than MaxDeleteAssetGroupIDs (%d)",
			MaxStopICPIDs, MaxDeleteAssetGroupIDs)
	}
	if _, err := NormalizeStopICPIDs([]string{objectIDForIndex(1)}); err != nil {
		t.Fatalf("single id should pass, got %v", err)
	}
	many := make([]string, 0, MaxStopICPIDs+1)
	for i := 0; i <= MaxStopICPIDs; i++ {
		many = append(many, objectIDForIndex(i+1))
	}
	if _, err := NormalizeStopICPIDs(many); !errors.Is(err, ErrTooManyIDs) {
		t.Fatalf("%d ids should be rejected, got %v", len(many), err)
	}
}

// 集团删除策略：dissolve/transfer 互斥校验 + targetGroupId 必须在被删集合外
func TestNormalizeAssetGroupAction(t *testing.T) {
	a := objectIDForIndex(1)
	b := objectIDForIndex(2)

	got, target, err := NormalizeAssetGroupAction("", "", []string{a})
	if err != nil || got != AssetGroupActionDissolve || target != "" {
		t.Fatalf("empty action should default to dissolve, got (%q,%q,%v)", got, target, err)
	}
	got, target, err = NormalizeAssetGroupAction(" DISSOLVE ", "", []string{a})
	if err != nil || got != AssetGroupActionDissolve || target != "" {
		t.Fatalf("DISSOLVE should normalize, got (%q,%q,%v)", got, target, err)
	}
	got, target, err = NormalizeAssetGroupAction("transfer", strings.ToUpper(b), []string{a})
	if err != nil || got != AssetGroupActionTransfer || target != b {
		t.Fatalf("transfer should pass and lowercase target, got (%q,%q,%v)", got, target, err)
	}

	// dissolve + targetGroupId：REST 会静默忽略，MCP 报错（避免误以为地契被划转）
	if _, _, err := NormalizeAssetGroupAction("dissolve", b, []string{a}); !errors.Is(err, ErrInvalidAction) {
		t.Fatalf("dissolve with targetGroupId should be rejected, got %v", err)
	}
	if _, _, err := NormalizeAssetGroupAction("transfer", "", []string{a}); !errors.Is(err, ErrTargetGroupRequired) {
		t.Fatalf("transfer without target should be rejected, got %v", err)
	}
	if _, _, err := NormalizeAssetGroupAction("transfer", "group-a", []string{a}); !errors.Is(err, ErrInvalidObjectID) {
		t.Fatalf("invalid targetGroupId should be rejected, got %v", err)
	}
	// transfer 目标 = 本批被删集团（REST 只挡 self-transfer，同批互转挡不住）
	if _, _, err := NormalizeAssetGroupAction("transfer", a, []string{a, b}); !errors.Is(err, ErrInvalidAction) {
		t.Fatalf("target inside ids should be rejected, got %v", err)
	}
	if _, _, err := NormalizeAssetGroupAction("delete-all", "", []string{a}); !errors.Is(err, ErrInvalidAction) {
		t.Fatalf("unknown action should be rejected, got %v", err)
	}
}

// 名称归一：rune 计长（中文按字符不按字节）
func TestNormalizeWriteName(t *testing.T) {
	got, err := NormalizeWriteName("name", "  集团A  ", MaxAssetGroupNameRunes)
	if err != nil || got != "集团A" {
		t.Fatalf("trim failed: %q / %v", got, err)
	}
	if _, err := NormalizeWriteName("name", "   ", MaxAssetGroupNameRunes); !errors.Is(err, ErrEmptyField) {
		t.Fatalf("blank name should be rejected, got %v", err)
	}
	// 64 个中文字符（192 字节）必须放行：按 len() 算会被误判超长
	cn64 := strings.Repeat("集", MaxAssetGroupNameRunes)
	if _, err := NormalizeWriteName("name", cn64, MaxAssetGroupNameRunes); err != nil {
		t.Fatalf("%d 个中文字符应该放行: %v", MaxAssetGroupNameRunes, err)
	}
	if _, err := NormalizeWriteName("name", cn64+"团", MaxAssetGroupNameRunes); !errors.Is(err, ErrFieldTooLong) {
		t.Fatalf("超长名称应被拒绝, got %v", err)
	}
	if MaxAssetGroupNameRunes >= MaxAssetScopeNameRunes {
		t.Fatalf("地契名上限应大于集团名上限")
	}
}

// 地契写侧 groupId：留空=未分组，只读标记必须被拒（写不进去）
func TestNormalizeScopeOwnerGroupID(t *testing.T) {
	if got, err := NormalizeScopeOwnerGroupID("  "); err != nil || got != "" {
		t.Fatalf("blank groupId should mean ungrouped: %q / %v", got, err)
	}
	oid := "507F1F77BCF86CD799439011"
	if got, err := NormalizeScopeOwnerGroupID(oid); err != nil || got != strings.ToLower(oid) {
		t.Fatalf("ObjectID should be lowercased: %q / %v", got, err)
	}
	for _, bad := range []string{"unassigned", "0", "Unassigned", "集团A", "507f1f77bcf86cd79943901"} {
		if _, err := NormalizeScopeOwnerGroupID(bad); !errors.Is(err, ErrInvalidGroupID) {
			t.Fatalf("groupId %q should be rejected on write side, got %v", bad, err)
		}
	}
	// 只读侧仍接受这些标记（写侧收紧不能把只读侧一起改坏）
	if _, err := NormalizeScopeGroupID("unassigned"); err != nil {
		t.Fatalf("read-side normalizer must keep accepting unassigned, got %v", err)
	}
}

// 地契类型与范围文本
func TestNormalizeAssetScopeTypeAndRange(t *testing.T) {
	for _, v := range []string{"", "domain", "IP", " Mixed "} {
		if _, err := NormalizeAssetScopeType(v); err != nil {
			t.Fatalf("scopeType %q should pass: %v", v, err)
		}
	}
	if got, _ := NormalizeAssetScopeType(" MIXED "); got != AssetScopeTypeMixed {
		t.Fatalf("scopeType should lowercase: %q", got)
	}
	if _, err := NormalizeAssetScopeType("host"); !errors.Is(err, ErrInvalidEnum) {
		t.Fatalf("unknown scopeType should be rejected, got %v", err)
	}

	text, err := NormalizeAssetScopeRange("  example.com\nexample.org;10.0.0.1  ")
	if err != nil {
		t.Fatalf("valid range should pass: %v", err)
	}
	if strings.TrimSpace(text) != text {
		t.Fatalf("range should be trimmed: %q", text)
	}
	for _, bad := range []string{"", "   ", ",,;;", "\n\n"} {
		if _, err := NormalizeAssetScopeRange(bad); !errors.Is(err, ErrEmptyField) {
			t.Fatalf("range %q should be rejected, got %v", bad, err)
		}
	}
}

// ICP 任务创建：type 回落 domain、queries/target 二者取一、domain 归一
func TestNormalizeICPForCreate(t *testing.T) {
	if got, err := NormalizeICPTypeForCreate(""); err != nil || got != models.IcpTaskTypeDomain {
		t.Fatalf("empty type should default to domain, got %q / %v", got, err)
	}
	if got, err := NormalizeICPTypeForCreate(" Company "); err != nil || got != models.IcpTaskTypeCompany {
		t.Fatalf("Company should normalize, got %q / %v", got, err)
	}
	if _, err := NormalizeICPTypeForCreate("enterprise"); !errors.Is(err, ErrInvalidEnum) {
		t.Fatalf("unknown type should be rejected, got %v", err)
	}

	// queries 优先；target 只在 queries 为空时生效
	q, err := NormalizeICPQueriesForCreate(models.IcpTaskTypeDomain,
		[]string{"Example.COM. ", "example.com", ""}, "ignored.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(q) != 1 || q[0] != "example.com" {
		t.Fatalf("domain queries should be lowercased/deduped: %v", q)
	}
	q, err = NormalizeICPQueriesForCreate(models.IcpTaskTypeDomain, nil, " Example.ORG. ")
	if err != nil || len(q) != 1 || q[0] != "example.org" {
		t.Fatalf("target fallback failed: %v / %v", q, err)
	}
	// 企业名保持原样（大小写/空格内嵌有意义，不能小写）
	q, err = NormalizeICPQueriesForCreate(models.IcpTaskTypeCompany, []string{" 某某科技有限公司 "}, "")
	if err != nil || len(q) != 1 || q[0] != "某某科技有限公司" {
		t.Fatalf("company name should keep as-is: %v / %v", q, err)
	}
	if _, err := NormalizeICPQueriesForCreate(models.IcpTaskTypeDomain, nil, "  "); !errors.Is(err, ErrEmptyQueries) {
		t.Fatalf("empty queries+target should be rejected, got %v", err)
	}
}
