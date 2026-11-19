package helper

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// 过滤器的字段别名与数值归一：此前 UrlScan 下 statuscode 过滤查不存在的字段、静默空。
func TestFilterNormalize(t *testing.T) {
	if v, ok := normalizeFilterValue("statuscode", "200"); !ok || v != 200 {
		t.Fatalf("statuscode 200 string -> got %v %v", v, ok)
	}
	if v, ok := normalizeFilterValue("status", "404"); !ok || v != 404 {
		t.Fatalf("status 404 string -> got %v %v", v, ok)
	}
	if v, ok := normalizeFilterValue("statuscode", 200); ok || v != 200 {
		t.Fatalf("already-int should pass through: %v %v", v, ok)
	}
	if v, ok := normalizeFilterValue("host", "example.com"); ok || v != "example.com" {
		t.Fatalf("non-numeric field untouched: %v %v", v, ok)
	}
	if v, ok := normalizeFilterValue("port", "abc"); ok || v != "abc" {
		t.Fatalf("non-numeric string untouched: %v %v", v, ok)
	}
}

// 端到端：UrlScan 的 statuscode 过滤应映射到 status 且数字串转数字。
func TestGetSearchQueryUrlScanFilterAlias(t *testing.T) {
	q, err := GetSearchQuery(models.SearchRequest{
		Index:  "UrlScan",
		Filter: map[string][]interface{}{"statuscode": {"200"}},
	})
	if err != nil {
		t.Fatalf("GetSearchQuery: %v", err)
	}
	s := fmt.Sprint(q)
	if strings.Contains(s, "statuscode") {
		t.Fatalf("UrlScan filter should alias to status, got %s", s)
	}
	if !strings.Contains(s, "200") || strings.Contains(s, "\"200\"") {
		t.Fatalf("numeric string should become int, got %s", s)
	}
}
