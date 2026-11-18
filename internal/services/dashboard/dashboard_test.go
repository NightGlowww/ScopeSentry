package dashboard

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// 趋势补零：窗口内每一天都要出现，没有数据的那天是 0，顺序按日期升序。
func TestBuildTrendPointsZeroFill(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	start := time.Date(2026, 11, 12, 0, 0, 0, 0, loc)

	asset := map[string]int64{
		"2026-11-12": 3,
		"2026-11-18": 5, // 中间几天故意缺数据
	}
	vuln := map[string]int64{"2026-11-15": 2}
	task := map[string]int64{"2026-11-18": 1}

	points, totals := buildTrendPoints(models.DashboardTrendDays7, start, asset, vuln, task)

	if len(points) != 7 {
		t.Fatalf("points = %d, want 7", len(points))
	}
	wantDates := []string{
		"2026-11-12", "2026-11-13", "2026-11-14", "2026-11-15",
		"2026-11-16", "2026-11-17", "2026-11-18",
	}
	for i, want := range wantDates {
		if points[i].Date != want {
			t.Fatalf("points[%d].Date = %q, want %q", i, points[i].Date, want)
		}
	}

	// 有数据的天
	if points[0].Asset != 3 {
		t.Errorf("11-12 asset = %d, want 3", points[0].Asset)
	}
	if points[3].Vulnerability != 2 {
		t.Errorf("11-15 vulnerability = %d, want 2", points[3].Vulnerability)
	}
	if points[6].Asset != 5 || points[6].Task != 1 {
		t.Errorf("11-18 = %+v, want asset=5 task=1", points[6])
	}

	// 补零的天：13/14/16/17 三列全 0
	for _, idx := range []int{1, 2, 4, 5} {
		p := points[idx]
		if p.Asset != 0 || p.Vulnerability != 0 || p.Task != 0 {
			t.Errorf("%s should be all zero, got %+v", p.Date, p)
		}
	}

	if totals.Asset != 8 || totals.Vulnerability != 2 || totals.Task != 1 {
		t.Fatalf("totals = %+v, want asset=8 vuln=2 task=1", totals)
	}

	// 列式视图（ARL-Next 兼容）：长度与下标必须和 points 对齐
	labels, assets, vulns, tasks := trendSeries(points)
	if len(labels) != 7 || len(assets) != 7 || len(vulns) != 7 || len(tasks) != 7 {
		t.Fatalf("series lengths = %d/%d/%d/%d, want 7", len(labels), len(assets), len(vulns), len(tasks))
	}
	for i, point := range points {
		if labels[i] != point.Date || assets[i] != point.Asset ||
			vulns[i] != point.Vulnerability || tasks[i] != point.Task {
			t.Fatalf("series[%d] = {%s %d %d %d} != point %+v",
				i, labels[i], assets[i], vulns[i], tasks[i], point)
		}
	}
}

// 30 天窗口同样要补齐 30 个点
func TestBuildTrendPointsThirtyDays(t *testing.T) {
	loc := time.UTC
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, loc)
	points, totals := buildTrendPoints(models.DashboardTrendDays30, start,
		map[string]int64{"2026-11-30": 4}, nil, nil)

	if len(points) != 30 {
		t.Fatalf("points = %d, want 30", len(points))
	}
	if points[len(points)-1].Date != "2026-11-30" || points[len(points)-1].Asset != 4 {
		t.Fatalf("last point = %+v, want 2026-11-30/4", points[len(points)-1])
	}
	if totals.Asset != 4 {
		t.Fatalf("totals.Asset = %d, want 4", totals.Asset)
	}
}

