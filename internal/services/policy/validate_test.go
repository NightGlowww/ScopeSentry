package policy

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

func errFields(v models.PolicyValidation) map[string]string {
	out := make(map[string]string, len(v.Errors))
	for _, e := range v.Errors {
		out[e.Field] = e.Code
	}
	return out
}

// 范围边界：并发 1-200、速率 0-1000、超时 10-3600。
func TestValidateConfigShapeRanges(t *testing.T) {
	cases := []struct {
		name      string
		cfg       models.ScanPolicyConfig
		wantField string
		wantCode  string
	}{
		{name: "thread 0", cfg: models.ScanPolicyConfig{ThreadNum: intPtr(0)}, wantField: "threadNum", wantCode: "thread_num_out_of_range"},
		{name: "thread -1", cfg: models.ScanPolicyConfig{ThreadNum: intPtr(-1)}, wantField: "threadNum", wantCode: "thread_num_out_of_range"},
		{name: "thread 1 ok", cfg: models.ScanPolicyConfig{ThreadNum: intPtr(1)}},
		{name: "thread 200 ok", cfg: models.ScanPolicyConfig{ThreadNum: intPtr(200)}},
		{name: "thread 201", cfg: models.ScanPolicyConfig{ThreadNum: intPtr(201)}, wantField: "threadNum", wantCode: "thread_num_out_of_range"},
		{name: "rate -1", cfg: models.ScanPolicyConfig{RateLimit: intPtr(-1)}, wantField: "rateLimit", wantCode: "rate_limit_out_of_range"},
		{name: "rate 0 ok", cfg: models.ScanPolicyConfig{RateLimit: intPtr(0)}},
		{name: "rate 1000 ok", cfg: models.ScanPolicyConfig{RateLimit: intPtr(1000)}},
		{name: "rate 1001", cfg: models.ScanPolicyConfig{RateLimit: intPtr(1001)}, wantField: "rateLimit", wantCode: "rate_limit_out_of_range"},
		{name: "timeout 9", cfg: models.ScanPolicyConfig{TimeoutSeconds: intPtr(9)}, wantField: "timeoutSeconds", wantCode: "timeout_out_of_range"},
		{name: "timeout 10 ok", cfg: models.ScanPolicyConfig{TimeoutSeconds: intPtr(10)}},
		{name: "timeout 3600 ok", cfg: models.ScanPolicyConfig{TimeoutSeconds: intPtr(3600)}},
		{name: "timeout 3601", cfg: models.ScanPolicyConfig{TimeoutSeconds: intPtr(3601)}, wantField: "timeoutSeconds", wantCode: "timeout_out_of_range"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ValidateConfigShape(tc.cfg)
			if tc.wantField == "" {
				if !got.Valid || len(got.Errors) != 0 {
					t.Fatalf("valid = %v, errors = %v, want valid", got.Valid, got.Errors)
				}
				return
			}
			if got.Valid {
				t.Fatalf("valid = true, want false")
			}
			if code, ok := errFields(got)[tc.wantField]; !ok || code != tc.wantCode {
				t.Fatalf("errors = %v, want %s/%s", got.Errors, tc.wantField, tc.wantCode)
			}
		})
	}
}

