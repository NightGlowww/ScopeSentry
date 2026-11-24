package images

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
)

// onePixelPNGBase64 是 1×1 透明 PNG，用作「扫描节点写进 MongoDB 的 favicon」。
const onePixelPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func newTestRouter(t *testing.T, lookup IconContentLookup) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	router := gin.New()
	RegisterRoutes(router, root, lookup)
	return router, root
}

func pngBody(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(onePixelPNGBase64)
	if err != nil {
		t.Fatalf("内置 PNG base64 无效: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("内置样例不是 PNG")
	}
	return data
}

func record(t *testing.T, router *gin.Engine, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(method, target, nil))
	return w
}

// 截图等非图标图片必须完全走本地文件，不能为了修图标把回源逻辑套到所有 /images 上。
func TestScreenshotServedFromDiskWithoutLookup(t *testing.T) {
	router, root := newTestRouter(t, func(context.Context, string) (string, error) {
		t.Fatal("截图请求不应回源 MongoDB")
		return "", ErrIconNotFound
	})
	want := pngBody(t)
	dir := filepath.Join(root, "screenshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("创建截图目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "abc.png"), want, 0o644); err != nil {
		t.Fatalf("写入截图失败: %v", err)
	}

	w := record(t, router, http.MethodGet, "/images/screenshots/abc.png")
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", got)
	}
	if !bytes.Equal(w.Body.Bytes(), want) {
		t.Fatal("响应内容与磁盘文件不一致")
	}
}

// 核心回归：图标文件不存在时回源 icon 集合，落盘后再请求不再回源。
func TestIconMaterializedFromMongoThenServedFromCache(t *testing.T) {
	want := pngBody(t)
	var lookups int32
	router, root := newTestRouter(t, func(_ context.Context, hash string) (string, error) {
		atomic.AddInt32(&lookups, 1)
		if hash != "123456789" {
			t.Fatalf("回源 hash = %q, want 123456789", hash)
		}
		return base64.StdEncoding.EncodeToString(want), nil
	})

	first := record(t, router, http.MethodGet, "/images/icon/123456789.png")
	if first.Code != http.StatusOK {
		t.Fatalf("首次请求状态码 = %d, want 200", first.Code)
	}
	if got := first.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", got)
	}
	if !bytes.Equal(first.Body.Bytes(), want) {
		t.Fatal("响应内容与 MongoDB 中的图标不一致")
	}
	if got := first.Header().Get("Cache-Control"); got == "" {
		t.Fatal("图标响应缺少 Cache-Control")
	}

	cached, err := os.ReadFile(filepath.Join(root, "icon", "123456789.png"))
	if err != nil {
		t.Fatalf("图标未落盘: %v", err)
	}
	if !bytes.Equal(cached, want) {
		t.Fatal("落盘内容与解码结果不一致")
	}

	second := record(t, router, http.MethodGet, "/images/icon/123456789.png")
	if second.Code != http.StatusOK {
		t.Fatalf("二次请求状态码 = %d, want 200", second.Code)
	}
	if n := atomic.LoadInt32(&lookups); n != 1 {
		t.Fatalf("回源次数 = %d, want 1（命中缓存后不应再查库）", n)
	}
}

func TestIconMissingReturns404(t *testing.T) {
	router, _ := newTestRouter(t, func(context.Context, string) (string, error) {
		return "", ErrIconNotFound
	})
	if w := record(t, router, http.MethodGet, "/images/icon/42.png"); w.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d, want 404", w.Code)
	}
}

// 数据库故障要能和「确实没有图标」区分开，否则排障时看什么都是 404。
func TestIconLookupFailureReturns503(t *testing.T) {
	router, _ := newTestRouter(t, func(context.Context, string) (string, error) {
		return "", errors.New("mongo unreachable")
	})
	if w := record(t, router, http.MethodGet, "/images/icon/42.png"); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("状态码 = %d, want 503", w.Code)
	}
}

// 脏数据不能变成「看起来像图片」的响应：非图片字节一律 404，也不落盘。
func TestIconRejectsNonImagePayload(t *testing.T) {
	router, root := newTestRouter(t, func(context.Context, string) (string, error) {
		return base64.StdEncoding.EncodeToString([]byte("<html>404 Not Found</html>")), nil
	})
	if w := record(t, router, http.MethodGet, "/images/icon/77.png"); w.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d, want 404", w.Code)
	}
	if _, err := os.Stat(filepath.Join(root, "icon", "77.png")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("脏数据不应写入缓存目录")
	}
}