// 日志行解析：标准行 / 颜色码 / 续行
func TestParseLogLines(t *testing.T) {
	lines := []string{
		"2026-11-18 10:00:00\tinfo\tapi/handler.go:42\tasset list ok",
		"\x1b[31m2026-11-18 10:00:01\tERROR\x1b[0m\tservices/task.go:88\tboom happened",
		"\tat foo.Bar()",
		"\tat baz.Qux()",
		"2026-11-18 10:00:02\twarn\tapi/x.go:7\tmsg with\tanother tab",
		"",
	}

	entries := parseLogLines(lines)
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3 (%+v)", len(entries), entries)
	}

	if entries[0].Level != "info" || entries[0].Caller != "api/handler.go:42" || entries[0].Message != "asset list ok" {
		t.Fatalf("entries[0] = %+v", entries[0])
	}

	// ANSI 颜色码要剥掉，否则 level 是 "\x1b[31mERROR\x1b[0m"
	if entries[1].Level != "error" {
		t.Fatalf("entries[1].Level = %q, want error", entries[1].Level)
	}
	// 两条续行并进上一条 message
	if !strings.Contains(entries[1].Message, "boom happened") ||
		!strings.Contains(entries[1].Message, "at foo.Bar()") ||
		!strings.Contains(entries[1].Message, "at baz.Qux()") {
		t.Fatalf("entries[1].Message = %q, want stack lines merged", entries[1].Message)
	}
	// message 里本身带 tab 时不能被拆掉（SplitN 4）
	if entries[2].Message != "msg with\tanother tab" {
		t.Fatalf("entries[2].Message = %q", entries[2].Message)
	}
}

// 第一条就不是标准行时也不能丢内容
func TestParseLogLinesKeepsLeadingGarbage(t *testing.T) {
	entries := parseLogLines([]string{"panic: something bad"})
	if len(entries) != 1 || entries[0].Message != "panic: something bad" {
		t.Fatalf("entries = %+v", entries)
	}
}

// 级别过滤 + 关键字过滤
func TestFilterLogEntries(t *testing.T) {
	entries := []models.DashboardLogEntry{
		{Time: "2026-11-18 10:00:00", Level: "info", Caller: "api/a.go:1", Message: "Subdomain scan started"},
		{Time: "2026-11-18 10:00:01", Level: "error", Caller: "api/b.go:2", Message: "mongo timeout"},
		{Time: "2026-11-18 10:00:02", Level: "warn", Caller: "services/c.go:3", Message: "slow query"},
	}

	if got := filterLogEntries(entries, "error", ""); len(got) != 1 || got[0].Message != "mongo timeout" {
		t.Fatalf("level filter = %+v", got)
	}
	if got := filterLogEntries(entries, "", "TIMEOUT"); len(got) != 1 || got[0].Level != "error" {
		t.Fatalf("keyword filter (case-insensitive) = %+v", got)
	}
	// 关键字也匹配 caller
	if got := filterLogEntries(entries, "", "services/c"); len(got) != 1 || got[0].Level != "warn" {
		t.Fatalf("caller keyword filter = %+v", got)
	}
	if got := filterLogEntries(entries, "debug", ""); len(got) != 0 {
		t.Fatalf("no-match filter = %+v", got)
	}
	if got := filterLogEntries(entries, "", ""); len(got) != 3 {
		t.Fatalf("empty filter should keep all, got %d", len(got))
	}
}

// 分页：新的在前
func TestPaginateNewestFirst(t *testing.T) {
	entries := []models.DashboardLogEntry{
		{Message: "m1"}, {Message: "m2"}, {Message: "m3"}, {Message: "m4"}, {Message: "m5"},
	}

	list, total := paginateNewestFirst(entries, 1, 2)
	if total != 5 {
		t.Fatalf("total = %d, want 5", total)
	}
	if len(list) != 2 || list[0].Message != "m5" || list[1].Message != "m4" {
		t.Fatalf("page1 = %+v, want [m5 m4]", list)
	}

	list, _ = paginateNewestFirst(entries, 3, 2)
	if len(list) != 1 || list[0].Message != "m1" {
		t.Fatalf("page3 = %+v, want [m1]", list)
	}

	list, _ = paginateNewestFirst(entries, 4, 2)
	if len(list) != 0 {
		t.Fatalf("page4 = %+v, want empty", list)
	}

	list, _ = paginateNewestFirst(nil, 1, 2)
	if list == nil || len(list) != 0 {
		t.Fatalf("empty entries should return empty slice, got %+v", list)
	}
}

