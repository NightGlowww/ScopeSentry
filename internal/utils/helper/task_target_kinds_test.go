package helper

import (
	"reflect"
	"testing"
)

// GenerateTarget 是靶标解析的基础：URL 原样、CIDR 展开、IP 段展开、域名原样。
// 特别注意 /31：RFC 3021 两端都可用，历史上被当成"网络地址+广播地址"剥空，
// 导致任务下发后 0 个目标。
func TestGenerateTargetKinds(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"http://example.com", []string{"http://example.com"}},
		{"my-site.com", []string{"my-site.com"}},
		{"10.0.0.0/30", []string{"10.0.0.1", "10.0.0.2"}},
		{"10.0.0.0/31", []string{"10.0.0.0", "10.0.0.1"}},
		{"10.0.0.1/32", []string{"10.0.0.1"}},
		{"192.168.1.1-192.168.1.3", []string{"192.168.1.1", "192.168.1.2", "192.168.1.3"}},
		// 超大网段与 IPv6 不展开，退化为字面量目标（否则 /0 会分配 40 亿个字符串）
		{"0.0.0.0/0", []string{"0.0.0.0/0"}},
		{"2001:db8::/126", []string{"2001:db8::/126"}},
	}
	for _, c := range cases {
		got := GenerateTarget(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Fatalf("GenerateTarget(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