func TestIconAcceptsDataURIPrefix(t *testing.T) {
	want := pngBody(t)
	router, _ := newTestRouter(t, func(context.Context, string) (string, error) {
		return "data:image/png;base64," + base64.StdEncoding.EncodeToString(want), nil
	})
	w := record(t, router, http.MethodGet, "/images/icon/88.png")
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", w.Code)
	}
	if !bytes.Equal(w.Body.Bytes(), want) {
		t.Fatal("data URI 前缀未被剥离")
	}
}

// mmh3 是 32 位有符号整数，负值哈希是正常数据，不能被路径校验挡掉。
func TestIconAcceptsNegativeHash(t *testing.T) {
	var seen string
	router, _ := newTestRouter(t, func(_ context.Context, hash string) (string, error) {
		seen = hash
		return base64.StdEncoding.EncodeToString(pngBody(t)), nil
	})
	if w := record(t, router, http.MethodGet, "/images/icon/-123456789.png"); w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", w.Code)
	}
	if seen != "-123456789" {
		t.Fatalf("回源 hash = %q, want -123456789", seen)
	}
}

// 历史上被打断的写入会留下 0 字节文件；不能因此永久破图。
func TestIconEmptyCacheFileIsRefetched(t *testing.T) {
	want := pngBody(t)
	var lookups int32
	router, root := newTestRouter(t, func(context.Context, string) (string, error) {
		atomic.AddInt32(&lookups, 1)
		return base64.StdEncoding.EncodeToString(want), nil
	})
	if err := os.MkdirAll(filepath.Join(root, "icon"), 0o755); err != nil {
		t.Fatalf("创建缓存目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "icon", "99.png"), nil, 0o644); err != nil {
		t.Fatalf("写入空缓存失败: %v", err)
	}

	w := record(t, router, http.MethodGet, "/images/icon/99.png")
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", w.Code)
	}
	if n := atomic.LoadInt32(&lookups); n != 1 {
		t.Fatalf("回源次数 = %d, want 1", n)
	}
	if cached, err := os.ReadFile(filepath.Join(root, "icon", "99.png")); err != nil || len(cached) == 0 {
		t.Fatalf("空缓存文件未被修复: %v", err)
	}
}

func TestHeadIconHasNoBody(t *testing.T) {
	router, _ := newTestRouter(t, func(context.Context, string) (string, error) {
		return base64.StdEncoding.EncodeToString(pngBody(t)), nil
	})
	w := record(t, router, http.MethodHead, "/images/icon/5.png")
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("HEAD 响应不应带 body，实际 %d 字节", w.Body.Len())
	}
}

func TestSafeRelativePathRejectsTraversal(t *testing.T) {
	for _, raw := range []string{"/../go.mod", "/icon/../../go.mod", `/icon/..\\..\\go.mod`, "/"} {
		if got, err := safeRelativePath(raw); err == nil {
			t.Fatalf("safeRelativePath(%q) = %q, 期望报错", raw, got)
		}
	}
	if got, err := safeRelativePath("/icon/123.png"); err != nil || filepath.ToSlash(got) != "icon/123.png" {
		t.Fatalf("safeRelativePath 正常路径解析错误: %q %v", got, err)
	}
}

// 即使路由匹配到了越界路径，也不能把仓库文件当图片读出来。
func TestTraversalRequestDoesNotLeakFiles(t *testing.T) {
	router, _ := newTestRouter(t, func(context.Context, string) (string, error) {
		return "", ErrIconNotFound
	})
	w := record(t, router, http.MethodGet, "/images/../go.mod")
	if w.Code == http.StatusOK && bytes.Contains(w.Body.Bytes(), []byte("module ")) {
		t.Fatal("路径穿越读到了仓库文件")
	}
}

func TestIconHashFromPath(t *testing.T) {
	cases := []struct {
		path   string
		hash   string
		wanted bool
	}{
		{"icon/123.png", "123", true},
		{"icon/-123.png", "-123", true},
		{"screenshots/123.png", "", false},
		{"icon/12 3.png", "", false},
		{"icon/a$b.png", "", false},
		{"icon/sub/123.png", "", false},
		{"icon/123.jpg", "", false},
		{"icon/.png", "", false},
		{"icon/" + string(make([]byte, 80)) + ".png", "", false},
	}
	for _, tc := range cases {
		hash, ok := iconHashFromPath(tc.path)
		if ok != tc.wanted || hash != tc.hash {
			t.Fatalf("iconHashFromPath(%q) = (%q,%v), want (%q,%v)", tc.path, hash, ok, tc.hash, tc.wanted)
		}
	}
}