// 分页参数归一化：默认 50、上限 200、pageIndex 至少 1
func TestNormalizeLogPage(t *testing.T) {
	cases := []struct {
		inPage, inSize     int
		wantPage, wantSize int
	}{
		{0, 0, 1, defaultLogPageSize},
		{-3, -1, 1, defaultLogPageSize},
		{2, 20, 2, 20},
		{1, 200, 1, 200},
		{1, 500, 1, maxPageSize}, // 兜底截断（HTTP 层已经先 400）
	}
	for _, c := range cases {
		page, size := normalizeLogPage(c.inPage, c.inSize)
		if page != c.wantPage || size != c.wantSize {
			t.Fatalf("normalizeLogPage(%d,%d) = (%d,%d), want (%d,%d)",
				c.inPage, c.inSize, page, size, c.wantPage, c.wantSize)
		}
	}
}

// 日志文件读取：小窗口触发 truncated，且从中间切进去的半行要丢掉
func TestReadLogFileTailTruncatesAndDropsPartialLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.log")

	var b strings.Builder
	for i := 0; i < 200; i++ {
		b.WriteString("2026-11-18 11:00:00\tinfo\tpkg/a.go:1\tline ")
		b.WriteString(strings.Repeat("x", 20))
		b.WriteString("\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}

	all, truncated, err := readLogFile(path, 1<<20)
	if err != nil {
		t.Fatalf("readLogFile(full): %v", err)
	}
	if truncated {
		t.Fatalf("small file should not be truncated")
	}
	if len(all) != 200 {
		t.Fatalf("full read entries = %d, want 200", len(all))
	}

	// 只读最后 300 字节：能读到几行，且必须 truncated
	tail, truncated, err := readLogFile(path, 300)
	if err != nil {
		t.Fatalf("readLogFile(tail): %v", err)
	}
	if !truncated {
		t.Fatalf("tail read should be truncated")
	}
	if len(tail) == 0 || len(tail) >= 200 {
		t.Fatalf("tail read entries = %d, want a small non-empty window", len(tail))
	}
	for _, entry := range tail {
		if entry.Level != "info" {
			t.Fatalf("tail entry parsed wrong (partial line not dropped?): %+v", entry)
		}
		if !strings.HasPrefix(entry.Message, "line ") {
			t.Fatalf("tail entry message = %q", entry.Message)
		}
	}
}

// 日志文件不存在时返回错误（service 会退回环形缓冲）
func TestReadLogFileMissing(t *testing.T) {
	if _, _, err := readLogFile(filepath.Join(t.TempDir(), "nope.log"), 1024); err == nil {
		t.Fatalf("missing file should return an error")
	}
}

// 时区：默认配置是 Asia/Shanghai
func TestDashboardLocationDefaultsToShanghai(t *testing.T) {
	loc, name := dashboardLocation()
	if loc == nil {
		t.Fatalf("location is nil")
	}
	if name != "Asia/Shanghai" {
		t.Fatalf("timezone = %q, want Asia/Shanghai", name)
	}
	if _, offset := time.Now().In(loc).Zone(); offset != 8*3600 {
		t.Fatalf("offset = %d, want +08:00", offset)
	}
}

// 趋势窗口只接受 7 / 30
func TestTrendRejectsOtherWindows(t *testing.T) {
	svc := NewService()
	if _, err := svc.Trend(context.TODO(), models.DashboardTrendRequest{Days: 5}); err != ErrInvalidDays {
		t.Fatalf("Trend(days=5) err = %v, want ErrInvalidDays", err)
	}
}
