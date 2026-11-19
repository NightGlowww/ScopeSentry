package helper

import (
	"reflect"
	"sort"
	"testing"
)

// 建任务的目标解析是扫描链路入口：换行拆分、去重、忽略（精确 + 通配 *）必须稳定。
func TestGetTargetListBasics(t *testing.T) {
	cases := []struct {
		name   string
		target string
		ignore string
		want   []string
	}{
		{
			name:   "多行去空白与空行",
			target: "example.com\n\n  foo.com  \n",
			want:   []string{"example.com", "foo.com"},
		},
		{
			name:   "重复目标去重",
			target: "a.com\na.com",
			want:   []string{"a.com"},
		},
		{
			name:   "忽略列表精确匹配",
			target: "a.com\nb.com",
			ignore: "b.com",
			want:   []string{"a.com"},
		},
		{
			name:   "忽略列表通配符",
			target: "a.test.com\nb.prod.com",
			ignore: "*.test.com",
			want:   []string{"b.prod.com"},
		},
		{
			name:   "忽略列表带协议前缀",
			target: "a.com\nb.com",
			ignore: "https://b.com",
			want:   []string{"a.com"},
		},
	}
	for _, c := range cases {
		got, err := GetTargetList(c.target, c.ignore)
		if err != nil {
			t.Fatalf("%s: error %v", c.name, err)
		}
		sort.Strings(got)
		sort.Strings(c.want)
		if !reflect.DeepEqual(got, c.want) {
			t.Fatalf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestGetTargetListEmpty(t *testing.T) {
	got, err := GetTargetList("   \n  ", "")
	if err != nil {
		t.Fatalf("error %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v, want empty", got)
	}
}
