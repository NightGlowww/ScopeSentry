package fofa_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	fofasvc "github.com/Autumn-27/ScopeSentry/internal/services/fofa"
)

// FOFA provider 单元测试（httptest 假服务，不连 Mongo / 不发真实请求）。
//
// 覆盖：
//  1. host 归一规则（协议/路径/端口/userinfo/尾点/IPv6）；
//  2. http provider 的 qbase64/page/size/fields 参数与 results 行形状；
//  3. FOFA 业务错误（{"error":true,"errmsg":...}）→ *ProviderError；
//  4. 未配置 key → none provider（available=false + 明确原因）；
//  5. 配置 key → status 返回余额；
//  6. FetchHosts 按 100/页翻页。

// fakeFofa 假 FOFA：记录收到的请求，按 page/size 切 host 列表
type fakeFofa struct {
	mu       sync.Mutex
	hosts    []string
	requests []fakeFofaRequest
	errQuery string
}

type fakeFofaRequest struct {
	Path  string
	Query string
	Page  int
	Size  int
}

func (f *fakeFofa) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/info/my":
			_, _ = w.Write([]byte(`{"error":false,"username":"it@example.com","fofa_point":1000,"remain_free_point":9,"remain_api_query":3,"vip_level":2,"is_vip":true}`))
		case "/api/v1/search/all":
			qbase64 := r.URL.Query().Get("qbase64")
			decoded, _ := base64.StdEncoding.DecodeString(qbase64)
			query := string(decoded)
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			size, _ := strconv.Atoi(r.URL.Query().Get("size"))
			f.mu.Lock()
			f.requests = append(f.requests, fakeFofaRequest{Path: r.URL.Path, Query: query, Page: page, Size: size})
			errQuery := f.errQuery
			hosts := append([]string(nil), f.hosts...)
			f.mu.Unlock()

			if errQuery != "" && query == errQuery {
				_, _ = w.Write([]byte(`{"error":true,"errmsg":"[-700] 账号无效"}`))
				return
			}
			start := (page - 1) * size
			if start > len(hosts) {
				start = len(hosts)
			}
			end := start + size
			if end > len(hosts) {
				end = len(hosts)
			}
			rows := make([][]string, 0, end-start)
			for _, host := range hosts[start:end] {
				rows = append(rows, []string{host})
			}
			payload := map[string]interface{}{
				"error":           false,
				"total":           len(hosts),
				"page":            page,
				"size":            len(rows),
				"results":         rows,
				"consumed_fpoint": 1,
			}
			_ = json.NewEncoder(w).Encode(payload)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

// serviceWith 用固定配置构造服务（base_url 指到假服务）
func serviceWith(baseURL, key string) *fofasvc.Service {
	registry := fofasvc.NewProviderRegistryWithConfig(func(context.Context) fofasvc.ProviderConfig {
		if key == "" {
			return fofasvc.ProviderConfig{BaseURL: baseURL, BaseURLSource: "test", KeySource: "none"}
		}
		return fofasvc.ProviderConfig{Key: key, KeySource: "test", BaseURL: baseURL, BaseURLSource: "test"}
	})
	return fofasvc.NewService(fofasvc.WithProviderRegistry(registry))
}

