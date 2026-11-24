// Package images 提供资产页用到的图片：截图与网站图标（favicon）。
//
// 数据来源有两条，必须分开处理：
//
//   - 截图：扫描节点把 PNG 写进 MongoDB 的 screenshot 集合，服务端迁移时落盘，
//     这里按 /images/screenshots/<hash>.png 直接读本地目录。
//   - 图标：扫描节点（ScopeSentry-Scan）与服务端不共享文件系统，它把 favicon 以
//     base64 写进共享 MongoDB 的 icon 集合（fav3 → content）。服务端的 images/icon
//     目录因此长期是空的，前端 /images/icon/<hash>.png 全部 404，页面显示破图。
//
// 这里在图标文件缺失时回源 icon 集合，解码校验后原子写入本地缓存目录，
// 之后同 hash 的请求都是磁盘读。缓存落在 $EXEDIR/images/icon（容器里是挂载卷），
// 所以重建容器不会丢；需要强制重新拉取时删掉对应 .png 即可。
package images

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	// maxEncodedIconBytes / maxDecodedIconBytes 限制单次回源的内存占用：
	// favicon 正常在 100KB 以内，超过上限基本可以判定是脏数据或非图片内容。
	maxEncodedIconBytes = 8 << 20
	maxDecodedIconBytes = 6 << 20

	// iconLookupTimeout 挡住 MongoDB 慢查询把页面请求一起拖住。
	iconLookupTimeout = 3 * time.Second

	// 图标文件名即内容哈希，内容不会变，可以长期缓存。
	iconCacheControl = "public, max-age=86400, immutable"
)

// ErrIconNotFound 表示本地缓存和扫描节点都没有这个图标（正常情况，对应 404）。
var ErrIconNotFound = errors.New("icon not found")

// IconContentLookup 返回扫描节点记录的 favicon base64 内容。
// 找不到时必须返回 ErrIconNotFound，其它错误按基础设施故障处理。
type IconContentLookup func(ctx context.Context, hash string) (string, error)

// iconHashPattern 允许数字（含 mmh3 的负值）与常见的十六进制/字母哈希写法，
// 同时排除 / \ $ . 等会被当成路径或查询运算符的字符。
var iconHashPattern = regexp.MustCompile(`^[0-9A-Za-z_-]{1,64}$`)

// Handler 负责 /images/* 的读写。lookupIcon 由调用方注入，便于测试与替换数据源。
type Handler struct {
	root       string
	fileServer http.Handler
	lookupIcon IconContentLookup
}

// NewHandler 创建图片处理器；lookupIcon 为 nil 时视为「只有本地文件」。
func NewHandler(root string, lookupIcon IconContentLookup) *Handler {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		absRoot = filepath.Clean(root)
	}
	if lookupIcon == nil {
		lookupIcon = func(context.Context, string) (string, error) {
			return "", ErrIconNotFound
		}
	}
	return &Handler{
		root:       absRoot,
		fileServer: http.FileServer(http.Dir(absRoot)),
		lookupIcon: lookupIcon,
	}
}

// RegisterRoutes 把 /images 挂到路由上。GET 与 HEAD 都注册：Gin 的 Static
// 也是这么做的，替换后不可少 HEAD，否则预取/探测会拿到 405。
func RegisterRoutes(router gin.IRoutes, root string, lookupIcon IconContentLookup) {
	handler := NewHandler(root, lookupIcon)
	router.GET("/images/*filepath", handler.serve)
	router.HEAD("/images/*filepath", handler.serve)
}

func (h *Handler) serve(c *gin.Context) {
	relative, err := safeRelativePath(c.Param("filepath"))
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	if hash, ok := iconHashFromPath(relative); ok {
		h.serveIcon(c, hash, filepath.Join(h.root, relative))
		return
	}
	h.serveFile(c, relative)
}

// serveFile 保持原来 router.Static 的语义：只回真实存在的文件，不列目录。
func (h *Handler) serveFile(c *gin.Context, relative string) {
	target := filepath.Join(h.root, relative)
	info, err := os.Stat(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.Status(http.StatusNotFound)
			return
		}
		c.Status(http.StatusInternalServerError)
		return
	}
	if info.IsDir() {
		c.Status(http.StatusNotFound)
		return
	}
	// 交给标准库处理 Range / If-Modified-Since / Content-Type 探测。
	req := c.Request.Clone(c.Request.Context())
	req.URL.Path = "/" + filepath.ToSlash(relative)
	h.fileServer.ServeHTTP(c.Writer, req)
}

