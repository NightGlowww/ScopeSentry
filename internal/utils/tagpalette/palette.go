// Package tagpalette 定义标签颜色的合法取值（issue #217）。
//
// 前端默认按标签名稳定取色；这里管的是「用户给某个标签指定固定颜色」的覆盖值。
// 合法值有两种：Element Plus 的语义色名，或 #rgb / #rrggbb——语义色随主题适配，
// 自定义 hex 由用户自己负责对比度。
package tagpalette

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// MaxTagLength 标签最大长度（按字符数）
const MaxTagLength = 64

// SemanticColors Element Plus 的语义色名
var SemanticColors = []string{"primary", "success", "info", "warning", "danger"}

var hexColor = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// ErrInvalidTag 标签为空或超长
var ErrInvalidTag = fmt.Errorf("tag must be 1-%d characters", MaxTagLength)

// ErrInvalidColor 颜色既不是语义色名也不是 #rgb/#rrggbb
var ErrInvalidColor = fmt.Errorf("color must be one of %s or #rgb/#rrggbb", strings.Join(SemanticColors, ", "))

// NormalizeTag 去首尾空白并校验长度。
func NormalizeTag(tag string) (string, error) {
	tag = strings.TrimSpace(tag)
	if tag == "" || utf8.RuneCountInString(tag) > MaxTagLength {
		return "", ErrInvalidTag
	}
	return tag, nil
}

// NormalizeColor 去空白并校验颜色。空字符串表示「删除覆盖」，由调用方处理。
func NormalizeColor(color string) (string, error) {
	color = strings.TrimSpace(color)
	if color == "" {
		return "", nil
	}
	for _, name := range SemanticColors {
		if strings.EqualFold(color, name) {
			return name, nil
		}
	}
	if hexColor.MatchString(color) {
		return color, nil
	}
	return "", ErrInvalidColor
}
