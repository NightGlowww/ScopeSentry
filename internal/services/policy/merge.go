package policy

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/constants"
	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// 策略合并（apply）与模板基线抽取的纯函数集合。
//
// 合并语义（对应「策略字段覆盖模板默认，nil 不覆盖」）：
//   - 标量用指针区分「未设置」与「显式 0」：threadNum / rateLimit / timeoutSeconds 都是 *int；
//   - plugins 只有非空时才覆盖（缺省 / 空数组都视为「不约束」）；
//   - dicts 每个子字段独立判断：nil 不动，非 nil（含空串 = 显式清空）覆盖；
//   - options 按 key 合并：策略里的 key 覆盖同名模板 key，模板其余 key 保留。
//
// 冲突（两边都设置且值不同）不阻断合并，只产出 warning，供前端提示「策略改掉了模板默认」。

// MergeConfig 把策略参数合并到模板默认值上（纯函数：不改入参、不读写库）
func MergeConfig(policyCfg, base models.ScanPolicyConfig) models.PolicyMergeResult {
	merged := cloneConfig(base)
	warnings := make([]models.PolicyWarning, 0)
	applied := make([]string, 0)

	warn := func(field, code, format string, args ...interface{}) {
		warnings = append(warnings, models.PolicyWarning{
			Field:   field,
			Code:    code,
			Message: fmt.Sprintf(format, args...),
		})
	}

	// 1) 模板
	if tid := strings.TrimSpace(policyCfg.TemplateID); tid != "" {
		if baseID := strings.TrimSpace(merged.TemplateID); baseID != "" && baseID != tid {
			warn("templateId", "template_override", "策略模板 %s 覆盖模板默认 %s", tid, baseID)
		}
		merged.TemplateID = tid
		applied = append(applied, "templateId")
	}

	// 2) 插件清单（集合比较，顺序差异不算冲突）
	if len(policyCfg.Plugins) > 0 {
		plugins := dedupeTrimmed(policyCfg.Plugins)
		if len(plugins) > 0 {
			if len(merged.Plugins) > 0 && !sameStringSet(merged.Plugins, plugins) {
				warn("plugins", "plugins_override", "策略插件（%d 个）覆盖模板默认（%d 个），新增 %d 个、移除 %d 个",
					len(plugins), len(merged.Plugins), countMissing(plugins, merged.Plugins), countMissing(merged.Plugins, plugins))
			}
			merged.Plugins = plugins
			applied = append(applied, "plugins")
		}
	}

	// 3) 标量：并发 / 速率 / 超时
	applyInt := func(field, code string, src, dst **int) {
		if src == nil || *src == nil {
			return
		}
		val := **src
		if *dst != nil && **dst != val {
			warn(field, code, "策略 %s=%d 覆盖模板默认 %d", field, val, **dst)
		}
		v := val
		*dst = &v
		applied = append(applied, field)
	}
	applyInt("threadNum", "thread_num_override", &policyCfg.ThreadNum, &merged.ThreadNum)
	applyInt("rateLimit", "rate_limit_override", &policyCfg.RateLimit, &merged.RateLimit)
	applyInt("timeoutSeconds", "timeout_override", &policyCfg.TimeoutSeconds, &merged.TimeoutSeconds)

	// 4) 字典引用（逐项覆盖）；三项都未设置时不改变 merged 的形状
	hasDictOverride := policyCfg.Dicts != nil &&
		(policyCfg.Dicts.Dir != nil || policyCfg.Dicts.Subdomain != nil ||
			policyCfg.Dicts.Brute != nil || policyCfg.Dicts.Port != nil)
	if hasDictOverride {
		if merged.Dicts == nil {
			merged.Dicts = &models.ScanPolicyDicts{}
		}
		applyDict := func(field string, src, dst **string) {
			if src == nil || *src == nil {
				return
			}
			val := strings.TrimSpace(**src)
			if *dst != nil {
				old := strings.TrimSpace(**dst)
				if old != val {
					warn(field, "dict_override", "策略 %s=%q 覆盖模板默认 %q", field, val, old)
				}
			}
			v := val
			*dst = &v
			applied = append(applied, field)
		}
		applyDict("dicts.dir", &policyCfg.Dicts.Dir, &merged.Dicts.Dir)
		applyDict("dicts.subdomain", &policyCfg.Dicts.Subdomain, &merged.Dicts.Subdomain)
		applyDict("dicts.brute", &policyCfg.Dicts.Brute, &merged.Dicts.Brute)
		applyDict("dicts.port", &policyCfg.Dicts.Port, &merged.Dicts.Port)
	}

	// 5) options：按 key 合并（key 排序保证 warning 顺序稳定）
	if len(policyCfg.Options) > 0 {
		keys := make([]string, 0, len(policyCfg.Options))
		for k := range policyCfg.Options {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		touched := false
		for _, rawKey := range keys {
			key := strings.TrimSpace(rawKey)
			if key == "" {
				continue
			}
			val := strings.TrimSpace(policyCfg.Options[rawKey])
			if merged.Options == nil {
				merged.Options = make(map[string]string, len(policyCfg.Options))
			}
			if old, ok := merged.Options[key]; ok && strings.TrimSpace(old) != val {
				warn("options."+key, "option_override", "策略选项 %s=%q 覆盖模板默认 %q", key, val, old)
			}
			merged.Options[key] = val
			touched = true
		}
		if touched {
			applied = append(applied, "options")
		}
	}

	return models.PolicyMergeResult{
		Config:        merged,
		Warnings:      warnings,
		AppliedFields: applied,
	}
}

// BaselineFromTemplate 从扫描模板的分阶段插件清单生成 baseline。
//
// 只做模板里真实存在的两件事：
//   - templateId = 模板 ID；
//   - plugins = 各阶段插件 hash 的并集（按 PLUGINSMODULES 顺序去重）。
//
// 模板的并发/速率/超时是每个插件自己的 CLI 参数串，没有全局值可抽，故不臆造默认值。
func BaselineFromTemplate(templateID string, phases models.ScanTemplatePhases) models.ScanPolicyConfig {
	byModule := map[string][]string{
		"TargetHandler":       phases.TargetHandler,
		"SubdomainScan":       phases.SubdomainScan,
		"SubdomainSecurity":   phases.SubdomainSecurity,
		"PortScanPreparation": phases.PortScanPreparation,
		"PortScan":            phases.PortScan,
		"PortFingerprint":     phases.PortFingerprint,
		"AssetMapping":        phases.AssetMapping,
		"AssetHandle":         phases.AssetHandle,
		"URLScan":             phases.URLScan,
		"WebCrawler":          phases.WebCrawler,
		"URLSecurity":         phases.URLSecurity,
		"DirScan":             phases.DirScan,
		"VulnerabilityScan":   phases.VulnerabilityScan,
		"PassiveScan":         phases.PassiveScan,
	}

	cfg := models.ScanPolicyConfig{TemplateID: strings.TrimSpace(templateID)}
	plugins := make([]string, 0, 16)
	seen := make(map[string]struct{}, 16)
	for _, module := range constants.PLUGINSMODULES {
		for _, p := range byModule[module] {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			plugins = append(plugins, p)
		}
	}
	if len(plugins) > 0 {
		cfg.Plugins = plugins
	}
	return cfg
}

// cloneConfig 深拷贝（合并前必须做，避免把入参的 map/slice/指针改脏）
func cloneConfig(c models.ScanPolicyConfig) models.ScanPolicyConfig {
	out := c
	if c.Plugins != nil {
		out.Plugins = append([]string(nil), c.Plugins...)
	}
	out.ThreadNum = cloneIntPtr(c.ThreadNum)
	out.RateLimit = cloneIntPtr(c.RateLimit)
	out.TimeoutSeconds = cloneIntPtr(c.TimeoutSeconds)
	if c.Dicts != nil {
		d := *c.Dicts
		d.Dir = cloneStringPtr(c.Dicts.Dir)
		d.Subdomain = cloneStringPtr(c.Dicts.Subdomain)
		d.Brute = cloneStringPtr(c.Dicts.Brute)
		d.Port = cloneStringPtr(c.Dicts.Port)
		out.Dicts = &d
	}
	if c.Options != nil {
		opts := make(map[string]string, len(c.Options))
		for k, v := range c.Options {
			opts[k] = v
		}
		out.Options = opts
	}
	return out
}

func cloneIntPtr(p *int) *int {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func cloneStringPtr(p *string) *string {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func dedupeTrimmed(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func sameStringSet(a, b []string) bool {
	setA := make(map[string]struct{}, len(a))
	for _, v := range a {
		setA[strings.TrimSpace(v)] = struct{}{}
	}
	setB := make(map[string]struct{}, len(b))
	for _, v := range b {
		setB[strings.TrimSpace(v)] = struct{}{}
	}
	if len(setA) != len(setB) {
		return false
	}
	for v := range setA {
		if _, ok := setB[v]; !ok {
			return false
		}
	}
	return true
}

// countMissing 返回 in 中不在 ref 里的元素个数（用于冲突提示里的「新增/移除」计数）
func countMissing(in, ref []string) int {
	set := make(map[string]struct{}, len(ref))
	for _, v := range ref {
		set[strings.TrimSpace(v)] = struct{}{}
	}
	missing := 0
	for _, v := range in {
		if _, ok := set[strings.TrimSpace(v)]; !ok {
			missing++
		}
	}
	return missing
}