func TestNormalizeFofaHost(t *testing.T) {
	cases := []struct{ in, want string }{
		{"http://Example.COM:8080/a/b?c=1", "example.com:8080"},
		{"https://1.2.3.4", "1.2.3.4"},
		{"ftp://a.example.com/path", "a.example.com"},
		{"a.example.com.", "a.example.com"},
		{"a.example.com:443", "a.example.com:443"},
		{"a.example.com:", "a.example.com"},
		{"a.example.com:abc", "a.example.com"},
		{"http://user:pass@a.example.com:8443/x", "a.example.com:8443"},
		{"[2001:db8::1]:443", "[2001:db8::1]:443"},
		{"http://[2001:db8::1]/x", "[2001:db8::1]"},
		{"  HTTPS://B.Example.com/  ", "b.example.com"},
		{"", ""},
		{"   ", ""},
		{"http://", ""},
		{"a example.com", ""},
		{"a.example.com, b.example.com", ""},
		{"-bad-.example.com", "-bad-.example.com"},
	}
	for _, tc := range cases {
		if got := models.NormalizeFofaHost(tc.in); got != tc.want {
			t.Errorf("NormalizeFofaHost(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestHTTPProviderSearchParamsAndDecode(t *testing.T) {
	fake := &fakeFofa{hosts: []string{
		"http://a.example.com:8443/x",
		"https://b.example.com",
		"1.2.3.4:8080",
	}}
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	svc := serviceWith(server.URL, "test-key")
	page, err := svc.Search(context.Background(), models.FofaSearchRequest{Query: `app="test"`, Page: 2, Size: 2})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if page.Provider != models.FofaProviderHTTP {
		t.Fatalf("provider = %q, want http", page.Provider)
	}
	if page.Page != 2 {
		t.Fatalf("page = %d, want 2", page.Page)
	}
	if len(page.Hosts) != 1 || page.Hosts[0] != "1.2.3.4:8080" {
		t.Fatalf("hosts = %v, want [1.2.3.4:8080]（第 2 页只剩 1 条）", page.Hosts)
	}
	if page.Total != 3 {
		t.Fatalf("total = %d, want 3", page.Total)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(fake.requests))
	}
	req := fake.requests[0]
	if req.Query != `app="test"` {
		t.Fatalf("qbase64 解码 = %q, want app=test", req.Query)
	}
	if req.Page != 2 || req.Size != 2 {
		t.Fatalf("page/size = %d/%d, want 2/2", req.Page, req.Size)
	}
}

func TestHTTPProviderSearchFieldsAndBase64(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 断言 qbase64 是标准 base64（而不是明文 query）
		raw := r.URL.Query().Get("qbase64")
		if raw == "" {
			t.Errorf("missing qbase64")
		}
		if decoded, err := base64.StdEncoding.DecodeString(raw); err != nil || string(decoded) != `domain="example.com"` {
			t.Errorf("qbase64 = %q, decode = %q, err = %v", raw, string(decoded), err)
		}
		if got := r.URL.Query().Get("fields"); got != "host" {
			t.Errorf("fields = %q, want host", got)
		}
		if got := r.URL.Query().Get("key"); got != "test-key" {
			t.Errorf("key = %q, want test-key", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":false,"total":1,"page":1,"size":1,"results":[["a.example.com"]]}`))
	}))
	defer server.Close()

	svc := serviceWith(server.URL, "test-key")
	page, err := svc.Search(context.Background(), models.FofaSearchRequest{Query: `domain="example.com"`, Size: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(page.Hosts) != 1 || page.Hosts[0] != "a.example.com" {
		t.Fatalf("hosts = %v", page.Hosts)
	}
}

func TestHTTPProviderBusinessError(t *testing.T) {
	fake := &fakeFofa{errQuery: "bad-query"}
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	svc := serviceWith(server.URL, "test-key")
	_, err := svc.Search(context.Background(), models.FofaSearchRequest{Query: "bad-query"})
	if err == nil {
		t.Fatalf("want error")
	}
	var providerErr *fofasvc.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("err = %T (%v), want *ProviderError", err, err)
	}
	if providerErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", providerErr.StatusCode)
	}
	if !strings.Contains(providerErr.Message, "账号无效") {
		t.Fatalf("message = %q, want 含 FOFA errmsg", providerErr.Message)
	}
}

func TestNoneProviderWhenKeyMissing(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	svc := serviceWith(server.URL, "")
	status := svc.ProviderStatus(context.Background())
	if status.Provider != models.FofaProviderNone {
		t.Fatalf("provider = %q, want none", status.Provider)
	}
	if status.Available {
		t.Fatalf("available = true, want false")
	}
	if !strings.Contains(status.Reason, "fofa.key") {
		t.Fatalf("reason = %q, want 提到 fofa.key", status.Reason)
	}
	if _, err := svc.Search(context.Background(), models.FofaSearchRequest{Query: `app="x"`}); !errors.Is(err, fofasvc.ErrProviderUnavailable) {
		t.Fatalf("Search err = %v, want ErrProviderUnavailable", err)
	}
}

func TestProviderStatusBalance(t *testing.T) {
	fake := &fakeFofa{}
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	svc := serviceWith(server.URL, "test-key")
	status := svc.ProviderStatus(context.Background())
	if !status.Available {
		t.Fatalf("available = false (%s)", status.Reason)
	}
	if status.Balance == nil {
		t.Fatalf("balance 为空")
	}
	if status.Balance.FofaPoint == nil || *status.Balance.FofaPoint != 1000 {
		t.Fatalf("fofaPoint = %v, want 1000", status.Balance.FofaPoint)
	}
	if status.Balance.Username != "it@example.com" {
		t.Fatalf("username = %q", status.Balance.Username)
	}
	if len(status.Candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(status.Candidates))
	}
}

