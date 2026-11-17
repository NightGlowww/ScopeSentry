package policy

import (
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

func intPtr(v int) *int { return &v }

func strPtr(v string) *string { return &v }

func codeSet(warnings []models.PolicyWarning) map[string]bool {
	out := make(map[string]bool, len(warnings))
	for _, w := range warnings {
		out[w.Code] = true
	}
	return out
}

func appliedSet(fields []string) map[string]bool {
	out := make(map[string]bool, len(fields))
	for _, f := range fields {
		out[f] = true
	}
	return out
}

// nil 不覆盖：空策略必须原样返回模板默认值，且不产生 warning / appliedFields。
func TestMergeConfigEmptyPolicyKeepsTemplate(t *testing.T) {
	base := models.ScanPolicyConfig{
		TemplateID:     "64b7f0c2a1b2c3d4e5f60718",
		Plugins:        []string{"p1", "p2"},
		ThreadNum:      intPtr(10),
		RateLimit:      intPtr(0),
		TimeoutSeconds: intPtr(60),
		Dicts:          &models.ScanPolicyDicts{Dir: strPtr("dir-a"), Subdomain: strPtr("sub-a")},
		Options:        map[string]string{"a": "1"},
	}

	res := MergeConfig(models.ScanPolicyConfig{}, base)
	if len(res.Warnings) != 0 {
		t.Fatalf("warnings = %d, want 0 (%v)", len(res.Warnings), res.Warnings)
	}
	if len(res.AppliedFields) != 0 {
		t.Fatalf("appliedFields = %v, want empty", res.AppliedFields)
	}
	if res.Config.TemplateID != base.TemplateID {
		t.Fatalf("templateId = %q, want %q", res.Config.TemplateID, base.TemplateID)
	}
	if res.Config.ThreadNum == nil || *res.Config.ThreadNum != 10 {
		t.Fatalf("threadNum = %v, want 10", res.Config.ThreadNum)
	}
	if res.Config.RateLimit == nil || *res.Config.RateLimit != 0 {
		t.Fatalf("rateLimit = %v, want 0（0 是合法值，不能被当成未设置）", res.Config.RateLimit)
	}
	if res.Config.Dicts == nil || res.Config.Dicts.Dir == nil || *res.Config.Dicts.Dir != "dir-a" {
		t.Fatalf("dicts.dir = %v, want dir-a", res.Config.Dicts)
	}
	if res.Config.Options["a"] != "1" {
		t.Fatalf("options.a = %q, want 1", res.Config.Options["a"])
	}
}

// 合并不能改写入参（base 的指针 / map / slice 必须与合并结果隔离）。
func TestMergeConfigDoesNotMutateInputs(t *testing.T) {
	base := models.ScanPolicyConfig{
		Plugins:   []string{"p1"},
		ThreadNum: intPtr(10),
		Dicts:     &models.ScanPolicyDicts{Dir: strPtr("dir-a")},
		Options:   map[string]string{"a": "1"},
	}
	policyCfg := models.ScanPolicyConfig{
		Plugins:   []string{"p2"},
		ThreadNum: intPtr(50),
		Dicts:     &models.ScanPolicyDicts{Dir: strPtr("dir-b")},
		Options:   map[string]string{"a": "9", "b": "2"},
	}

	res := MergeConfig(policyCfg, base)

	*res.Config.ThreadNum = 99
	res.Config.Options["a"] = "changed"
	*res.Config.Dicts.Dir = "changed"
	res.Config.Plugins[0] = "changed"

	if *base.ThreadNum != 10 {
		t.Fatalf("base.threadNum 被合并结果改脏: %d", *base.ThreadNum)
	}
	if base.Options["a"] != "1" {
		t.Fatalf("base.options 被合并结果改脏: %v", base.Options)
	}
	if *base.Dicts.Dir != "dir-a" {
		t.Fatalf("base.dicts 被合并结果改脏: %v", *base.Dicts.Dir)
	}
	if base.Plugins[0] != "p1" {
		t.Fatalf("base.plugins 被合并结果改脏: %v", base.Plugins)
	}
	if *policyCfg.ThreadNum != 50 || policyCfg.Options["a"] != "9" {
		t.Fatalf("policy 入参被改脏: %+v", policyCfg)
	}
}

// 冲突合并：策略覆盖模板默认，每个不同字段产一条 warning，appliedFields 记录生效字段。
func TestMergeConfigOverridesAndWarnings(t *testing.T) {
	base := models.ScanPolicyConfig{
		TemplateID:     "64b7f0c2a1b2c3d4e5f60718",
		Plugins:        []string{"a", "b"},
		ThreadNum:      intPtr(10),
		RateLimit:      intPtr(100),
		TimeoutSeconds: intPtr(60),
		Dicts: &models.ScanPolicyDicts{
			Dir:       strPtr("dir-1"),
			Subdomain: strPtr("sub-1"),
			Brute:     strPtr("brute-1"),
		},
		Options: map[string]string{"k1": "v1", "k2": "v2"},
	}
	policyCfg := models.ScanPolicyConfig{
		TemplateID:     "64b7f0c2a1b2c3d4e5f60719",
		Plugins:        []string{"b", "c"},
		ThreadNum:      intPtr(50),
		RateLimit:      intPtr(0),
		TimeoutSeconds: intPtr(120),
		Dicts:          &models.ScanPolicyDicts{Dir: strPtr("dir-2")},
		Options:        map[string]string{"k1": "v9", "k3": "v3"},
	}

	res := MergeConfig(policyCfg, base)

	if res.Config.TemplateID != policyCfg.TemplateID {
		t.Fatalf("templateId = %q, want 策略值 %q", res.Config.TemplateID, policyCfg.TemplateID)
	}
	if got := res.Config.Plugins; len(got) != 2 || got[0] != "b" || got[1] != "c" {
		t.Fatalf("plugins = %v, want [b c]", got)
	}
	if res.Config.ThreadNum == nil || *res.Config.ThreadNum != 50 {
		t.Fatalf("threadNum = %v, want 50", res.Config.ThreadNum)
	}
	if res.Config.RateLimit == nil || *res.Config.RateLimit != 0 {
		t.Fatalf("rateLimit = %v, want 0", res.Config.RateLimit)
	}
	if res.Config.TimeoutSeconds == nil || *res.Config.TimeoutSeconds != 120 {
		t.Fatalf("timeoutSeconds = %v, want 120", res.Config.TimeoutSeconds)
	}
	if res.Config.Dicts == nil ||
		res.Config.Dicts.Dir == nil || *res.Config.Dicts.Dir != "dir-2" ||
		res.Config.Dicts.Subdomain == nil || *res.Config.Dicts.Subdomain != "sub-1" ||
		res.Config.Dicts.Brute == nil || *res.Config.Dicts.Brute != "brute-1" {
		t.Fatalf("dicts = %+v, want dir-2/sub-1/brute-1", res.Config.Dicts)
	}
	wantOpts := map[string]string{"k1": "v9", "k2": "v2", "k3": "v3"}
	for k, want := range wantOpts {
		if res.Config.Options[k] != want {
			t.Fatalf("options[%s] = %q, want %q (%v)", k, res.Config.Options[k], want, res.Config.Options)
		}
	}

	codes := codeSet(res.Warnings)
	for _, want := range []string{
		"template_override", "plugins_override", "thread_num_override", "rate_limit_override",
		"timeout_override", "dict_override", "option_override",
	} {
		if !codes[want] {
			t.Fatalf("缺 warning %s，实际 %v", want, res.Warnings)
		}
	}
	if len(res.Warnings) != 7 {
		t.Fatalf("warnings = %d, want 7 (%v)", len(res.Warnings), res.Warnings)
	}

	applied := appliedSet(res.AppliedFields)
	for _, want := range []string{"templateId", "plugins", "threadNum", "rateLimit", "timeoutSeconds", "dicts.dir", "options"} {
		if !applied[want] {
			t.Fatalf("appliedFields 缺 %s，实际 %v", want, res.AppliedFields)
		}
	}
	if applied["dicts.subdomain"] || applied["dicts.brute"] {
		t.Fatalf("未设置的 dicts 子字段不应出现在 appliedFields: %v", res.AppliedFields)
	}
}

// 值相同 / 集合相同不算冲突：plugins 顺序不同但集合一致时不产 warning。
func TestMergeConfigSameValueNoWarning(t *testing.T) {
	base := models.ScanPolicyConfig{
		TemplateID: "64b7f0c2a1b2c3d4e5f60718",
		Plugins:    []string{"a", "b"},
		ThreadNum:  intPtr(10),
		Options:    map[string]string{"k": "v"},
	}
	policyCfg := models.ScanPolicyConfig{
		TemplateID: "64b7f0c2a1b2c3d4e5f60718",
		Plugins:    []string{"b", "a"},
		ThreadNum:  intPtr(10),
		Options:    map[string]string{"k": "v"},
	}

	res := MergeConfig(policyCfg, base)
	if len(res.Warnings) != 0 {
		t.Fatalf("warnings = %v, want empty", res.Warnings)
	}
	if len(res.Config.Plugins) != 2 || res.Config.Plugins[0] != "b" {
		t.Fatalf("plugins = %v, want 策略顺序 [b a]", res.Config.Plugins)
	}
	if len(res.AppliedFields) == 0 {
		t.Fatalf("值相同也应记录 appliedFields（策略确实提供了这些字段）")
	}
}

// 显式清空字典引用：dicts.dir="" 覆盖模板默认，并产 dict_override。
func TestMergeConfigExplicitClearDict(t *testing.T) {
	base := models.ScanPolicyConfig{Dicts: &models.ScanPolicyDicts{Dir: strPtr("dir-1")}}
	policyCfg := models.ScanPolicyConfig{Dicts: &models.ScanPolicyDicts{Dir: strPtr("")}}

	res := MergeConfig(policyCfg, base)
	if res.Config.Dicts == nil || res.Config.Dicts.Dir == nil || *res.Config.Dicts.Dir != "" {
		t.Fatalf("dicts.dir = %+v, want 显式空串", res.Config.Dicts)
	}
	codes := codeSet(res.Warnings)
	if !codes["dict_override"] {
		t.Fatalf("缺 dict_override warning: %v", res.Warnings)
	}
}

// dicts 结构体存在但三个子字段都没设置：不改变模板默认形状（不凭空造出 dicts）。
func TestMergeConfigEmptyDictsDoesNotAllocate(t *testing.T) {
	base := models.ScanPolicyConfig{ThreadNum: intPtr(10)}
	res := MergeConfig(models.ScanPolicyConfig{Dicts: &models.ScanPolicyDicts{}}, base)
	if res.Config.Dicts != nil {
		t.Fatalf("dicts = %+v, want nil", res.Config.Dicts)
	}
	if len(res.Warnings) != 0 || len(res.AppliedFields) != 0 {
		t.Fatalf("warnings=%v applied=%v, want empty", res.Warnings, res.AppliedFields)
	}
}

// 模板基线：只取 templateId + 各阶段插件并集（按 PLUGINSMODULES 顺序去重）。
func TestBaselineFromTemplate(t *testing.T) {
	phases := models.ScanTemplatePhases{
		TargetHandler: []string{"x"},
		SubdomainScan: []string{"a", "b", "a"},
		PortScan:      []string{"c"},
		PassiveScan:   []string{"z"},
	}
	cfg := BaselineFromTemplate(" 64b7f0c2a1b2c3d4e5f60718 ", phases)
	if cfg.TemplateID != "64b7f0c2a1b2c3d4e5f60718" {
		t.Fatalf("templateId = %q", cfg.TemplateID)
	}
	want := []string{"x", "a", "b", "c", "z"}
	if len(cfg.Plugins) != len(want) {
		t.Fatalf("plugins = %v, want %v", cfg.Plugins, want)
	}
	for i := range want {
		if cfg.Plugins[i] != want[i] {
			t.Fatalf("plugins = %v, want %v", cfg.Plugins, want)
		}
	}
	if cfg.ThreadNum != nil || cfg.RateLimit != nil || cfg.TimeoutSeconds != nil || cfg.Dicts != nil {
		t.Fatalf("模板没有全局并发/速率/超时/字典，不应臆造默认值: %+v", cfg)
	}

	empty := BaselineFromTemplate("64b7f0c2a1b2c3d4e5f60718", models.ScanTemplatePhases{})
	if empty.Plugins != nil {
		t.Fatalf("空模板 plugins = %v, want nil", empty.Plugins)
	}
}
