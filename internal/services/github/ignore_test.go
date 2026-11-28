package github

import "testing"

// 黑名单判定是「忽略仓库」这个功能的唯一执行点：写错了要么漏拦（噪声继续进），
// 要么过度拦截（真结果被吞）。所以把三种边界都钉住。
func TestMatchIgnoredRepo(t *testing.T) {
	cases := []struct {
		name     string
		patterns []string
		repo     string
		want     bool
	}{
		{"精确命中", []string{"acme/web-app"}, "acme/web-app", true},
		{"大小写不敏感", []string{"Acme/Web-App"}, "acme/web-app", true},
		{"owner 通配", []string{"acme/*"}, "acme/anything", true},
		{"owner 通配不跨 owner", []string{"acme/*"}, "other/anything", false},
		{"未命中", []string{"acme/web-app"}, "acme/web-app2", false},
		{"前缀相同但不同仓库", []string{"acme/web"}, "acme/web-app", false},
		{"空黑名单", nil, "acme/web-app", false},
		{"空仓库名", []string{"acme/*"}, "", false},
		{"空白项被忽略", []string{"  ", ""}, "acme/web-app", false},
	}
	for _, c := range cases {
		if got := matchIgnoredRepo(c.patterns, c.repo); got != c.want {
			t.Fatalf("%s: matchIgnoredRepo(%v, %q) = %v, want %v", c.name, c.patterns, c.repo, got, c.want)
		}
	}
}
