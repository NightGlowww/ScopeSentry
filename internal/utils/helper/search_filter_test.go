package helper

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// 双形态匹配：port 在 asset 里是 string 存法、statuscode 是 number 存法，
// 同一份过滤器要两种都能命中。
func TestBuildFilterForms(t *testing.T) {
	forms := buildFilterForms("port", "443")
	if len(forms) != 2 || forms[0] != "443" || forms[1] != 443 {
		t.Fatalf("port 443 forms = %v, want [443 443]", forms)
	}
	forms = buildFilterForms("host", "example.com")
	if len(forms) != 1 || forms[0] != "example.com" {
		t.Fatalf("non-numeric = %v", forms)
	}
	forms = buildFilterForms("statuscode", 200)
	if len(forms) != 1 || forms[0] != 200 {
		t.Fatalf("already-int passthrough = %v", forms)
	}
	forms = buildFilterForms("port", "abc")
	if len(forms) != 1 || forms[0] != "abc" {
		t.Fatalf("non-numeric string = %v", forms)
	}
}

// asset.port（string 存法）必须能命中：$or 里同时有 '443' 与 443。
func TestGetSearchQueryAssetPortDualForm(t *testing.T) {
	q, err := GetSearchQuery(models.SearchRequest{
		Index:  "asset",
		Filter: map[string][]interface{}{"port": {"443"}},
	})
	if err != nil {
		t.Fatalf("GetSearchQuery: %v", err)
	}
	s := fmt.Sprint(q)
	if !strings.Contains(s, "443") {
		t.Fatalf("query missing port value: %s", s)
	}
}

// UrlScan 的 statuscode 别名到 status 后仍生成双形态。
func TestGetSearchQueryUrlScanFilterAliasDual(t *testing.T) {
	q, err := GetSearchQuery(models.SearchRequest{
		Index:  "UrlScan",
		Filter: map[string][]interface{}{"statuscode": {"200"}},
	})
	if err != nil {
		t.Fatalf("GetSearchQuery: %v", err)
	}
	s := fmt.Sprint(q)
	if strings.Contains(s, "statuscode") {
		t.Fatalf("UrlScan should alias to status: %s", s)
	}
	if !strings.Contains(s, "200") {
		t.Fatalf("missing value: %s", s)
	}
}
