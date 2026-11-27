package policy

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// 校验规则（test / create / update 共用同一套纯函数）：
//  1. ValidateConfigShape —— 纯函数：归一化 + 范围/格式/去重，不碰 DB，可直接单测；
//  2. ValidateConfigRefs  —— 引用存在性（模板、插件），依赖 RefChecker，集成测试覆盖。
//
// 范围口径：并发 1-200、速率 0-1000（0=不限速）、超时 10-3600 秒。

var objectIDPattern = regexp.MustCompile("^[0-9a-fA-F]{24}$")

// RefChecker 存在性校验依赖的最小只读接口（生产实现是 repositories/policy）
type RefChecker interface {
	// MissingPlugins 返回查不到（hash 与 name 均未命中）的插件引用
	MissingPlugins(ctx context.Context, plugins []string) ([]string, error)
	// TemplateExists 扫描模板是否存在
	TemplateExists(ctx context.Context, templateID string) (bool, error)
}

// NormalizeConfig 归一化：去空白、丢空值、插件去重、空 options/dicts 折叠为 nil。
func NormalizeConfig(cfg models.ScanPolicyConfig) models.ScanPolicyConfig {
	out := cloneConfig(cfg)
	out.TemplateID = strings.TrimSpace(out.TemplateID)
	out.Plugins = nil
	if plugins := dedupeTrimmed(cfg.Plugins); len(plugins) > 0 {
		out.Plugins = plugins
	}

	if out.Dicts != nil {
		out.Dicts.Dir = normalizeDictRef(out.Dicts.Dir)
		out.Dicts.Subdomain = normalizeDictRef(out.Dicts.Subdomain)
		out.Dicts.Brute = normalizeDictRef(out.Dicts.Brute)
		out.Dicts.Port = normalizeDictRef(out.Dicts.Port)
		if out.Dicts.Dir == nil && out.Dicts.Subdomain == nil && out.Dicts.Brute == nil && out.Dicts.Port == nil {
			out.Dicts = nil
		}
	}

	out.Options = nil
	if len(cfg.Options) > 0 {
		opts := make(map[string]string, len(cfg.Options))
		for k, v := range cfg.Options {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			opts[k] = strings.TrimSpace(v)
		}
		if len(opts) > 0 {
			out.Options = opts
		}
	}
	return out
}

func normalizeDictRef(p *string) *string {
	if p == nil {
		return nil
	}
	v := strings.TrimSpace(*p)
	return &v
}

