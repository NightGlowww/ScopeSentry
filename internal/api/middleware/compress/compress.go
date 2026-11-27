package compress

import (
	"compress/gzip"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// 静态资源路径（与 cmd/main 里的 StaticFS 对应）
var staticPrefixes = []string{
	"/assets", "/css", "/js", "/lib", "/plugins", "/png", "/draco", "/images",
}

// 已经是压缩格式或不需要压缩的扩展名
var skipCompressExt = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	".ico": true, ".woff": true, ".woff2": true, ".ttf": true, ".eot": true,
	".mp4": true, ".webm": true, ".zip": true, ".gz": true, ".br": true,
	".pdf": true, ".xlsx": true, ".xls": true, ".docx": true, ".doc": true,
}

func isStaticPath(path string) bool {
	for _, prefix := range staticPrefixes {
		if strings.HasPrefix(path, prefix+"/") || path == prefix {
			return true
		}
	}
	return false
}

// StaticCache 给内嵌前端资源加缓存头：
// 带 hash 的 /assets/* 可以长缓存，index.html 必须每次协商。
// wantsHTML 判断这次请求是不是「浏览器要一个页面」。
// 只对 HTML 导航加 no-store，接口 JSON 与静态资源不受影响。
func wantsHTML(c *gin.Context) bool {
	return strings.Contains(c.GetHeader("Accept"), "text/html")
}

// cacheOnSuccessWriter 把缓存头推迟到真正写状态码时再决定。
//
// 之前的实现无论 200 还是 404 都给 /assets/* 加 immutable。2026-11-28 实测：
// 发版窗口里浏览器拿到一次 404（chunk 名已随新构建变化），这条 404 被按
// immutable 缓存了一年 —— 之后每次重载都直接用缓存里的 404，/node-management
// 与 /dashboard 永久白屏；同一个 URL 用 fetch(url) 仍是 404，只有
// fetch(url,{cache:'reload'}) 才 200，正是缓存条目被污染的典型表现。
type cacheOnSuccessWriter struct {
	gin.ResponseWriter
	cacheControl string
	wrote        bool
}

func (w *cacheOnSuccessWriter) WriteHeader(code int) {
	if w.wrote {
		w.ResponseWriter.WriteHeader(code)
		return
	}
	w.wrote = true
	if code == http.StatusOK {
		w.Header().Set("Cache-Control", w.cacheControl)
	} else {
		// 失败响应一律不落缓存，否则一次抖动就变成客户端的长期故障
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Del("Expires")
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *cacheOnSuccessWriter) WriteHeaderNow() {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.ResponseWriter.WriteHeaderNow()
}

func (w *cacheOnSuccessWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func StaticCache() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		switch {
		case strings.HasPrefix(path, "/assets/"):
			c.Writer = &cacheOnSuccessWriter{
				ResponseWriter: c.Writer,
				cacheControl:   "public, max-age=31536000, immutable",
			}
		case isStaticPath(path):
			c.Writer = &cacheOnSuccessWriter{
				ResponseWriter: c.Writer,
				cacheControl:   "public, max-age=3600",
			}
		case wantsHTML(c) && !strings.HasPrefix(path, "/api") && !strings.HasPrefix(path, "/mcp"):
			// SPA 的入口 HTML：/、/xxx.html，以及「深链直接返回首页」的那一类路径
			// （/configuration/mcp-audit 这种）。之前深链没有任何 Cache-Control，
			// 浏览器按启发式把旧 index.html 缓存住；发版后它引用的 chunk 名已不存在 →
			// 动态 import 失败 → 整页白屏（2026-11-28）。HTML 一律不落缓存。
			c.Header("Cache-Control", "no-store, must-revalidate")
			c.Header("Pragma", "no-cache")
			c.Header("Expires", "0")
		}
		c.Next()
	}
}

type gzipResponseWriter struct {
	gin.ResponseWriter
	writer      *gzip.Writer
	wroteHeader bool
	skip        bool
}

func (g *gzipResponseWriter) WriteHeader(code int) {
	if !g.wroteHeader {
		g.wroteHeader = true
		header := g.ResponseWriter.Header()
		// 压缩后长度会变，必须删掉原 Content-Length，并声明 Vary
		header.Del("Content-Length")
		header.Set("Content-Encoding", "gzip")
		header.Add("Vary", "Accept-Encoding")
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipResponseWriter) Write(data []byte) (int, error) {
	if !g.wroteHeader {
		g.WriteHeader(http.StatusOK)
	}
	return g.writer.Write(data)
}

func (g *gzipResponseWriter) finish() {
	if !g.wroteHeader {
		// 没有 body（如 204/304），不要留下 Content-Encoding
		g.ResponseWriter.Header().Del("Content-Encoding")
	}
	_ = g.writer.Close()
}

// Gzip 只压文本类响应（JS/CSS/HTML/JSON/SVG…），
// 跳过 HEAD、Range、图片与已压缩格式，避免破坏静态资源的断点续传。
func Gzip() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if c.Request.Method == http.MethodHead ||
			c.Request.Header.Get("Range") != "" ||
			c.Request.Header.Get("Upgrade") != "" ||
			strings.HasPrefix(path, "/mcp") ||
			!strings.Contains(c.Request.Header.Get("Accept-Encoding"), "gzip") ||
			skipCompressExt[strings.ToLower(filepath.Ext(path))] {
			c.Next()
			return
		}

		gz := gzip.NewWriter(c.Writer)
		gw := &gzipResponseWriter{ResponseWriter: c.Writer, writer: gz}
		c.Writer = gw
		defer gw.finish()

		c.Next()
	}
}
