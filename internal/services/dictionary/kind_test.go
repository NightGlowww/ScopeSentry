package dictionary

import (
	"errors"
	"strings"
	"testing"
)

// kind 校验：5 个规范值 + 常见别名 + 非法值。
func TestNormalizeKind(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "dir", want: "dir"},
		{in: "DIR", want: "dir"},
		{in: " directory ", want: "dir"},
		{in: "subdomain", want: "subdomain"},
		{in: "sub", want: "subdomain"},
		{in: "domain", want: "subdomain"},
		{in: "brute", want: "brute"},
		{in: "password", want: "brute"},
		{in: "username", want: "brute"},
		{in: "cdn", want: "cdn"},
		{in: "other", want: "other"},
		{in: "misc", want: "other"},
		{in: "", wantErr: true},
		{in: "   ", wantErr: true},
		{in: "evil", wantErr: true},
		{in: "../dir", wantErr: true},
	}
	for _, c := range cases {
		got, err := NormalizeKind(c.in)
		if c.wantErr {
			if err == nil {
				t.Fatalf("NormalizeKind(%q) 期望报错，实际得到 %q", c.in, got)
			}
			if !errors.Is(err, ErrInvalidKind) {
				t.Fatalf("NormalizeKind(%q) 错误类型不是 ErrInvalidKind: %v", c.in, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("NormalizeKind(%q) 意外报错: %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("NormalizeKind(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// 文件名路径穿越必须拒绝：字典名会进 Content-Disposition 与节点目录。
func TestSanitizeDictionaryNameRejectsTraversal(t *testing.T) {
	bad := []string{
		"../etc/passwd",
		"..",
		"..\\windows\\system32",
		"a/../../b",
		"dir/sub",
		"dir\\sub",
		"name\x00with-null",
		"name\nwith-newline",
		"name\rwith-cr",
		"name\twith-tab",
		"",
		"   ",
		strings.Repeat("a", MaxNameLength+1),
	}
	for _, name := range bad {
		if _, err := SanitizeDictionaryName(name); err == nil {
			t.Fatalf("SanitizeDictionaryName(%q) 应当被拒绝", name)
		} else if !errors.Is(err, ErrInvalidName) {
			t.Fatalf("SanitizeDictionaryName(%q) 错误类型不是 ErrInvalidName: %v", name, err)
		}
	}

	good := []string{
		"default",
		"dir-small",
		"dir_small.v2",
		"中文目录字典",
		"sub domains",
	}
	for _, name := range good {
		got, err := SanitizeDictionaryName(name)
		if err != nil {
			t.Fatalf("SanitizeDictionaryName(%q) 意外报错: %v", name, err)
		}
		if got != strings.TrimSpace(name) {
			t.Fatalf("SanitizeDictionaryName(%q) = %q", name, got)
		}
	}
}

// 下载文件名同样不能带出路径分隔符（老库里可能已有脏名字）。
func TestDownloadFilenameSanitizesSeparators(t *testing.T) {
	got := DownloadFilename("dir", "../../etc/passwd")
	if strings.ContainsAny(got, "/\\") {
		t.Fatalf("DownloadFilename 仍含路径分隔符: %q", got)
	}
	if !strings.HasPrefix(got, "dir_") || !strings.HasSuffix(got, ".txt") {
		t.Fatalf("DownloadFilename 格式不对: %q", got)
	}
}

// 行去重：保序、去空白、丢空行。
func TestNormalizeLinesDedupesAndKeepsOrder(t *testing.T) {
	got, err := NormalizeLines([]string{"admin", " admin ", "", "root", "admin", "\r\n", "test"}, MaxAppendLines)
	if err != nil {
		t.Fatalf("NormalizeLines: %v", err)
	}
	want := []string{"admin", "root", "test"}
	if len(got) != len(want) {
		t.Fatalf("NormalizeLines = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("NormalizeLines[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	if _, err := NormalizeLines([]string{"  ", ""}, MaxAppendLines); !errors.Is(err, ErrEmptyLines) {
		t.Fatalf("全空行应当返回 ErrEmptyLines，却得到 %v", err)
	}
	if _, err := NormalizeLines([]string{strings.Repeat("a", MaxLineLength+1)}, MaxAppendLines); !errors.Is(err, ErrLineTooLong) {
		t.Fatalf("超长行应当返回 ErrLineTooLong，却得到 %v", err)
	}
	tooMany := make([]string, MaxAppendLines+1)
	for i := range tooMany {
		tooMany[i] = "line"
	}
	if _, err := NormalizeLines(tooMany, MaxAppendLines); !errors.Is(err, ErrTooManyLines) {
		t.Fatalf("超行数应当返回 ErrTooManyLines，却得到 %v", err)
	}
}

// 分页上限是硬约束：201 必须报错，200 放行；pageIndex 缺省按 1。
func TestNormalizePage(t *testing.T) {
	if _, size, err := NormalizePage(1, 0); err != nil || size != DefaultPageSize {
		t.Fatalf("pageSize=0 应当回落默认值 %d，实际 %d err=%v", DefaultPageSize, size, err)
	}
	if idx, _, err := NormalizePage(0, 10); err != nil || idx != 1 {
		t.Fatalf("pageIndex=0 应当回落 1，实际 %d err=%v", idx, err)
	}
	if _, _, err := NormalizePage(1, MaxPageSize); err != nil {
		t.Fatalf("pageSize=%d 应当放行: %v", MaxPageSize, err)
	}
	if _, _, err := NormalizePage(1, MaxPageSize+1); !errors.Is(err, ErrPageSizeTooLarge) {
		t.Fatalf("pageSize=%d 应当返回 ErrPageSizeTooLarge，实际 %v", MaxPageSize+1, err)
	}
}

// 预览上限 200 行：limit 缺省/超限都按 200 处理。
func TestClampPreviewLimit(t *testing.T) {
	if got := ClampPreviewLimit(0); got != MaxPreviewLines {
		t.Fatalf("ClampPreviewLimit(0) = %d, want %d", got, MaxPreviewLines)
	}
	if got := ClampPreviewLimit(9999); got != MaxPreviewLines {
		t.Fatalf("ClampPreviewLimit(9999) = %d, want %d", got, MaxPreviewLines)
	}
	if got := ClampPreviewLimit(10); got != 10 {
		t.Fatalf("ClampPreviewLimit(10) = %d, want 10", got)
	}
}

// 整份内容归一化：去重 + 兼容 CRLF。
func TestNormalizeContent(t *testing.T) {
	lines, err := normalizeContent("a\r\nb\n\na\n c \n")
	if err != nil {
		t.Fatalf("normalizeContent: %v", err)
	}
	want := []string{"a", "b", "c"}
	if len(lines) != len(want) {
		t.Fatalf("normalizeContent = %v, want %v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("normalizeContent[%d] = %q, want %q", i, lines[i], want[i])
		}
	}

	empty, err := normalizeContent("   \n\n")
	if err != nil {
		t.Fatalf("normalizeContent(空内容) 不该报错: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("normalizeContent(空内容) 应当为空，实际 %v", empty)
	}
}

// 上传模式：缺省 create，append 放行，其它报错。
func TestNormalizeUploadMode(t *testing.T) {
	if mode, err := NormalizeUploadMode(""); err != nil || mode != "create" {
		t.Fatalf("空 mode = %q err=%v，期望 create", mode, err)
	}
	if mode, err := NormalizeUploadMode("APPEND"); err != nil || mode != "append" {
		t.Fatalf("APPEND mode = %q err=%v", mode, err)
	}
	if _, err := NormalizeUploadMode("merge"); !errors.Is(err, ErrInvalidUploadMode) {
		t.Fatalf("merge 应当返回 ErrInvalidUploadMode，实际 %v", err)
	}
}
