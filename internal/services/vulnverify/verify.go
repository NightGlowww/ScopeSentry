// Package vulnverify 对已落库的漏洞做「证据复验」：把模板 matcher 对着保存下来的
// 请求/响应重算一遍。算不通的（DSL、二进制等）一律不动，只清「算得通且明确不成立」的，
// 避免把真实漏洞误删。
package vulnverify

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Verdict 复验结论。
type Verdict struct {
	// Verified 证据成立。
	Verified bool
	// Evaluable 本次是否真的算出了结论；false 表示模板用了不支持的 matcher，
	// 此时 Verified 无意义，调用方必须保留记录。
	Evaluable bool
	// Weak 表示模板的判据只有状态码（没有任何正文/头部判据），这种「成立」不构成证据，
	// 只能标待复核，不能当已确认。2026-11-24 用友那批误报的同源问题。
	Weak   bool
	Reason string
}

type httpBlock struct {
	MatchersCondition string       `yaml:"matchers-condition"`
	Matchers          []rawMatcher `yaml:"matchers"`
}

type template struct {
	HTTP     []httpBlock `yaml:"http"`
	Requests []httpBlock `yaml:"requests"`
}

// blocks 合并 http: 与 requests: 两种写法（nuclei 两者等价）。
func (t template) blocks() []httpBlock {
	out := make([]httpBlock, 0, len(t.HTTP)+len(t.Requests))
	out = append(out, t.HTTP...)
	out = append(out, t.Requests...)
	return out
}

type rawMatcher struct {
	Type      string   `yaml:"type"`
	Part      string   `yaml:"part"`
	Words     []string `yaml:"words"`
	Regex     []string `yaml:"regex"`
	Status    []int    `yaml:"status"`
	DSL       []string `yaml:"dsl"`
	Condition string   `yaml:"condition"`
}

// Response 是拆开的 HTTP 响应。
type Response struct {
	Status int
	Header string
	Body   string
}

// ParseResponse 从原始响应文本里取状态码、头、正文。
func ParseResponse(raw string) Response {
	r := Response{}
	s := strings.ReplaceAll(raw, "\r\n", "\n")
	if i := strings.Index(s, "\n\n"); i >= 0 {
		r.Header, r.Body = s[:i], s[i+2:]
	} else {
		r.Header = s
	}
	lines := strings.SplitN(r.Header, "\n", 2)
	if f := strings.Fields(lines[0]); len(f) >= 2 && strings.HasPrefix(f[0], "HTTP/") {
		if code, err := strconv.Atoi(f[1]); err == nil {
			r.Status = code
		}
	}
	return r
}

// Check 用模板内容复验一条发现。
func Check(templateContent, rawResponse string) Verdict {
	var t template
	if err := yaml.Unmarshal([]byte(templateContent), &t); err != nil {
		return Verdict{Evaluable: false, Reason: "template parse: " + err.Error()}
	}
	blocks := t.blocks()
	if len(blocks) == 0 {
		return Verdict{Evaluable: false, Reason: "template has no http blocks"}
	}
	r := ParseResponse(rawResponse)
	if r.Status == 0 {
		return Verdict{Evaluable: true, Verified: false, Reason: "response is not HTTP"}
	}
	// 有任何一个请求块整体成立，就算证据成立（nuclei 命中其中一路就会报）。
	weak := true
	for _, blk := range blocks {
		if blockHasContentMatcher(blk.Matchers) {
			weak = false
		}
	}

	for _, block := range blocks {
		ok, evaluable, reason := evalBlock(block.MatchersCondition, block.Matchers, r)
		if !evaluable {
			return Verdict{Evaluable: false, Reason: reason}
		}
		if ok {
			return Verdict{Evaluable: true, Verified: true, Weak: weak, Reason: "matcher satisfied"}
		}
	}
	return Verdict{Evaluable: true, Verified: false, Weak: weak, Reason: "no http block matcher satisfied"}
}

// blockHasContentMatcher 判断这个请求块里是否有「内容级」判据（word/regex，
// 或引用了 body/header 的 dsl）。只有状态码的块不算。
func blockHasContentMatcher(ms []rawMatcher) bool {
	for _, m := range ms {
		switch m.Type {
		case "word":
			if len(m.Words) > 0 {
				return true
			}
		case "regex":
			if len(m.Regex) > 0 {
				return true
			}
		case "dsl":
			for _, e := range m.DSL {
				if strings.Contains(e, "body") || strings.Contains(e, "header") {
					return true
				}
			}
		}
	}
	return false
}

func evalBlock(cond string, ms []rawMatcher, r Response) (bool, bool, string) {
	if len(ms) == 0 {
		return false, false, "block without matchers"
	}
	all := strings.EqualFold(cond, "and")
	passed := 0
	for _, m := range ms {
		ok, evaluable, reason := evalMatcher(m, r)
		if !evaluable {
			return false, false, reason
		}
		if ok {
			passed++
		}
	}
	if all {
		return passed == len(ms), true, ""
	}
	return passed > 0, true, ""
}

func evalMatcher(m rawMatcher, r Response) (bool, bool, string) {
	switch m.Type {
	case "status":
		if len(m.Status) == 0 {
			return false, false, "status matcher without values"
		}
		for _, s := range m.Status {
			if s == r.Status {
				return true, true, ""
			}
		}
		return false, true, ""
	case "word":
		subject := partOf(m.Part, r)
		for _, w := range m.Words {
			if strings.Contains(subject, w) {
				return true, true, ""
			}
		}
		return false, true, ""
	case "regex":
		subject := partOf(m.Part, r)
		for _, p := range m.Regex {
			re, err := regexp.Compile(strings.ReplaceAll(p, "(?i)", "(?i)"))
			if err != nil {
				return false, false, "regex compile: " + err.Error()
			}
			if re.MatchString(subject) {
				return true, true, ""
			}
		}
		return false, true, ""
	case "dsl":
		if len(m.DSL) == 0 {
			return false, false, "dsl matcher without expressions"
		}
		anyExpr := strings.EqualFold(m.Condition, "or")
		passed := 0
		for _, e := range m.DSL {
			v, ok := EvalDSL(e, r)
			if !ok {
				return false, false, "dsl not evaluable: " + e
			}
			if v {
				passed++
			}
		}
		if anyExpr {
			return passed > 0, true, ""
		}
		return passed == len(m.DSL), true, ""
	default:
		return false, false, fmt.Sprintf("unsupported matcher type %q", m.Type)
	}
}

func partOf(part string, r Response) string {
	switch strings.ToLower(strings.TrimSpace(part)) {
	case "header", "headers", "all_headers":
		return r.Header
	case "body":
		return r.Body
	case "", "all":
		return r.Header + "\n\n" + r.Body
	default:
		return r.Header + "\n\n" + r.Body
	}
}
