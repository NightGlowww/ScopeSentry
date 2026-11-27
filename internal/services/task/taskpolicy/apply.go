// Package taskpolicy 把「扫描策略」落到真实的扫描载荷上。
//
// 背景（docs/audit/EMPTY_COLLECTIONS.md §2 scan_policy 行）：internal/services/policy
// 只提供策略的存取与合并预览（/api/policy/apply 是纯预览、不写库），建任务链路
// （internal/services/task/**、internal/api/handlers/task/**）此前完全不读策略，
// 所以 scan_policy 是一条「能建、没人消费」的孤岛。
//
// 这里补上消费端，并且只消费扫描载荷里**真实存在**的字段：
//
//   - templateId → 选哪个扫描模板（models.Task.Template，随后 GetScanTemplate 读该模板）；
//   - plugins    → 模板各阶段插件清单的白名单（models.ScanTemplate 的 14 个 <Phase> 切片，
//     以及同名 Parameters[phase][hash] 参数表，两者同步裁剪）。
//
// 其余四个旋钮没有承载位置，因此不臆造字段，只产出 warning 说明它不影响执行：
//
//   - threadNum / rateLimit / timeoutSeconds：扫描模板没有全局执行字段。模板参数是
//     「每个插件自己的 CLI 字符串」（internal/services/task/common/common.go 的
//     GetScanTemplate 只在 VulList 非空时追加 -t，其余原样下发），并发/速率/超时的
//     旗标名由各插件自己定义（-t 在 nuclei 里是模板路径），服务端按插件猜旗标就是臆造；
//   - dicts：模板参数里以 {dict.<类别>.<名称>} 占位符引用字典，策略里的字典名没有可写位置；
//   - options：模板参数没有自由键值槽位。
//
// 本文件的函数都是纯函数：不读写库、不改入参（策略 config 只读），可直接单测。
package taskpolicy

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// appliedFields / warning 口径与 internal/services/policy/merge.go 对齐：
// appliedFields 用模型字段名（templateId / plugins），warning 带 field + code。
const (
	// CodeTemplateConflict 策略模板与任务已选模板不同：保留任务模板（不覆盖用户选择）
	CodeTemplateConflict = "template_conflict"
	// CodePluginsNotApplied 策略插件在模板里一个都没命中（清单保持不变）
	CodePluginsNotApplied = "plugins_not_applied"
	// CodeExecLimitsUnsupported 并发 / 速率 / 超时在扫描载荷里没有全局字段
	CodeExecLimitsUnsupported = "exec_limits_unsupported"
	// CodeDictsUnsupported 字典引用没有可写位置
	CodeDictsUnsupported = "dicts_unsupported"
	// CodeOptionsUnsupported options 没有可写位置
	CodeOptionsUnsupported = "options_unsupported"
)

// phaseRef 一个扫描阶段的「插件清单 + 参数表」视角。
//
// 两个字段必须成对裁剪：阶段切片决定跑哪些插件，Parameters[phase] 决定每个插件的
// CLI 参数；只裁一边会让节点侧看到不一致的数据。
type phaseRef struct {
	Name   string
	List   *[]string
	Params map[string]string
}

// phasesOf 按 constants.PLUGINSMODULES 的顺序暴露 ScanTemplate 的 14 个阶段。
func phasesOf(tpl *models.ScanTemplate) []phaseRef {
	return []phaseRef{
		{Name: "TargetHandler", List: &tpl.TargetHandler, Params: tpl.Parameters.TargetHandler},
		{Name: "SubdomainScan", List: &tpl.SubdomainScan, Params: tpl.Parameters.SubdomainScan},
		{Name: "SubdomainSecurity", List: &tpl.SubdomainSecurity, Params: tpl.Parameters.SubdomainSecurity},
		{Name: "PortScanPreparation", List: &tpl.PortScanPreparation, Params: tpl.Parameters.PortScanPreparation},
		{Name: "PortScan", List: &tpl.PortScan, Params: tpl.Parameters.PortScan},
		{Name: "PortFingerprint", List: &tpl.PortFingerprint, Params: tpl.Parameters.PortFingerprint},
		{Name: "AssetMapping", List: &tpl.AssetMapping, Params: tpl.Parameters.AssetMapping},
		{Name: "AssetHandle", List: &tpl.AssetHandle, Params: tpl.Parameters.AssetHandle},
		{Name: "URLScan", List: &tpl.URLScan, Params: tpl.Parameters.URLScan},
		{Name: "WebCrawler", List: &tpl.WebCrawler, Params: tpl.Parameters.WebCrawler},
		{Name: "URLSecurity", List: &tpl.URLSecurity, Params: tpl.Parameters.URLSecurity},
		{Name: "DirScan", List: &tpl.DirScan, Params: tpl.Parameters.DirScan},
		{Name: "VulnerabilityScan", List: &tpl.VulnerabilityScan, Params: tpl.Parameters.VulnerabilityScan},
		{Name: "PassiveScan", List: &tpl.PassiveScan, Params: tpl.Parameters.PassiveScan},
	}
}