// ValidateConfigShape 纯函数校验（不访问数据库），返回归一化后的 config 与问题列表。
func ValidateConfigShape(cfg models.ScanPolicyConfig) models.PolicyValidation {
	norm := NormalizeConfig(cfg)
	result := models.PolicyValidation{
		Valid:    true,
		Config:   norm,
		Errors:   make([]models.PolicyIssue, 0),
		Warnings: make([]models.PolicyIssue, 0),
	}
	addErr := func(field, code, message string) {
		result.Errors = append(result.Errors, models.PolicyIssue{Field: field, Code: code, Message: message})
	}
	addWarn := func(field, code, message string) {
		result.Warnings = append(result.Warnings, models.PolicyIssue{Field: field, Code: code, Message: message})
	}

	// 模板：只校验形状，存在性交给 ValidateConfigRefs
	if norm.TemplateID != "" && !objectIDPattern.MatchString(norm.TemplateID) {
		addErr("templateId", "template_invalid", "模板 ID 必须是 24 位十六进制 ObjectID")
	}

	// 插件：去重/去空给 warning，数量上限给 error
	rawNonEmpty := 0
	for _, p := range cfg.Plugins {
		if strings.TrimSpace(p) != "" {
			rawNonEmpty++
		}
	}
	if rawNonEmpty != len(norm.Plugins) {
		addWarn("plugins", "plugin_deduped",
			fmt.Sprintf("插件引用已去重/去空：%d → %d", rawNonEmpty, len(norm.Plugins)))
	}
	if len(norm.Plugins) > models.ScanPolicyMaxPlugins {
		addErr("plugins", "plugins_too_many",
			fmt.Sprintf("插件引用不能超过 %d 个", models.ScanPolicyMaxPlugins))
	}

	// 标量范围
	if norm.ThreadNum != nil && (*norm.ThreadNum < models.ScanPolicyMinThreadNum || *norm.ThreadNum > models.ScanPolicyMaxThreadNum) {
		addErr("threadNum", "thread_num_out_of_range",
			fmt.Sprintf("并发必须在 %d-%d 之间（当前 %d）", models.ScanPolicyMinThreadNum, models.ScanPolicyMaxThreadNum, *norm.ThreadNum))
	}
	if norm.RateLimit != nil && (*norm.RateLimit < models.ScanPolicyMinRateLimit || *norm.RateLimit > models.ScanPolicyMaxRateLimit) {
		addErr("rateLimit", "rate_limit_out_of_range",
			fmt.Sprintf("速率必须在 %d-%d 之间，0 表示不限速（当前 %d）", models.ScanPolicyMinRateLimit, models.ScanPolicyMaxRateLimit, *norm.RateLimit))
	}
	if norm.TimeoutSeconds != nil && (*norm.TimeoutSeconds < models.ScanPolicyMinTimeoutSeconds || *norm.TimeoutSeconds > models.ScanPolicyMaxTimeoutSeconds) {
		addErr("timeoutSeconds", "timeout_out_of_range",
			fmt.Sprintf("超时必须 %d-%d 秒（当前 %d）", models.ScanPolicyMinTimeoutSeconds, models.ScanPolicyMaxTimeoutSeconds, *norm.TimeoutSeconds))
	}

	// 字典引用：只允许字典名，禁止路径分隔符/控制字符（引用最终会拼进节点参数）
	if norm.Dicts != nil {
		refs := []struct {
			field string
			value *string
		}{
			{"dicts.dir", norm.Dicts.Dir},
			{"dicts.subdomain", norm.Dicts.Subdomain},
			{"dicts.brute", norm.Dicts.Brute},
		}
		for _, ref := range refs {
			if ref.value == nil || *ref.value == "" {
				continue
			}
			v := *ref.value
			if utf8.RuneCountInString(v) > models.ScanPolicyMaxDictRefRunes {
				addErr(ref.field, "dict_ref_too_long",
					fmt.Sprintf("字典引用不能超过 %d 个字符", models.ScanPolicyMaxDictRefRunes))
				continue
			}
			if strings.ContainsAny(v, "/\\") || strings.Contains(v, "..") || strings.ContainsAny(v, "\x00\n\r\t") {
				addErr(ref.field, "dict_ref_invalid", "字典引用只能是字典名，不能包含路径分隔符、.. 或控制字符")
			}
		}
	}

	// options：条目数 / key 长度 / value 长度；空 key 归一化时被丢弃，这里给 warning
	if len(cfg.Options) != len(norm.Options) {
		emptyKeys := 0
		for k := range cfg.Options {
			if strings.TrimSpace(k) == "" {
				emptyKeys++
			}
		}
		if emptyKeys > 0 {
			addWarn("options", "option_key_dropped", fmt.Sprintf("已丢弃 %d 个空 key", emptyKeys))
		}
	}
	if len(norm.Options) > models.ScanPolicyMaxOptions {
		addErr("options", "options_too_many", fmt.Sprintf("options 不能超过 %d 条", models.ScanPolicyMaxOptions))
	}
	if len(norm.Options) > 0 {
		keys := make([]string, 0, len(norm.Options))
		for k := range norm.Options {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if utf8.RuneCountInString(k) > models.ScanPolicyMaxOptionKeyRunes {
				addErr("options."+k, "option_key_too_long",
					fmt.Sprintf("option key 不能超过 %d 个字符", models.ScanPolicyMaxOptionKeyRunes))
			}
			if utf8.RuneCountInString(norm.Options[k]) > models.ScanPolicyMaxOptionValueRunes {
				addErr("options."+k, "option_value_too_long",
					fmt.Sprintf("option value 不能超过 %d 个字符", models.ScanPolicyMaxOptionValueRunes))
			}
		}
	}

	result.Valid = len(result.Errors) == 0
	return result
}

// ValidateConfigRefs 引用存在性校验：模板必须存在；插件按 hash 或 name 至少命中一个。
// 入参应为 ValidateConfigShape 归一化后的 config（templateId 已是合法 ObjectID）。
func ValidateConfigRefs(ctx context.Context, cfg models.ScanPolicyConfig, refs RefChecker) ([]models.PolicyIssue, error) {
	issues := make([]models.PolicyIssue, 0)
	if refs == nil {
		return issues, nil
	}
	if cfg.TemplateID != "" {
		exists, err := refs.TemplateExists(ctx, cfg.TemplateID)
		if err != nil {
			return nil, err
		}
		if !exists {
			issues = append(issues, models.PolicyIssue{
				Field:   "templateId",
				Code:    "template_not_found",
				Message: fmt.Sprintf("扫描模板 %s 不存在", cfg.TemplateID),
			})
		}
	}
	if len(cfg.Plugins) > 0 {
		missing, err := refs.MissingPlugins(ctx, cfg.Plugins)
		if err != nil {
			return nil, err
		}
		for _, name := range missing {
			issues = append(issues, models.PolicyIssue{
				Field:   "plugins",
				Code:    "plugin_not_found",
				Message: fmt.Sprintf("插件 %s 不存在（hash 与 name 均未命中）", name),
			})
		}
	}
	return issues, nil
}
