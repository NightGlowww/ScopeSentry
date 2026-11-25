package intel

import (
	"testing"
)

// 判定纪律的三条，全部用合成数据钉住：
//  1. 拿不到版本 → unknown，绝不 confirmed；
//  2. 版本在区间内 → confirmed；
//  3. 版本不在区间内 → unknown（不是 confirmed，也不是 likely）。
func TestJudgeUnknownWhenNoVersion(t *testing.T) {
	rec := CVERecord{CVE: "CVE-TEST-1", Ranges: []VersionRange{{Product: "nginx", VersionEndIncl: "1.25.0"}}}
	a, include := judge(rec, "a.example.com", "Nginx", versionHit{})
	if !include {
		t.Fatalf("unknown 分支必须收录（它是「无法判定」，不是阴性）")
	}
	if a.Applicability != "unknown" {
		t.Fatalf("无版本必须 unknown，得到 %q", a.Applicability)
	}
	if a.Version != nil || a.VersionSource != nil {
		t.Fatalf("无版本时 version/version_source 必须为 null")
	}
	if a.Reason == "" {
		t.Fatalf("unknown 必须写清理由")
	}
}

func TestJudgeConfirmedInRange(t *testing.T) {
	rec := CVERecord{CVE: "CVE-TEST-2", Ranges: []VersionRange{{Product: "nginx", VersionEndIncl: "1.25.0"}}}
	a, include := judge(rec, "b.example.com", "Nginx", versionHit{Version: "1.24.0", Source: "header"})
	if !include {
		t.Fatalf("confirmed 必须收录")
	}
	if a.Applicability != "confirmed" {
		t.Fatalf("1.24.0 <= 1.25.0 应 confirmed，得到 %q (%s)", a.Applicability, a.Reason)
	}
	if a.Version == nil || *a.Version != "1.24.0" {
		t.Fatalf("version 必须回填")
	}
	if a.VersionSource == nil || *a.VersionSource != "header" {
		t.Fatalf("version_source 必须写清")
	}
}

func TestJudgeOutOfRangeIsNotConfirmed(t *testing.T) {
	rec := CVERecord{CVE: "CVE-TEST-3", Ranges: []VersionRange{{Product: "nginx", VersionEndIncl: "1.25.0"}}}
	a, include := judge(rec, "c.example.com", "Nginx", versionHit{Version: "1.27.3", Source: "banner"})
	if include {
		t.Fatalf("版本明确超出区间 = 阴性对照，不应收录进 affected")
	}
	if a.Applicability == "confirmed" {
		t.Fatalf("1.27.3 超出 <=1.25.0，不得 confirmed")
	}
	if a.Applicability != "unknown" {
		t.Fatalf("超出区间应为 unknown，得到 %q", a.Applicability)
	}
}

func TestJudgeLikelyOnlyWhenVersionKnown(t *testing.T) {
	// 产品匹配但没有版本区间 → likely（因为版本是已知的）；
	rec := CVERecord{CVE: "CVE-TEST-4", Products: []string{"nginx"}}
	a, _ := judge(rec, "d.example.com", "Nginx", versionHit{Version: "1.20.0", Source: "body"})
	if a.Applicability != "likely" {
		t.Fatalf("有版本但无区间应为 likely，得到 %q", a.Applicability)
	}
	// 无版本时同样是这条 CVE，必须 unknown 而不是 likely。
	b, _ := judge(rec, "e.example.com", "Nginx", versionHit{})
	if b.Applicability != "unknown" {
		t.Fatalf("无版本不得降级为 likely，得到 %q", b.Applicability)
	}
}

func TestCmpVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.24.0", "1.25.0", -1},
		{"1.25.0", "1.25.0", 0},
		{"2.3.3", "2.3.3", 0},
		{"1.27.3", "1.25.0", 1},
	}
	for _, c := range cases {
		if got := cmpVersion(c.a, c.b); got != c.want {
			t.Fatalf("cmp(%s,%s)=%d，期望 %d", c.a, c.b, got, c.want)
		}
	}
}

func TestInRangeExclusive(t *testing.T) {
	r := VersionRange{Product: "nginx", VersionStartIncl: "1.20.0", VersionEndExcl: "1.25.0"}
	if !inRange("1.25.0", r) {
		// 1.25.0 是排他的上界，不应命中
	} else {
		t.Fatalf("1.25.0 命中排他上界，区间判断错")
	}
	if !inRange("1.24.9", r) {
		t.Fatalf("1.24.9 应命中")
	}
	if inRange("1.19.0", r) {
		t.Fatalf("1.19.0 低于下界，不应命中")
	}
}