func TestFetchHostsPaging(t *testing.T) {
	hosts := make([]string, 0, 250)
	for i := 0; i < 250; i++ {
		hosts = append(hosts, fmt.Sprintf("h%d.example.com", i))
	}
	fake := &fakeFofa{hosts: hosts}
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	svc := serviceWith(server.URL, "test-key")
	got, err := svc.FetchHosts(context.Background(), `domain="example.com"`, 250)
	if err != nil {
		t.Fatalf("FetchHosts: %v", err)
	}
	if len(got) != 250 {
		t.Fatalf("hosts = %d, want 250", len(got))
	}
	if got[0] != "h0.example.com" || got[249] != "h249.example.com" {
		t.Fatalf("首尾不符: %q ... %q", got[0], got[249])
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.requests) != 3 {
		t.Fatalf("requests = %d, want 3（250 条按 100/页翻 3 页）", len(fake.requests))
	}
	for i, req := range fake.requests {
		if req.Size != 100 {
			t.Fatalf("request[%d].size = %d, want 100（每页固定 100，避免最后一页偏移重叠）", i, req.Size)
		}
		if req.Page != i+1 {
			t.Fatalf("request[%d].page = %d, want %d", i, req.Page, i+1)
		}
	}
}

func TestSearchSizeLimit(t *testing.T) {
	fake := &fakeFofa{}
	server := httptest.NewServer(fake.handler())
	defer server.Close()
	svc := serviceWith(server.URL, "test-key")
	if _, err := svc.Search(context.Background(), models.FofaSearchRequest{Query: "x", Size: models.FofaMaxPreviewSize + 1}); !errors.Is(err, fofasvc.ErrPreviewSizeExceeded) {
		t.Fatalf("err = %v, want ErrPreviewSizeExceeded", err)
	}
	if _, err := svc.Search(context.Background(), models.FofaSearchRequest{Query: "x", Size: models.FofaMaxPreviewSize}); err != nil {
		t.Fatalf("size=100 err = %v", err)
	}
}

// 确保 url.Values 编码可解析（qbase64 里 + / = 必须被正确转义）
func TestQueryEncodingRoundTrip(t *testing.T) {
	values := url.Values{}
	values.Set("qbase64", base64.StdEncoding.EncodeToString([]byte("a=1 && b=2")))
	parsed, err := url.ParseQuery(values.Encode())
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	raw := parsed.Get("qbase64")
	if decoded, _ := base64.StdEncoding.DecodeString(raw); string(decoded) != "a=1 && b=2" {
		t.Fatalf("decoded = %q", string(decoded))
	}
}
