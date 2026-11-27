package compress

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(StaticCache())
	r.Use(Gzip())
	r.GET("/assets/app-abc123.js", func(c *gin.Context) {
		c.Header("Content-Type", "text/javascript")
		c.String(http.StatusOK, strings.Repeat("console.log('x');", 100))
	})
	r.GET("/", func(c *gin.Context) {
		c.Header("Content-Type", "text/html")
		c.String(http.StatusOK, "<html></html>")
	})
	r.POST("/api/login", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"code": 200, "message": "ok", "data": gin.H{"token": strings.Repeat("a", 500)}})
	})
	return r
}

func TestStaticCacheHeaders(t *testing.T) {
	r := setupRouter()

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/assets/app-abc123.js", nil))
	if got := w.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("hashed asset should be immutable, got %q", got)
	}

	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	r.ServeHTTP(w, req)
	if got := w.Header().Get("Cache-Control"); got != "no-store, must-revalidate" {
		t.Fatalf("index should be no-store, got %q", got)
	}
}

// SPA 深链（/configuration/mcp-audit 这类）返回的也是入口 HTML，
// 之前没有任何 Cache-Control，浏览器启发式缓存后发版即白屏（2026-11-28）。
func TestStaticCacheSPADeepLinkNoStore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(StaticCache())
	r.NoRoute(func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, "<html></html>")
	})

	req := httptest.NewRequest(http.MethodGet, "/configuration/mcp-audit", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if got := w.Header().Get("Cache-Control"); got != "no-store, must-revalidate" {
		t.Fatalf("SPA deep link should be no-store, got %q", got)
	}

	// 接口与静态资源不能被误伤
	req = httptest.NewRequest(http.MethodGet, "/assets/x-abc.js", nil)
	req.Header.Set("Accept", "text/html")
	w = httptest.NewRecorder()
	r.Use(StaticCache())
	r.ServeHTTP(w, req)
}

func TestGzipCompressesJSON(t *testing.T) {
	r := setupRouter()

	req := httptest.NewRequest(http.MethodPost, "/api/login", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("expected gzip, got %q", got)
	}
	if w.Body.Len() == 0 {
		t.Fatal("expected compressed body")
	}
	if !strings.Contains(w.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatal("expected Vary: Accept-Encoding")
	}
}

func TestGzipSkipsHeadAndRange(t *testing.T) {
	r := setupRouter()

	req := httptest.NewRequest(http.MethodHead, "/assets/app-abc123.js", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("HEAD must not be compressed, got %q", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/assets/app-abc123.js", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Range", "bytes=0-10")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Range must not be compressed, got %q", got)
	}
}

func TestGzipSkippedWhenClientDoesNotAcceptIt(t *testing.T) {
	r := setupRouter()

	req := httptest.NewRequest(http.MethodPost, "/api/login", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("unexpected gzip without Accept-Encoding: %q", got)
	}
}
