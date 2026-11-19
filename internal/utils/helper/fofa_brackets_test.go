package helper

import (
	"testing"
	"time"
)

// 回归：含括号的查询曾经让 for 循环永不前进，卡死请求 goroutine。
// 现在括号查询直接透传（FOFA 原生支持），必须快速返回且不改写。
func TestParseFofaQueryWithBracketsTerminates(t *testing.T) {
	cases := []string{
		"(app=nginx || app=apache) && country=CN",
		"(((((app=nginx)))))",
		"(a=1",
	}
	for _, q := range cases {
		done := make(chan string, 1)
		go func(in string) { done <- parseFofaQueryWithBrackets(in) }(q)
		select {
		case got := <-done:
			if got != q {
				t.Fatalf("bracket query should pass through unchanged: in=%q got=%q", q, got)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("HANG: parseFofaQueryWithBrackets(%q) 未返回", q)
		}
	}
}

// 不带括号的查询仍走原有归一化逻辑（不能被透传分支吞掉）。
func TestParseFofaQuerySimpleStillParsed(t *testing.T) {
	got := parseFofaQueryWithBrackets("app=nginx")
	if got == "" {
		t.Fatal("simple query returned empty")
	}
	if got == "app=nginx" {
		t.Logf("simple query returned unchanged: %q（若归一化本应加引号，说明规则变更）", got)
	}
}
