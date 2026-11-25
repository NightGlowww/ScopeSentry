package intel

import (
	"testing"
	"time"
)

// 判定纪律的三条，全部用合成数据钉住：
//  1. 拿不到版本 → unknown，绝不 confirmed；
//  2. 版本在区间内 → confirmed；
//  3. 版本不在区间内 → unknown（不是 confirmed，也不是 likely），且**不收录**。
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
}

func TestJudgeLikelyOnlyWhenVersionKnown(t *testing.T) {
	rec := CVERecord{CVE: "CVE-TEST-4", Products: []string{"nginx"}}
	a, _ := judge(rec, "d.example.com", "Nginx", versionHit{Version: "1.20.0", Source: "body"})
	if a.Applicability != "likely" {
		t.Fatalf("有版本但无区间应为 likely，得到 %q", a.Applicability)
	}
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
	if inRange("1.25.0", r) {
		t.Fatalf("1.25.0 命中排他上界，区间判断错")
	}
	if !inRange("1.24.9", r) {
		t.Fatalf("1.24.9 应命中")
	}
	if inRange("1.19.0", r) {
		t.Fatalf("1.19.0 低于下界，不应命中")
	}
}

// 缓存新鲜度：空结果必须用更短的 TTL。
// 否则 NVD 抖动一次，「查不到任何 CVE」会被当成事实固化 6 小时，
// 下游拿这份空情报去做判断（2026-11-25 真实踩到）。
func TestCacheFreshEmptyShorterTTL(t *testing.T) {
	now := time.Now()
	full := cacheDoc{FetchedAt: now.Add(-1 * time.Hour), Items: []cacheItem{{CVE: "CVE-1"}}}
	if !cacheFresh(full, now) {
		t.Fatalf("有结果时 1 小时应仍在 6 小时 TTL 内")
	}
	empty := cacheDoc{FetchedAt: now.Add(-1 * time.Hour), Items: []cacheItem{}}
	if cacheFresh(empty, now) {
		t.Fatalf("空结果 1 小时后必须过期（TTL 15 分钟）")
	}
	if !cacheFresh(cacheDoc{FetchedAt: now.Add(-5 * time.Minute)}, now) {
		t.Fatalf("空结果 5 分钟内应仍是新鲜")
	}
}

// 统计口径按宿主去重：同一台机器命中两个技术名只算一台。
func TestStatsDedupByHost(t *testing.T) {
	obs := map[string]map[string]versionHit{
		"nginx":   {"a.example.com": {Version: "1.24.0"}, "b.example.com": {}},
		"tengine": {"a.example.com": {}, "c.example.com": {}},
	}
	hostWithVersion := map[string]bool{}
	for _, hosts := range obs {
		for h, hit := range hosts {
			if hit.Version != "" {
				hostWithVersion[h] = true
			} else if _, ok := hostWithVersion[h]; !ok {
				hostWithVersion[h] = false
			}
		}
	}
	if len(hostWithVersion) != 3 {
		t.Fatalf("a/b/c 三台宿主，得到 %d", len(hostWithVersion))
	}
	if !hostWithVersion["a.example.com"] {
		t.Fatalf("a 在 nginx 上拿到版本，应记为有版本")
	}
	if hostWithVersion["b.example.com"] || hostWithVersion["c.example.com"] {
		t.Fatalf("b/c 无版本，不应记为有版本")
	}
}
