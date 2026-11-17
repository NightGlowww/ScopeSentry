package github

import (
	"regexp"
	"strings"
)

// escapeRegex 把用户输入转成安全的正则字面量（keyword 过滤统一走这里）
func escapeRegex(s string) string {
	return regexp.QuoteMeta(strings.TrimSpace(s))
}
