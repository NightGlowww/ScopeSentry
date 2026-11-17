package dashboard

import (
	"context"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/config"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// 日志源：
//   - file   ：配了 logs.file_name 的部署，直接读日志文件（只读尾部窗口，不整文件读进内存）；
//   - memory ：默认部署（日志只进 stdout）读进程内环形缓冲（logger.ring）；
//   - none   ：两边都没有数据。
const (
	logSourceFile   = "file"
	logSourceMemory = "memory"
	logSourceNone   = "none"

	// maxLogReadBytes 单次最多读的日志尾部字节数。
	// 8 MiB 按每行 200 字节算约 4 万行，足够撑满 200 条 × 多页；
	// 更大的文件只读尾部，并在响应里标 truncated，避免一次请求把文件全读进内存。
	maxLogReadBytes = 8 << 20
)

// ansiEscape zap 的 console encoder 给级别加了颜色（CapitalColorLevelEncoder），
// 落到文件里是 ESC[...m。解析前先剥掉，否则 level 字段永远对不上。
var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

// Logs 服务端日志分页 + 过滤。
//
// 顺序固定为「新的在前」：运维看日志就是看最近发生了什么。
func (s *service) Logs(ctx context.Context, req models.DashboardLogsRequest) (*models.DashboardLogsResult, error) {
	pageIndex, pageSize := normalizeLogPage(req.PageIndex, req.PageSize)

	entries, source, truncated := collectLogEntries()
	filtered := filterLogEntries(entries, req.Level, req.Keyword)
	list, total := paginateNewestFirst(filtered, pageIndex, pageSize)
	if list == nil {
		list = []models.DashboardLogEntry{}
	}

	return &models.DashboardLogsResult{
		List:      list,
		Total:     total,
		Page:      pageIndex,
		PageSize:  pageSize,
		Source:    source,
		Truncated: truncated,
	}, nil
}

// collectLogEntries 选一个日志源并读出全部条目（时间升序）。
func collectLogEntries() ([]models.DashboardLogEntry, string, bool) {
	if path := strings.TrimSpace(config.GlobalConfig.Log.Filename); path != "" {
		if entries, truncated, err := readLogFile(path, maxLogReadBytes); err == nil {
			return entries, logSourceFile, truncated
		}
		// 文件配了但读不到（还没创建 / 刚被 rotate 掉）：退回环形缓冲，
		// 不让仪表盘在没有文件的那一刻整块空掉。这里不写日志，避免自己刷自己。
	}

	recent := logger.RecentLogs()
	entries := make([]models.DashboardLogEntry, 0, len(recent))
	for _, item := range recent {
		entries = append(entries, models.DashboardLogEntry{
			Time:    item.Time,
			Level:   item.Level,
			Caller:  item.Caller,
			Message: item.Message,
		})
	}
	if len(entries) == 0 {
		return entries, logSourceNone, false
	}
	return entries, logSourceMemory, logger.LogsTruncated()
}

// readLogFile 读日志文件的尾部窗口并解析。
// 返回值第二个是 truncated：文件比窗口大时为 true（更早的日志没有返回）。
func readLogFile(path string, maxBytes int64) ([]models.DashboardLogEntry, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	size := stat.Size()

	var offset int64
	truncated := false
	if size > maxBytes {
		offset = size - maxBytes
		truncated = true
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, false, err
	}

	remaining := size - offset
	if remaining < 0 {
		remaining = 0
	}
	data := make([]byte, remaining)
	if _, err := io.ReadFull(f, data); err != nil && err != io.ErrUnexpectedEOF {
		return nil, false, err
	}

	lines := strings.Split(string(data), "\n")
	if truncated && len(lines) > 0 {
		// 从中间切进去的第一行大概率是半行，丢掉
		lines = lines[1:]
	}
	return parseLogLines(lines), truncated, nil
}

// parseLogLines 把 zap console encoder 的行解析成条目。
//
// zap 的输出形状： time \t level \t caller \t msg [\t {json fields}]
// 不是这个形状的行按「续行」处理（堆栈、多行 message），拼到上一条的 message 后面，
// 这样日志里不会出现一堆没有时间戳的碎行。
func parseLogLines(lines []string) []models.DashboardLogEntry {
	out := make([]models.DashboardLogEntry, 0, len(lines))
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		clean := ansiEscape.ReplaceAllString(line, "")

		parts := strings.SplitN(clean, "\t", 4)
		if len(parts) == 4 && isLogTimestamp(parts[0]) {
			out = append(out, models.DashboardLogEntry{
				Time:    strings.TrimSpace(parts[0]),
				Level:   strings.ToLower(strings.TrimSpace(parts[1])),
				Caller:  strings.TrimSpace(parts[2]),
				Message: strings.TrimSpace(parts[3]),
			})
			continue
		}

		if len(out) > 0 {
			last := &out[len(out)-1]
			last.Message = last.Message + "\n" + clean
			continue
		}
		// 第一条就不是标准行：保留成一条无时间戳的记录，别把内容吞掉
		out = append(out, models.DashboardLogEntry{Message: clean})
	}
	return out
}

// isLogTimestamp 判断字段是不是 "2006-01-02 15:04:05"（也接受 RFC3339 前缀）
func isLogTimestamp(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 19 {
		return false
	}
	if s[4] != '-' || s[7] != '-' {
		return false
	}
	if s[10] != ' ' && s[10] != 'T' {
		return false
	}
	return s[13] == ':' && s[16] == ':'
}

// filterLogEntries 级别精确匹配 + 关键字子串（message / caller，大小写不敏感）
func filterLogEntries(entries []models.DashboardLogEntry, level, keyword string) []models.DashboardLogEntry {
	level = strings.ToLower(strings.TrimSpace(level))
	kw := strings.ToLower(strings.TrimSpace(keyword))

	if level == "" && kw == "" {
		return entries
	}

	out := make([]models.DashboardLogEntry, 0, len(entries))
	for _, entry := range entries {
		if level != "" && strings.ToLower(entry.Level) != level {
			continue
		}
		if kw != "" &&
			!strings.Contains(strings.ToLower(entry.Message), kw) &&
			!strings.Contains(strings.ToLower(entry.Caller), kw) {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// paginateNewestFirst 按「新的在前」分页：总条数返回全量，列表只返回当前页。
func paginateNewestFirst(entries []models.DashboardLogEntry, pageIndex, pageSize int) ([]models.DashboardLogEntry, int64) {
	total := int64(len(entries))
	start := (pageIndex - 1) * pageSize
	if start >= len(entries) {
		return []models.DashboardLogEntry{}, total
	}
	end := start + pageSize
	if end > len(entries) {
		end = len(entries)
	}

	out := make([]models.DashboardLogEntry, 0, end-start)
	for i := start; i < end; i++ {
		out = append(out, entries[len(entries)-1-i])
	}
	return out, total
}

// normalizeLogPage 归一化分页参数：pageIndex>=1，pageSize 默认 50 / 上限 200。
// handler 的 binding 已经把 pageSize>200 拒成 400，这里的兜底只防内部调用绕过 binding。
func normalizeLogPage(pageIndex, pageSize int) (int, int) {
	if pageIndex < 1 {
		pageIndex = 1
	}
	if pageSize <= 0 {
		pageSize = defaultLogPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return pageIndex, pageSize
}
