// services-------------------------------------
// @file      : kind.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2026/11/18 02:10
// -------------------------------------------

package dictionary

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// 字典模块的哨兵错误：handler 用 errors.Is 映射到 400/404，
// 其余未命中哨兵的错误统一走 500。
var (
	ErrInvalidKind        = errors.New("invalid dictionary kind")
	ErrInvalidName        = errors.New("invalid dictionary name")
	ErrDictionaryNotFound = errors.New("dictionary not found")
	ErrDictionaryExists   = errors.New("dictionary already exists")
	ErrPageSizeTooLarge   = errors.New("pageSize exceeds max 200")
	ErrTooManyLines       = errors.New("too many lines in one request")
	ErrEmptyLines         = errors.New("lines cannot be empty")
	ErrLineTooLong        = errors.New("line exceeds max length")
	ErrInvalidUploadMode  = errors.New("invalid upload mode")
	ErrUploadNotFound     = errors.New("upload task not found")
	ErrUploadTooLarge     = errors.New("file exceeds 200MB limit")
	ErrEmptyKeyword       = errors.New("keyword is required")
)

const (
	// MaxPageSize 列表接口单页上限，超出直接 400
	MaxPageSize = 200
	// DefaultPageSize 列表接口默认单页条数
	DefaultPageSize = 20
	// MaxPreviewLines 预览最多返回的行数（ARL-Next 预览也是前 200 行）
	MaxPreviewLines = 200
	// DefaultSearchLimit 行搜索默认条数
	DefaultSearchLimit = 50
	// MaxSearchLimit 行搜索上限
	MaxSearchLimit = 200
	// MaxAppendLines 单次 append 允许的最大行数
	MaxAppendLines = 100000
	// MaxDeleteEntries 单次 delete_entries 允许的最大行数
	MaxDeleteEntries = 100000
	// MaxUploadBytes 单文件上传上限 200MB
	MaxUploadBytes int64 = 200 << 20
	// MaxNameLength 字典名长度上限
	MaxNameLength = 128
	// MaxLineLength 单行长度上限
	MaxLineLength = 4096
)

// kindAliases 别名 → 规范 kind。
// 前端历史数据里有 directory/domain 之类写法，统一收敛到 5 个规范值。
var kindAliases = map[string]string{
	"dir":         models.DictKindDir,
	"dirs":        models.DictKindDir,
	"directory":   models.DictKindDir,
	"directories": models.DictKindDir,
	"dirscan":     models.DictKindDir,
	"dir_scan":    models.DictKindDir,
	"path":        models.DictKindDir,

	"subdomain":  models.DictKindSubdomain,
	"subdomains": models.DictKindSubdomain,
	"sub":        models.DictKindSubdomain,
	"domain":     models.DictKindSubdomain,
	"domains":    models.DictKindSubdomain,
	"dns":        models.DictKindSubdomain,

	"brute":       models.DictKindBrute,
	"brutes":      models.DictKindBrute,
	"bruteforce":  models.DictKindBrute,
	"brute_force": models.DictKindBrute,
	"brute-force": models.DictKindBrute,
	"password":    models.DictKindBrute,
	"passwords":   models.DictKindBrute,
	"passwd":      models.DictKindBrute,
	"pwd":         models.DictKindBrute,
	"username":    models.DictKindBrute,
	"usernames":   models.DictKindBrute,
	"user":        models.DictKindBrute,
	"account":     models.DictKindBrute,
	"accounts":    models.DictKindBrute,
	"cred":        models.DictKindBrute,

	"cdn":      models.DictKindCDN,
	"cdns":     models.DictKindCDN,
	"cdn_dict": models.DictKindCDN,

	"other":   models.DictKindOther,
	"misc":    models.DictKindOther,
	"general": models.DictKindOther,
}

// invalidKindError 带上合法列表，方便前端直接提示
func invalidKindError(raw string) error {
	return fmt.Errorf("%w: %q (allowed: %s)", ErrInvalidKind, raw, strings.Join(models.DictionaryKinds, ", "))
}

// NormalizeKind 把入参 kind 收敛成规范值。
func NormalizeKind(raw string) (string, error) {
	k := strings.ToLower(strings.TrimSpace(raw))
	if k == "" {
		return "", invalidKindError(raw)
	}
	if canonical, ok := kindAliases[k]; ok {
		return canonical, nil
	}
	return "", invalidKindError(raw)
}

