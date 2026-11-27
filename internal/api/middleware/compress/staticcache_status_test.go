package compress

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// 200 的 /assets 响应仍然长缓存；非 200 必须 no-store。
// 依据（2026-11-28 实测）：发版窗口里的一次 404 被 immutable 缓存了一年，
// 之后每次重载都直接用缓存里的 404 → 页面永久白屏。
func TestStaticCacheOnlyCachesSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name   string
		path   string
		status int
		wantCC string
	}{
		{"assets 200 长缓存", "/assets/index-abc.js", http.StatusOK, "public, max-age=31536000, immutable"},
		{"assets 404 不缓存", "/assets/gone-abc.js", http.StatusNotFound, "no-store"},
		{"其它静态 200", "/css/app.css", http.StatusOK, "public, max-age=3600"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.Use(StaticCache())
			r.GET(tc.path, func(c *gin.Context) {
				c.String(tc.status, "body")
			})
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodGet, tc.path, nil)
			r.ServeHTTP(w, req)
			if got := w.Header().Get("Cache-Control"); got != tc.wantCC {
				t.Fatalf("%s: Cache-Control = %q, want %q", tc.path, got, tc.wantCC)
			}
		})
	}
}