// 归一化：trim、插件去重（warning）、空 options/dicts 折叠。
func TestValidateConfigShapeNormalize(t *testing.T) {
	cfg := models.ScanPolicyConfig{
		TemplateID: " 64b7f0c2a1b2c3d4e5f60718 ",
		Plugins:    []string{" a ", "a", "", "b"},
		Dicts:      &models.ScanPolicyDicts{Dir: strPtr(" dir-top "), Subdomain: strPtr("")},
		Options:    map[string]string{" k ": " v ", "": "dropped"},
	}
	got := ValidateConfigShape(cfg)
	if !got.Valid {
		t.Fatalf("valid = false, errors = %v", got.Errors)
	}
	if got.Config.TemplateID != "64b7f0c2a1b2c3d4e5f60718" {
		t.Fatalf("templateId = %q", got.Config.TemplateID)
	}
	if len(got.Config.Plugins) != 2 || got.Config.Plugins[0] != "a" || got.Config.Plugins[1] != "b" {
		t.Fatalf("plugins = %v, want [a b]", got.Config.Plugins)
	}
	if got.Config.Dicts == nil || *got.Config.Dicts.Dir != "dir-top" {
		t.Fatalf("dicts.dir = %+v, want dir-top", got.Config.Dicts)
	}
	if got.Config.Dicts.Subdomain == nil || *got.Config.Dicts.Subdomain != "" {
		t.Fatalf("dicts.subdomain = %+v, want 显式空串", got.Config.Dicts.Subdomain)
	}
	if got.Config.Options["k"] != "v" || len(got.Config.Options) != 1 {
		t.Fatalf("options = %v, want {k:v}", got.Config.Options)
	}
	codes := make(map[string]bool, len(got.Warnings))
	for _, w := range got.Warnings {
		codes[w.Code] = true
	}
	if !codes["plugin_deduped"] || !codes["option_key_dropped"] {
		t.Fatalf("warnings = %v, want plugin_deduped + option_key_dropped", got.Warnings)
	}
}

func TestValidateConfigShapeTemplateInvalid(t *testing.T) {
	got := ValidateConfigShape(models.ScanPolicyConfig{TemplateID: "not-an-objectid"})
	if got.Valid {
		t.Fatalf("valid = true, want false")
	}
	if code := errFields(got)["templateId"]; code != "template_invalid" {
		t.Fatalf("errors = %v, want template_invalid", got.Errors)
	}
}

func TestValidateConfigShapeDictRefs(t *testing.T) {
	bad := []string{"../etc/passwd", "dir/sub", "dir\\sub", "a\tb"}
	for _, v := range bad {
		got := ValidateConfigShape(models.ScanPolicyConfig{Dicts: &models.ScanPolicyDicts{Dir: strPtr(v)}})
		if got.Valid {
			t.Fatalf("%q valid = true, want false", v)
		}
		if code := errFields(got)["dicts.dir"]; code != "dict_ref_invalid" {
			t.Fatalf("%q errors = %v, want dict_ref_invalid", v, got.Errors)
		}
	}

	long := strings.Repeat("a", models.ScanPolicyMaxDictRefRunes+1)
	got := ValidateConfigShape(models.ScanPolicyConfig{Dicts: &models.ScanPolicyDicts{Subdomain: strPtr(long)}})
	if code := errFields(got)["dicts.subdomain"]; code != "dict_ref_too_long" {
		t.Fatalf("errors = %v, want dict_ref_too_long", got.Errors)
	}

	ok := ValidateConfigShape(models.ScanPolicyConfig{Dicts: &models.ScanPolicyDicts{
		Dir: strPtr("dir-top-1000.txt"), Subdomain: strPtr("sub-domain"), Brute: strPtr("pwd"),
	}})
	if !ok.Valid {
		t.Fatalf("合法字典引用被判非法: %v", ok.Errors)
	}
}

func TestValidateConfigShapeOptionsLimit(t *testing.T) {
	opts := make(map[string]string, models.ScanPolicyMaxOptions+1)
	for i := 0; i <= models.ScanPolicyMaxOptions; i++ {
		opts["k"+strconv.Itoa(i)] = "v"
	}
	got := ValidateConfigShape(models.ScanPolicyConfig{Options: opts})
	found := false
	for _, e := range got.Errors {
		if e.Code == "options_too_many" {
			found = true
		}
	}
	if !found {
		t.Fatalf("errors = %v, want options_too_many", got.Errors)
	}
}

