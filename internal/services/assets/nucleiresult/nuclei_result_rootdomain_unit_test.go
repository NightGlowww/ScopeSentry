package nucleiresult

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// 关键词搜索必须同时覆盖 rootdomain（扫描侧实际落库的键）与 rootDomain（老数据），
// 否则按根域名搜新数据恒为空。见 docs/audit/VULN_LINK.md。
func TestBuildListFilterKeywordCoversBothRootDomainSpellings(t *testing.T) {
	filter := BuildListFilter(models.NucleiResultListRequest{Keyword: "example.com"})

	raw, err := json.Marshal(filter)
	if err != nil {
		t.Fatalf("marshal filter: %v", err)
	}
	got := string(raw)

	for _, want := range []string{"\"rootdomain\"", "\"rootDomain\""} {
		if !strings.Contains(got, want) {
			t.Errorf("keyword filter missing key %s: %s", want, got)
		}
	}
	if !strings.Contains(got, "\"url\"") || !strings.Contains(got, "\"vulnname\"") {
		t.Errorf("keyword filter lost url/vulnname branches: %s", got)
	}
}
