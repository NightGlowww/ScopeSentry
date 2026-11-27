package taskpolicy

import (
	"strings"
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

func intPtr(v int) *int { return &v }

func strPtr(v string) *string { return &v }

// newTemplate 造一张「四阶段有插件 + 同名参数表」的模板，覆盖「命中 / 未命中」两种阶段。
func newTemplate() *models.ScanTemplate {
	return &models.ScanTemplate{
		TargetHandler:     []string{"th-1"},
		SubdomainScan:     []string{"sd-1", "sd-2"},
		DirScan:           []string{"dir-1", "dir-2"},
		VulnerabilityScan: []string{"vul-1"},
		Parameters: models.Parameters{
			TargetHandler:     map[string]string{"th-1": "-i foo"},
			SubdomainScan:     map[string]string{"sd-1": "-a 1", "sd-2": "-a 2"},
			DirScan:           map[string]string{"dir-1": "-d d1", "dir-2": "-d d2"},
			VulnerabilityScan: map[string]string{"vul-1": "-s critical,high"},
		},
	}
}

func hasApplied(fields []string, want string) bool {
	for _, f := range fields {
		if f == want {
			return true
		}
	}
	return false
}

func hasWarning(warnings []models.PolicyWarning, code string) bool {
	for _, w := range warnings {
		if w.Code == code {
			return true
		}
	}
	return false
}

// 不选策略（空 config）时任务一字不改：向后兼容的底线。
func TestApplyToTaskEmptyConfigKeepsTask(t *testing.T) {
	task := &models.Task{Template: "tpl-a", Name: "task-a"}
	applied, warnings := ApplyToTask(task, models.ScanPolicyConfig{})
	if task.Template != "tpl-a" {
		t.Fatalf("template = %q, want tpl-a", task.Template)
	}
	if len(applied) != 0 {
		t.Fatalf("applied = %v, want empty", applied)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want empty", warnings)
	}
}

// 任务已选模板与策略模板不同：保留任务模板（用户选择优先），只给冲突提示。
func TestApplyToTaskKeepsUserTemplateOnConflict(t *testing.T) {
	task := &models.Task{Template: "tpl-a", Name: "task-a"}
	cfg := models.ScanPolicyConfig{TemplateID: "  tpl-b  "}
	applied, warnings := ApplyToTask(task, cfg)
	if task.Template != "tpl-a" {
		t.Fatalf("template = %q, want tpl-a（不得覆盖用户已选模板）", task.Template)
	}
	if len(applied) != 0 {
		t.Fatalf("applied = %v, want empty", applied)
	}
	if !hasWarning(warnings, CodeTemplateConflict) {
		t.Fatalf("warnings = %v, want %s", warnings, CodeTemplateConflict)
	}
}

// 策略模板与任务模板相同：不算覆盖、不产出 warning。
func TestApplyToTaskSameTemplateNoWarning(t *testing.T) {
	task := &models.Task{Template: "tpl-a"}
	applied, warnings := ApplyToTask(task, models.ScanPolicyConfig{TemplateID: "tpl-a"})
	if task.Template != "tpl-a" {
		t.Fatalf("template = %q, want tpl-a", task.Template)
	}
	if len(applied) != 0 {
		t.Fatalf("applied = %v, want empty", applied)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want empty", warnings)
	}
}

// 任务模板为空时直接填策略模板，不产生「覆盖」提示。
func TestApplyToTaskFillsEmptyTemplate(t *testing.T) {
	task := &models.Task{}
	applied, warnings := ApplyToTask(task, models.ScanPolicyConfig{TemplateID: "tpl-b"})
	if task.Template != "tpl-b" {
		t.Fatalf("template = %q, want tpl-b", task.Template)
	}
	if !hasApplied(applied, "templateId") {
		t.Fatalf("applied = %v, want templateId", applied)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want empty", warnings)
	}
}

// 插件白名单：命中阶段裁到策略点名的插件并同步删参数；未命中阶段原样保留。
func TestApplyToTemplateRestrictsMatchedPhases(t *testing.T) {
	tpl := newTemplate()
	cfg := models.ScanPolicyConfig{Plugins: []string{"sd-2", "dir-1"}}
	applied, warnings := ApplyToTemplate(tpl, cfg)

	if len(applied) != 1 || applied[0] != "plugins" {
		t.Fatalf("applied = %v, want [plugins]", applied)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want empty", warnings)
	}
	if got := tpl.SubdomainScan; len(got) != 1 || got[0] != "sd-2" {
		t.Fatalf("SubdomainScan = %v, want [sd-2]", got)
	}
	if _, ok := tpl.Parameters.SubdomainScan["sd-1"]; ok {
		t.Fatalf("sd-1 的参数应随插件一起删除")
	}
	if _, ok := tpl.Parameters.SubdomainScan["sd-2"]; !ok {
		t.Fatalf("sd-2 的参数应保留")
	}
	if got := tpl.DirScan; len(got) != 1 || got[0] != "dir-1" {
		t.Fatalf("DirScan = %v, want [dir-1]", got)
	}
	if _, ok := tpl.Parameters.DirScan["dir-2"]; ok {
		t.Fatalf("dir-2 的参数应随插件一起删除")
	}
	// 策略没点名的阶段：不清空、不动参数
	if got := tpl.TargetHandler; len(got) != 1 || got[0] != "th-1" {
		t.Fatalf("TargetHandler = %v, want [th-1]", got)
	}
	if got := tpl.VulnerabilityScan; len(got) != 1 || got[0] != "vul-1" {
		t.Fatalf("VulnerabilityScan = %v, want [vul-1]", got)
	}
	if _, ok := tpl.Parameters.VulnerabilityScan["vul-1"]; !ok {
		t.Fatalf("VulnerabilityScan 参数应保留")
	}
}

// 策略插件在模板里一个都没命中：清单完全不动，产出一条 plugins_not_applied。
func TestApplyToTemplateNoMatchKeepsLists(t *testing.T) {
	tpl := newTemplate()
	cfg := models.ScanPolicyConfig{Plugins: []string{"not-in-template"}}
	applied, warnings := ApplyToTemplate(tpl, cfg)

	if len(applied) != 0 {
		t.Fatalf("applied = %v, want empty", applied)
	}
	if !hasWarning(warnings, CodePluginsNotApplied) {
		t.Fatalf("warnings = %v, want %s", warnings, CodePluginsNotApplied)
	}
	if got := tpl.SubdomainScan; len(got) != 2 {
		t.Fatalf("SubdomainScan = %v, want 2 个插件不变", got)
	}
	if _, ok := tpl.Parameters.SubdomainScan["sd-1"]; !ok {
		t.Fatalf("未命中时不应删除参数")
	}
}

// 插件引用去空白 / 去重：带空格的 sd-2 与 sd-2 等价。
func TestApplyToTemplateNormalizesPluginRefs(t *testing.T) {
	tpl := newTemplate()
	cfg := models.ScanPolicyConfig{Plugins: []string{" sd-2 ", "sd-2", ""}}
	applied, _ := ApplyToTemplate(tpl, cfg)
	if len(applied) != 1 {
		t.Fatalf("applied = %v, want [plugins]", applied)
	}
	if got := tpl.SubdomainScan; len(got) != 1 || got[0] != "sd-2" {
		t.Fatalf("SubdomainScan = %v, want [sd-2]", got)
	}
}

// 空插件清单等价于「不约束」：模板不动。
func TestApplyToTemplateEmptyPluginsKeepsTemplate(t *testing.T) {
	tpl := newTemplate()
	applied, warnings := ApplyToTemplate(tpl, models.ScanPolicyConfig{Plugins: []string{"  "}})
	if len(applied) != 0 || len(warnings) != 0 {
		t.Fatalf("applied=%v warnings=%v, want empty", applied, warnings)
	}
	if got := tpl.DirScan; len(got) != 2 {
		t.Fatalf("DirScan = %v, want 2", got)
	}
}

// nil 模板不 panic。
func TestApplyToTemplateNil(t *testing.T) {
	applied, warnings := ApplyToTemplate(nil, models.ScanPolicyConfig{Plugins: []string{"x"}})
	if len(applied) != 0 || len(warnings) != 0 {
		t.Fatalf("applied=%v warnings=%v, want empty", applied, warnings)
	}
}

// 纯函数不得改写入参的策略 config。
func TestApplyToTemplateDoesNotMutatePolicyConfig(t *testing.T) {
	plugins := []string{"sd-2"}
	cfg := models.ScanPolicyConfig{Plugins: plugins}
	tpl := newTemplate()
	_, _ = ApplyToTemplate(tpl, cfg)
	if len(cfg.Plugins) != 1 || cfg.Plugins[0] != "sd-2" {
		t.Fatalf("cfg.Plugins = %v, want [sd-2]", cfg.Plugins)
	}
}

// 没有承载位置的三个旋钮必须显式告警，且顺序稳定（threadNum → rateLimit → timeoutSeconds → options）。
// dicts 不再属于这一类：它已经能通过模板参数的 {dict.<类别>.<名称>} 占位符生效（见 applyDictsToTemplate），
// 是否命中占位符要拿到模板才知道，由那一侧给结论。
func TestUnsupportedWarningsCoversAllKnobs(t *testing.T) {
	cfg := models.ScanPolicyConfig{
		ThreadNum:      intPtr(20),
		RateLimit:      intPtr(0),
		TimeoutSeconds: intPtr(600),
		Dicts:          &models.ScanPolicyDicts{Dir: strPtr("dir-common"), Subdomain: strPtr("")},
		Options:        map[string]string{"rate": "50"},
	}
	warnings := UnsupportedWarnings(cfg)
	if len(warnings) != 4 {
		t.Fatalf("warnings = %d, want 4: %+v", len(warnings), warnings)
	}
	wantFields := []string{"threadNum", "rateLimit", "timeoutSeconds", "options"}
	for i, want := range wantFields {
		if warnings[i].Field != want {
			t.Fatalf("warnings[%d].Field = %s, want %s", i, warnings[i].Field, want)
		}
	}
	if warnings[0].Code != CodeExecLimitsUnsupported || warnings[1].Code != CodeExecLimitsUnsupported || warnings[2].Code != CodeExecLimitsUnsupported {
		t.Fatalf("并发/速率/超时 code 应为 %s: %+v", CodeExecLimitsUnsupported, warnings)
	}
	if warnings[3].Code != CodeOptionsUnsupported {
		t.Fatalf("options code = %s, want %s", warnings[3].Code, CodeOptionsUnsupported)
	}
}

// 只有空白的字典引用不算「设置了字典」，不产生 warning。
func TestUnsupportedWarningsSkipsBlankDicts(t *testing.T) {
	cfg := models.ScanPolicyConfig{Dicts: &models.ScanPolicyDicts{Dir: strPtr("  ")}}
	if warnings := UnsupportedWarnings(cfg); len(warnings) != 0 {
		t.Fatalf("warnings = %+v, want empty", warnings)
	}
}

// 字典重定向（ARL-Next 1.5.2 对齐）：策略里的字典名写进模板参数的 {dict.<类别>.<名称>} 占位符。
// 只重定向模板里已存在的同类占位符，不臆造插件 CLI 旗标。
func TestApplyToTemplateRedirectsDictPlaceholders(t *testing.T) {
	tpl := newTemplate()
	tpl.PortScan = []string{"ps-1"}
	tpl.Parameters.PortScan = map[string]string{"ps-1": "-p {dict.port.default} -sV --timeout 3"}
	tpl.DirScan = []string{"dir-1"}
	tpl.Parameters.DirScan = map[string]string{"dir-1": "-w {dict.dir.default} -t 30"}

	cfg := models.ScanPolicyConfig{Dicts: &models.ScanPolicyDicts{
		Port: strPtr("port-custom-2026"),
		Dir:  strPtr("dir-deep"),
	}}
	applied, warnings := ApplyToTemplate(tpl, cfg)

	if got := tpl.Parameters.PortScan["ps-1"]; got != "-p {dict.port.port-custom-2026} -sV --timeout 3" {
		t.Fatalf("port param = %q", got)
	}
	if got := tpl.Parameters.DirScan["dir-1"]; got != "-w {dict.dir.dir-deep} -t 30" {
		t.Fatalf("dir param = %q", got)
	}
	if !hasApplied(applied, "dicts.port") || !hasApplied(applied, "dicts.dir") {
		t.Fatalf("applied = %v, want dicts.port + dicts.dir", applied)
	}
	if hasWarning(warnings, CodeDictsUnsupported) {
		t.Fatalf("unexpected warning: %v", warnings)
	}
}

// 模板里没有该类别占位符时不臆造参数，只给一条「未命中」说明。
func TestApplyToTemplateDictWithoutPlaceholderWarns(t *testing.T) {
	tpl := newTemplate()
	cfg := models.ScanPolicyConfig{Dicts: &models.ScanPolicyDicts{Brute: strPtr("brute-big")}}
	applied, warnings := ApplyToTemplate(tpl, cfg)

	if len(applied) != 0 {
		t.Fatalf("applied = %v, want empty", applied)
	}
	if !hasWarning(warnings, CodeDictsUnsupported) {
		t.Fatalf("warnings = %v, want %s", warnings, CodeDictsUnsupported)
	}
	for _, phase := range phasesOf(tpl) {
		for hash, raw := range phase.Params {
			if strings.Contains(raw, "brute-big") {
				t.Fatalf("must not fabricate param %s/%s = %q", phase.Name, hash, raw)
			}
		}
	}
}
