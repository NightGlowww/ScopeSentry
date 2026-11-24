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
	Reason    string
}

type template struct {
	HTTP []struct {
		MatchersCondition string       `yaml:"matchers-condition"`
		Matchers          []rawMatcher `yaml:"matchers"`
	} `yaml:"http"`
}

type rawMatcher struct {
	Type      string   `yaml:"type"`
	Part      string   `yaml:"part"`
	Words     []string `yaml:"words"`
	Regex     []string `yaml:"regex"`
	Status    []int    `yaml:"status"`
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
	if len(t.HTTP) == 0 {
		return Verdict{Evaluable: false, Reason: "template has no http blocks"}
	}
	r := ParseResponse(rawResponse)
	if r.Status == 0 {
		return Verdict{Evaluable: true, Verified: false, Reason: "response is not HTTP"}
	}
	// 有任何一个请求块整体成立，就算证据成立（nuclei 命中其中一路就会报）。
	for _, block := range t.HTTP {
		ok, evaluable, reason := evalBlock(block.MatchersCondition, block.Matchers, r)
		if !evaluable {
			return Verdict{Evaluable: false, Reason: reason}
		}
		if ok {
			return Verdict{Evaluable: true, Verified: true, Reason: "matcher satisfied"}
		}
	}
	return Verdict{Evaluable: true, Verified: false, Reason: "no http block matcher satisfied"}
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
