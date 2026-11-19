package helper

import (
	"strings"
	"testing"
	"time"
)

// 用户输入直接进入解析器：极深嵌套/超长表达式必须快速返回，不能挂起或爆栈。
func TestSearchToMongoDBDeepInputTerminates(t *testing.T) {
	cases := map[string]string{
		"2000 层括号":  strings.Repeat("(", 2000) + "domain=a" + strings.Repeat(")", 2000),
		"2000 个 &&": strings.Join(make([]string, 2000), "domain=a && ") + "domain=b",
	}
	for name, expr := range cases {
		done := make(chan struct{}, 1)
		go func(e string) { _, _ = SearchToMongoDB(e, map[string]string{"domain": "host"}); done <- struct{}{} }(expr)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: SearchToMongoDB 未在 5s 内返回", name)
		}
	}
}
