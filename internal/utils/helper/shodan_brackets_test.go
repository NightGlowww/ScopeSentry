package helper

import (
	"testing"
	"time"
)

// 与 FOFA 同源缺陷：Shodan 括号查询曾让 for 循环永不前进。现在透传，必须快速返回。
func TestParseShodanQueryTerminates(t *testing.T) {
	for _, q := range []string{"(port:80 || port:443) country:CN", "((((port:80))))", "(port:80"} {
		done := make(chan string, 1)
		go func(in string) { done <- parseShodanQueryWithBrackets(in) }(q)
		select {
		case got := <-done:
			if got != q {
				t.Fatalf("bracket query should pass through: in=%q got=%q", q, got)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("HANG: parseShodanQueryWithBrackets(%q)", q)
		}
	}
}
