package nucleiresult

import "testing"

// 扫描侧落库是 "rootdomain"，老数据是 "rootDomain"，两种都要能取到根域名。
func TestNucleiDocEffectiveRootDomain(t *testing.T) {
	cases := []struct {
		name string
		doc  nucleiDoc
		want string
	}{
		{"lowercase (node writes this)", nucleiDoc{RootDomain: "example.com"}, "example.com"},
		{"camelCase legacy", nucleiDoc{RootDomainLegacy: "legacy.com"}, "legacy.com"},
		{"both present prefers lowercase", nucleiDoc{RootDomain: "a.com", RootDomainLegacy: "b.com"}, "a.com"},
		{"empty", nucleiDoc{}, ""},
	}
	for _, c := range cases {
		if got := c.doc.effectiveRootDomain(); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}