// ApplyToTask 把策略落到任务的可持久化字段上（当前只有扫描模板）。
//
// 语义（谁优先）：
//   - task.Template 为空（API / 脚本建任务没填模板）→ 用策略模板补上并持久化；
//   - task.Template 已填且与策略模板不同 → **保留任务模板**，只产出一条 template_conflict
//     提示。UI 里策略只是「预填默认值」，用户之后改的模板必须算数，否则表单会骗人；
//   - 策略缺席 / 字段为空 → task 一字不改（不选策略则行为不变）。
//
// 补上的模板随任务落库，所以重测 / 定时轮次仍然用同一个模板。
func ApplyToTask(task *models.Task, cfg models.ScanPolicyConfig) ([]string, []models.PolicyWarning) {
	if task == nil {
		return nil, nil
	}
	applied := make([]string, 0, 1)
	warnings := UnsupportedWarnings(cfg)

	templateID := strings.TrimSpace(cfg.TemplateID)
	if templateID == "" {
		return applied, warnings
	}
	switch current := strings.TrimSpace(task.Template); {
	case current == "":
		task.Template = templateID
		applied = append(applied, "templateId")
	case current != templateID:
		warnings = append(warnings, models.PolicyWarning{
			Field:   "templateId",
			Code:    CodeTemplateConflict,
			Message: fmt.Sprintf("策略模板 %s 与任务已选模板 %s 不同，保留任务模板", templateID, current),
		})
	}
	return applied, warnings
}

