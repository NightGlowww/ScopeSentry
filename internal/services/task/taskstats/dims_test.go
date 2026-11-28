package taskstats

import (
	"errors"
	"testing"
)

// 维度解析是纯逻辑：不支持的维度必须报错，不能静默退化成「全部」，
// 否则用户筛「nuclei」会拿到全部任务，还以为命中的是 nuclei。
func TestResolveDims(t *testing.T) {
	all, err := ResolveDims(nil)
	if err != nil {
		t.Fatalf("空维度应等于全部: %v", err)
	}
	want := []string{"asset", "subdomain", "url", "ip", "vulnerability", "sensitive"}
	if len(all) != len(want) {
		t.Fatalf("默认维度数 = %d, want %d (%v)", len(all), len(want), all)
	}
	for i := range want {
		if all[i] != want[i] {
			t.Fatalf("默认维度[%d] = %q, want %q", i, all[i], want[i])
		}
	}

	got, err := ResolveDims([]string{"Site", "domain", "site"})
	if err != nil {
		t.Fatalf("合法维度不该报错: %v", err)
	}
	if len(got) != 2 || got[0] != "asset" || got[1] != "subdomain" {
		t.Fatalf("别名归一/去重失败: %v", got)
	}

	for _, bad := range []string{"nuclei", "", "whatever"} {
		if _, err := ResolveDims([]string{bad}); !errors.Is(err, ErrUnsupportedDim) {
			t.Fatalf("维度 %q 应返回 ErrUnsupportedDim，实际 %v", bad, err)
		}
	}
}