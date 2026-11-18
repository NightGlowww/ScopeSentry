package ct

import (
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/utils/ct/types"
)

// 固定「range string 取代 []rune」重构后的行为：中文/拼音/普通域名的增删查一致。
func TestDomainMatcherAddHasRemove(t *testing.T) {
	m := NewDomainMatcher(&types.DomainMatcherConfig{})
	for _, d := range []string{"example.com", "测试.中国", "sub.example.com"} {
		if err := m.AddDomain(d); err != nil {
			t.Fatalf("AddDomain(%q) error: %v", d, err)
		}
	}
	for _, d := range []string{"example.com", "测试.中国", "sub.example.com"} {
		if !m.HasDomain(d) {
			t.Fatalf("HasDomain(%q) = false", d)
		}
	}
	if m.HasDomain("absent.com") {
		t.Fatal("HasDomain(absent.com) = true")
	}
	if err := m.RemoveDomain("example.com"); err != nil {
		t.Fatalf("RemoveDomain: %v", err)
	}
	if m.HasDomain("example.com") {
		t.Fatal("example.com should be removed")
	}
	if !m.HasDomain("sub.example.com") {
		t.Fatal("sub.example.com should survive removal of its parent")
	}
}