// ApplyToTemplate 用策略的插件清单收窄扫描模板。
//
// 判定规则（「策略点名了哪个阶段就约束哪个阶段」）：
//   - 某个阶段的插件清单里命中了至少一个策略插件 → 该阶段只保留策略点名的插件，
//     被移除插件的 Parameters 条目同步删除；
//   - 某阶段一个都没命中 → 该阶段原样保留（策略没管这个阶段，不能因为策略只列了
//     DirScan 插件就把 PortScan 整段清空）；
//   - 策略插件在整张模板里一个都没命中 → 清单完全不动，产出一条 plugins_not_applied。
func ApplyToTemplate(tpl *models.ScanTemplate, cfg models.ScanPolicyConfig) ([]string, []models.PolicyWarning) {
	if tpl == nil {
		return nil, nil
	}
	// 字典重定向先做：它与插件裁剪互相独立——只改字典的策略也要生效。
	dictApplied, dictWarnings := applyDictsToTemplate(tpl, cfg)

	allow := normalizePluginRefs(cfg.Plugins)
	if len(allow) == 0 {
		return dictApplied, dictWarnings
	}
	allowSet := make(map[string]struct{}, len(allow))
	for _, ref := range allow {
		allowSet[ref] = struct{}{}
	}

	restricted := make([]string, 0, len(phasesOf(tpl)))
	for _, phase := range phasesOf(tpl) {
		if phase.List == nil || len(*phase.List) == 0 {
			continue
		}
		matched := false
		for _, hash := range *phase.List {
			if _, ok := allowSet[strings.TrimSpace(hash)]; ok {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		keep := make([]string, 0, len(*phase.List))
		for _, hash := range *phase.List {
			value := strings.TrimSpace(hash)
			if _, ok := allowSet[value]; ok {
				keep = append(keep, value)
				continue
			}
			if phase.Params != nil {
				delete(phase.Params, value)
			}
		}
		*phase.List = keep
		restricted = append(restricted, phase.Name)
	}

	if len(restricted) == 0 {
		warnings := append([]models.PolicyWarning{{
			Field:   "plugins",
			Code:    CodePluginsNotApplied,
			Message: fmt.Sprintf("策略里的 %d 个插件在该模板的分阶段清单里一个都没命中，插件清单保持不变", len(allow)),
		}}, dictWarnings...)
		return dictApplied, warnings
	}
	// 命中即算生效；removed=0 表示清单本来就是策略要的样子（幂等，不重复告警）。
	return append([]string{"plugins"}, dictApplied...), dictWarnings
}

// dictKinds 策略可重定向的字典类别（顺序固定，便于稳定产出提示）
var dictKinds = []string{"dir", "subdomain", "brute", "port"}

// dictPlaceholderRe 匹配模板参数里的字典占位符 {dict.<类别>.<名称>}。
// 节点侧按这个占位符在扫描时读字典内容，所以只改「名称」就能换字典，
// 不需要为每个插件猜端口/字典的 CLI 旗标（那会臆造参数）。
var dictPlaceholderRe = regexp.MustCompile(`\{dict\.([a-zA-Z]+)\.([^}]*)\}`)

// applyDictsToTemplate 把策略里指定的字典名写进模板参数占位符。
//
// 语义：只重定向模板里**已经存在**的同类占位符，例如模板里原本是
// {dict.port.default}，策略 ports=<自定义字典> 就变成 {dict.port.<自定义字典>}。
// 模板里没有该类占位符时不臆造参数，只产出一条说明性 warning。
func applyDictsToTemplate(tpl *models.ScanTemplate, cfg models.ScanPolicyConfig) ([]string, []models.PolicyWarning) {
	if tpl == nil || cfg.Dicts == nil {
		return nil, nil
	}
	want := map[string]string{}
	add := func(kind string, v *string) {
		if v == nil {
			return
		}
		name := strings.TrimSpace(*v)
		if name == "" {
			return
		}
		want[kind] = name
	}
	add("dir", cfg.Dicts.Dir)
	add("subdomain", cfg.Dicts.Subdomain)
	add("brute", cfg.Dicts.Brute)
	add("port", cfg.Dicts.Port)
	if len(want) == 0 {
		return nil, nil
	}

	hit := map[string]bool{}
	for _, phase := range phasesOf(tpl) {
		if phase.Params == nil {
			continue
		}
		for hash, raw := range phase.Params {
			if !strings.Contains(raw, "{dict.") {
				continue
			}
			replaced := dictPlaceholderRe.ReplaceAllStringFunc(raw, func(m string) string {
				sub := dictPlaceholderRe.FindStringSubmatch(m)
				if len(sub) != 3 {
					return m
				}
				kind := sub[1]
				name, ok := want[kind]
				if !ok {
					return m
				}
				hit[kind] = true
				return "{dict." + kind + "." + name + "}"
			})
			if replaced != raw {
				phase.Params[hash] = replaced
			}
		}
	}

	applied := make([]string, 0, len(hit))
	warnings := make([]models.PolicyWarning, 0, len(want))
	for _, kind := range dictKinds {
		name, ok := want[kind]
		if !ok {
			continue
		}
		if hit[kind] {
			applied = append(applied, "dicts."+kind)
			continue
		}
		warnings = append(warnings, models.PolicyWarning{
			Field: "dicts." + kind,
			Code:  CodeDictsUnsupported,
			Message: fmt.Sprintf(
				"策略指定的 %s 字典「%s」没有生效：该模板参数里没有 {dict.%s.<名称>} 占位符。在模板参数里用占位符引用字典后，改策略即可切换字典",
				kind, name, kind),
		})
	}
	return applied, warnings
}

// UnsupportedWarnings 说明「策略里有、但扫描载荷接不住」的字段。
//
// 这些 warning 不阻断任务创建，只保证「策略页填了并发/速率/超时/字典/options」
// 不会被误解成已经在执行期生效 —— 宁可显式说明，也不往模板参数里塞臆造的旗标。
func UnsupportedWarnings(cfg models.ScanPolicyConfig) []models.PolicyWarning {
	warnings := make([]models.PolicyWarning, 0, 5)
	if cfg.ThreadNum != nil {
		warnings = append(warnings, models.PolicyWarning{
			Field:   "threadNum",
			Code:    CodeExecLimitsUnsupported,
			Message: fmt.Sprintf("扫描模板没有全局并发字段（参数是每个插件自己的 CLI 字符串），并发 %d 不会写进本次任务", *cfg.ThreadNum),
		})
	}
	if cfg.RateLimit != nil {
		warnings = append(warnings, models.PolicyWarning{
			Field:   "rateLimit",
			Code:    CodeExecLimitsUnsupported,
			Message: fmt.Sprintf("扫描模板没有全局速率字段，速率 %d（0=不限速）不会写进本次任务", *cfg.RateLimit),
		})
	}
	if cfg.TimeoutSeconds != nil {
		warnings = append(warnings, models.PolicyWarning{
			Field:   "timeoutSeconds",
			Code:    CodeExecLimitsUnsupported,
			Message: fmt.Sprintf("扫描模板没有全局超时字段，超时 %d 秒不会写进本次任务", *cfg.TimeoutSeconds),
		})
	}
	// dicts 不再在这里告警：它已经能在模板参数里生效（见 applyDictsToTemplate），
	// 是否命中占位符要拿到模板才知道，由那一侧给出「生效/未命中」的结论。
	if len(cfg.Options) > 0 {
		warnings = append(warnings, models.PolicyWarning{
			Field:   "options",
			Code:    CodeOptionsUnsupported,
			Message: fmt.Sprintf("扫描模板没有自由键值槽位，策略里的 %d 条 options 仅作记录", len(cfg.Options)),
		})
	}
	return warnings
}

// normalizePluginRefs 去空白、去空值、去重保序（与 policy.NormalizeConfig 同口径）。
func normalizePluginRefs(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, raw := range in {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// setDictRefs 返回策略里实际设置的字典引用（固定顺序：dir / subdomain / brute）。
func setDictRefs(dicts *models.ScanPolicyDicts) []string {
	if dicts == nil {
		return nil
	}
	refs := make([]string, 0, 3)
	for _, item := range []struct {
		name  string
		value *string
	}{
		{"dir", dicts.Dir},
		{"subdomain", dicts.Subdomain},
		{"brute", dicts.Brute},
	} {
		if item.value == nil {
			continue
		}
		if value := strings.TrimSpace(*item.value); value != "" {
			refs = append(refs, item.name+"="+value)
		}
	}
	return refs
}