func (h *Handler) serveIcon(c *gin.Context, hash, cachePath string) {
	// 命中缓存直接出。空文件、或历史脏写（内容不是图片）一律当未命中，避免永久破图。
	if data, err := os.ReadFile(cachePath); err == nil {
		if contentType := detectImageContentType(data); contentType != "" {
			writeIcon(c, data, contentType)
			return
		}
		_ = os.Remove(cachePath)
	} else if !errors.Is(err, os.ErrNotExist) {
		c.Status(http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), iconLookupTimeout)
	defer cancel()
	encoded, err := h.lookupIcon(ctx, hash)
	if err != nil {
		if errors.Is(err, ErrIconNotFound) {
			c.Status(http.StatusNotFound)
			return
		}
		// 数据库不可用是服务端问题，回 503 才能在浏览器网络面板里区分于「确实没有图标」。
		log.Printf("images: icon lookup failed for hash %s: %v", hash, err)
		c.Status(http.StatusServiceUnavailable)
		return
	}

	// 先校验再落盘：脏数据不进缓存目录。
	decoded, contentType, err := decodeIcon(encoded)
	if err != nil {
		log.Printf("images: stored icon %s is unusable: %v", hash, err)
		c.Status(http.StatusNotFound)
		return
	}
	if err := cacheIcon(cachePath, decoded); err != nil {
		// 缓存失败不影响本次响应，下次请求重新回源即可。
		log.Printf("images: cannot cache icon %s: %v", hash, err)
	}
	writeIcon(c, decoded, contentType)
}

func writeIcon(c *gin.Context, data []byte, contentType string) {
	c.Header("Cache-Control", iconCacheControl)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Type", contentType)
	http.ServeContent(c.Writer, c.Request, "", time.Time{}, bytes.NewReader(data))
}

// safeRelativePath 把 URL 路径归一成相对 root 的本地路径，拒绝任何 .. 片段。
func safeRelativePath(raw string) (string, error) {
	raw = strings.TrimPrefix(raw, "/")
	for _, segment := range strings.Split(strings.ReplaceAll(raw, `\`, "/"), "/") {
		if segment == ".." {
			return "", errors.New("path traversal")
		}
	}
	clean := path.Clean("/" + raw)
	if clean == "/" || clean == "/." {
		return "", errors.New("empty path")
	}
	return filepath.FromSlash(strings.TrimPrefix(clean, "/")), nil
}

// iconHashFromPath 只对 icon/<hash>.png 生效；其它路径（截图等）一律走静态文件。
func iconHashFromPath(relative string) (string, bool) {
	slashPath := filepath.ToSlash(relative)
	if !strings.HasPrefix(slashPath, "icon/") || strings.Contains(strings.TrimPrefix(slashPath, "icon/"), "/") {
		return "", false
	}
	name := strings.TrimPrefix(slashPath, "icon/")
	if !strings.HasSuffix(name, ".png") {
		return "", false
	}
	hash := strings.TrimSuffix(name, ".png")
	if !iconHashPattern.MatchString(hash) {
		return "", false
	}
	return hash, true
}

// decodeIcon 解码扫描节点写入的 base64。扫描端写的是裸 base64，
// 历史数据可能带 data URI 前缀，两种都接受；解出来不是图片则判为脏数据。
func decodeIcon(encoded string) ([]byte, string, error) {
	encoded = strings.TrimSpace(encoded)
	if strings.HasPrefix(encoded, "data:image/") {
		comma := strings.IndexByte(encoded, ',')
		if comma < 0 || !strings.Contains(encoded[:comma], ";base64") {
			return nil, "", errors.New("unsupported data URI")
		}
		encoded = encoded[comma+1:]
	}
	if encoded == "" || len(encoded) > maxEncodedIconBytes {
		return nil, "", errors.New("empty or oversized icon payload")
	}

	var data []byte
	var err error
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if data, err = encoding.DecodeString(encoded); err == nil {
			break
		}
	}
	if err != nil {
		return nil, "", err
	}
	if len(data) == 0 || len(data) > maxDecodedIconBytes {
		return nil, "", errors.New("empty or oversized decoded icon")
	}
	// 类型判定放在这里，保证「能落盘的必然是图片」。
	contentType := detectImageContentType(data)
	if contentType == "" {
		return nil, "", errors.New("stored content is not a supported image")
	}
	return data, contentType, nil
}

// detectImageContentType 只放行真正的图片字节。浏览器对 <img> 里的 ICO/SVG
// 是支持的，但 http.DetectContentType 会把 SVG 判成 text/plain，所以单独识别。
func detectImageContentType(data []byte) string {
	if len(data) >= 4 && data[0] == 0 && data[1] == 0 && (data[2] == 1 || data[2] == 2) && data[3] == 0 {
		return "image/x-icon"
	}
	trimmed := bytes.TrimSpace(data)
	if bytes.HasPrefix(trimmed, []byte("<svg")) ||
		(bytes.HasPrefix(trimmed, []byte("<?xml")) && bytes.Contains(trimmed, []byte("<svg"))) {
		return "image/svg+xml"
	}
	if contentType := http.DetectContentType(data); strings.HasPrefix(contentType, "image/") {
		return contentType
	}
	return ""
}

// cacheIcon 先写临时文件再改名：并发请求下不会读到半个文件，
// 进程中途退出也不会留下损坏的 .png。
func cacheIcon(filename string, data []byte) error {
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".icon-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, filename)
}