// JSON 语义：rateLimit=0 必须解成非 nil 指针；dicts 缺省字段保持 nil。
func TestScanPolicyConfigJSONPointers(t *testing.T) {
	raw := []byte(`{"rateLimit":0,"threadNum":50,"dicts":{"dir":"d1"},"plugins":["a"]}`)
	var cfg models.ScanPolicyConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.RateLimit == nil || *cfg.RateLimit != 0 {
		t.Fatalf("rateLimit = %v, want 0 指针", cfg.RateLimit)
	}
	if cfg.ThreadNum == nil || *cfg.ThreadNum != 50 {
		t.Fatalf("threadNum = %v, want 50", cfg.ThreadNum)
	}
	if cfg.Dicts == nil || cfg.Dicts.Dir == nil || *cfg.Dicts.Dir != "d1" {
		t.Fatalf("dicts = %+v", cfg.Dicts)
	}
	if cfg.Dicts.Subdomain != nil || cfg.Dicts.Brute != nil {
		t.Fatalf("缺省 dicts 子字段应为 nil: %+v", cfg.Dicts)
	}
}

// 引用校验：fake checker，只验证「缺失项 → 结构化 issue」与「模板不存在」。
type fakeRefs struct {
	missingPlugins []string
	templateOK     bool
	gotPlugins     []string
	gotTemplate    string
}

func (f *fakeRefs) MissingPlugins(_ context.Context, plugins []string) ([]string, error) {
	f.gotPlugins = append([]string(nil), plugins...)
	return f.missingPlugins, nil
}

func (f *fakeRefs) TemplateExists(_ context.Context, templateID string) (bool, error) {
	f.gotTemplate = templateID
	return f.templateOK, nil
}

func TestValidateConfigRefs(t *testing.T) {
	const tid = "64b7f0c2a1b2c3d4e5f60718"
	refs := &fakeRefs{missingPlugins: []string{"ghost"}, templateOK: false}
	issues, err := ValidateConfigRefs(context.Background(), models.ScanPolicyConfig{
		TemplateID: tid,
		Plugins:    []string{"p1", "ghost"},
	}, refs)
	if err != nil {
		t.Fatalf("refs: %v", err)
	}
	if refs.gotTemplate != tid {
		t.Fatalf("template checker 入参 = %q, want %q", refs.gotTemplate, tid)
	}
	if len(refs.gotPlugins) != 2 {
		t.Fatalf("plugin checker 入参 = %v", refs.gotPlugins)
	}
	codes := make(map[string]int, len(issues))
	for _, i := range issues {
		codes[i.Code]++
	}
	if codes["template_not_found"] != 1 || codes["plugin_not_found"] != 1 {
		t.Fatalf("issues = %v, want template_not_found + plugin_not_found", issues)
	}
}

// Service.Test：形状非法时不查引用；形状合法时把引用问题合并进 errors。
func TestServiceTestSkipsRefsWhenShapeInvalid(t *testing.T) {
	refs := &fakeRefs{}
	svc := NewService(WithRefChecker(refs))
	if _, err := svc.Test(context.Background(), models.ScanPolicyConfig{ThreadNum: intPtr(999)}); err != nil {
		t.Fatalf("test: %v", err)
	}
	if refs.gotPlugins != nil || refs.gotTemplate != "" {
		t.Fatalf("形状非法时不应查引用: plugins=%v template=%q", refs.gotPlugins, refs.gotTemplate)
	}
}

func TestServiceTestMergesRefIssues(t *testing.T) {
	refs := &fakeRefs{missingPlugins: []string{"ghost"}, templateOK: true}
	svc := NewService(WithRefChecker(refs))
	got, err := svc.Test(context.Background(), models.ScanPolicyConfig{
		TemplateID: "64b7f0c2a1b2c3d4e5f60718",
		Plugins:    []string{"ghost"},
		ThreadNum:  intPtr(10),
	})
	if err != nil {
		t.Fatalf("test: %v", err)
	}
	if got.Valid || len(got.Errors) != 1 || got.Errors[0].Code != "plugin_not_found" {
		t.Fatalf("result = %+v, want 1 个 plugin_not_found", got)
	}
}
