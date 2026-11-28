package maintenance

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// 截断必须切在 UTF-8 字符边界：BSON 字符串要求合法 UTF-8，
// 半截字符会让写入失败或渲染出乱码（2026-11-28 实测截断后长度对不上）。
func TestTruncateUTF8(t *testing.T) {
	// 全是 3 字节汉字：limit 落在字符中间时必须回退
	s := strings.Repeat("测", 10) // 30 字节
	got := truncateUTF8(s, 10)
	if len(got) > 10 {
		t.Fatalf("超过上限: %d", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatalf("截断结果不是合法 UTF-8: %q", got)
	}
	if len(got) != 9 {
		t.Fatalf("应回退到 9 字节（3 个汉字），实际 %d", len(got))
	}

	// 边界正好时不回退
	if got := truncateUTF8("abcdef", 3); got != "abc" {
		t.Fatalf("ASCII 截断错误: %q", got)
	}
	// 小于上限原样返回
	if got := truncateUTF8("abc", 10); got != "abc" {
		t.Fatalf("未超限不该截断: %q", got)
	}
	// 4 字节字符（emoji）被切到时也要回退到边界
	emoji := "\U0001F600\U0001F600" // 8 字节
	if got := truncateUTF8(emoji, 6); !utf8.ValidString(got) || len(got) != 4 {
		t.Fatalf("4 字节字符截断错误: %q len=%d", got, len(got))
	}
}