// hasForbiddenNameChar 名字里出现即拒绝的字符：路径分隔符、NUL、控制字符。
// 字典内容落 Mongo/GridFS，但名字会进 Content-Disposition 与节点目录，
// 所以按文件名口径校验，避免路径穿越与响应头注入。
func hasForbiddenNameChar(s string) bool {
	for _, r := range s {
		if r == '/' || r == '\\' || r == 0 || r == unicode.ReplacementChar {
			return true
		}
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// SanitizeDictionaryName 校验并归一化字典名（文件名口径）。
func SanitizeDictionaryName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", fmt.Errorf("%w: name is empty", ErrInvalidName)
	}
	if len(name) > MaxNameLength {
		return "", fmt.Errorf("%w: name longer than %d bytes", ErrInvalidName, MaxNameLength)
	}
	// path traversal / 绝对路径 / 反斜杠路径 一律拒绝
	if strings.Contains(name, "..") {
		return "", fmt.Errorf("%w: %q contains path traversal sequence", ErrInvalidName, name)
	}
	if hasForbiddenNameChar(name) {
		return "", fmt.Errorf("%w: %q contains forbidden characters", ErrInvalidName, name)
	}
	return name, nil
}

// DownloadFilename 下载时的安全文件名：<kind>_<name>.txt。
func DownloadFilename(kind, name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == '/' || r == '\\' || r == '"' || r == '\r' || r == '\n':
			b.WriteByte('_')
		case unicode.IsControl(r):
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	return kind + "_" + b.String() + ".txt"
}

// NormalizeLine 行归一化：去首尾空白（含 CR），空行返回 false。
// 不做注释裁剪：'#' 开头的行在 wordlist 里可能是合法条目。
func NormalizeLine(s string) (string, bool) {
	line := strings.TrimSpace(s)
	if line == "" {
		return "", false
	}
	return line, true
}

// NormalizeLines 归一化 + 保序去重；超过 maxLines 返回 ErrTooManyLines。
func NormalizeLines(lines []string, maxLines int) ([]string, error) {
	if maxLines > 0 && len(lines) > maxLines {
		return nil, fmt.Errorf("%w: %d > %d", ErrTooManyLines, len(lines), maxLines)
	}
	seen := make(map[string]struct{}, len(lines))
	out := make([]string, 0, len(lines))
	for _, raw := range lines {
		line, ok := NormalizeLine(raw)
		if !ok {
			continue
		}
		if len(line) > MaxLineLength {
			return nil, fmt.Errorf("%w: %d > %d bytes", ErrLineTooLong, len(line), MaxLineLength)
		}
		if _, dup := seen[line]; dup {
			continue
		}
		seen[line] = struct{}{}
		out = append(out, line)
	}
	if len(out) == 0 {
		return nil, ErrEmptyLines
	}
	return out, nil
}

// SplitContent 把整份内容按行切开（兼容 CRLF / CR）。
func SplitContent(content string) []string {
	if content == "" {
		return nil
	}
	return strings.FieldsFunc(content, func(r rune) bool {
		return r == '\n' || r == '\r'
	})
}

// NormalizePage 归一化分页：pageIndex 缺省按 1，pageSize 超上限返回
// ErrPageSizeTooLarge（列表接口硬约束：>200 直接 400，不静默截断）。
func NormalizePage(pageIndex, pageSize int) (int, int, error) {
	if pageIndex < 1 {
		pageIndex = 1
	}
	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}
	if pageSize > MaxPageSize {
		return 0, 0, fmt.Errorf("%w: %d > %d", ErrPageSizeTooLarge, pageSize, MaxPageSize)
	}
	return pageIndex, pageSize, nil
}

// ClampPreviewLimit 预览条数：默认/上限都是 200，超出按 200 截断。
func ClampPreviewLimit(limit int) int {
	if limit <= 0 {
		return MaxPreviewLines
	}
	if limit > MaxPreviewLines {
		return MaxPreviewLines
	}
	return limit
}

// ClampSearchLimit 搜索条数：默认 50，上限 200。
func ClampSearchLimit(limit int) int {
	if limit <= 0 {
		return DefaultSearchLimit
	}
	if limit > MaxSearchLimit {
		return MaxSearchLimit
	}
	return limit
}

// NormalizeUploadMode create / append，缺省 create。
func NormalizeUploadMode(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", models.DictUploadModeCreate:
		return models.DictUploadModeCreate, nil
	case models.DictUploadModeAppend:
		return models.DictUploadModeAppend, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidUploadMode, raw)
	}
}
