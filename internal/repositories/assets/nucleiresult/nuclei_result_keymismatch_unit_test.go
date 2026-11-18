package nucleiresult

import "testing"

// 扫描侧落库是 "vulname"/"rootdomain"，历史读模型写的是 "vulnname"/"rootDomain"，
// 两种拼写都要能取到，否则列表会出现「有结果但名字/根域名为空」。
func TestNucleiDocEffectiveFields(t *testing.T) {
	cases := []struct {
		name     string
		doc      nucleiDoc
		wantName string
		wantRoot string
	}{
		{"node writes lowercase", nucleiDoc{VulName: "Log4j RCE", RootDomain: "example.com"}, "Log4j RCE", "example.com"},
		{"legacy spelling", nucleiDoc{VulNameLegacy: "Old RCE", RootDomainLegacy: "legacy.com"}, "Old RCE", "legacy.com"},
		{"both present prefers actual", nucleiDoc{VulName: "a", VulNameLegacy: "b", RootDomain: "a.com", RootDomainLegacy: "b.com"}, "a", "a.com"},
		{"empty", nucleiDoc{}, "", ""},
	}
	for _, c := range cases {
		if got := c.doc.effectiveVulName(); got != c.wantName {
			t.Errorf("%s: name got %q want %q", c.name, got, c.wantName)
		}
		if got := c.doc.effectiveRootDomain(); got != c.wantRoot {
			t.Errorf("%s: rootDomain got %q want %q", c.name, got, c.wantRoot)
		}
	}
}
