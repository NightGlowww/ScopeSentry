package logger

import (
	"sync"
	"time"

	"go.uber.org/zap/zapcore"

	"github.com/Autumn-27/ScopeSentry/internal/config"
)

// 服务端日志的进程内环形缓冲（对标 ARL-Next dashboard 的日志卡片）。
//
// 为什么不是「读日志文件」：默认部署没有配 logs.file_name（日志只进 stdout，
// 由 docker logs 收），读文件会读到一个空。所以在写日志的主链路上挂一个
// 有界环形缓冲，/api/dashboard/logs 直接读它；配了 file_name 的部署仍然读文件
// （见 services/dashboard 的日志源选择）。
//
// 容量：默认 2000 条，按时间淘汰最旧的。每条只存格式化后的 4 个字段，
// 不存 zap 的 field 对象，避免把业务对象引用挂在 logger 上。

// ringCapacity 环形缓冲容量（条）
const ringCapacity = 2000

// LogEntry 一条可展示的服务端日志
type LogEntry struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Caller  string `json:"caller,omitempty"`
	Message string `json:"message"`
}

// ringBuffer 有界环形缓冲，并发安全
type ringBuffer struct {
	mu      sync.RWMutex
	entries []LogEntry
	next    int  // 下一次写入位置
	full    bool // 是否已经绕过一圈（此时 next 指向最旧的条目）
}

func newRing(capacity int) *ringBuffer {
	if capacity <= 0 {
		capacity = ringCapacity
	}
	return &ringBuffer{entries: make([]LogEntry, capacity)}
}

// add 追加一条，缓冲满时覆盖最旧的一条
func (r *ringBuffer) add(e LogEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.entries[r.next] = e
	r.next++
	if r.next >= len(r.entries) {
		r.next = 0
		r.full = true
	}
}

// snapshot 按时间升序返回当前所有条目（copy，调用方可自由处理）
func (r *ringBuffer) snapshot() []LogEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.full {
		out := make([]LogEntry, r.next)
		copy(out, r.entries[:r.next])
		return out
	}

	out := make([]LogEntry, 0, len(r.entries))
	out = append(out, r.entries[r.next:]...)
	out = append(out, r.entries[:r.next]...)
	return out
}

// 进程内唯一实例：包级变量，在所有 init() 之前完成初始化，
// 所以 logger.init 里 tee 进 zap 时一定已经可用。
var ringLogs = newRing(ringCapacity)

// RecentLogs 返回环形缓冲里的日志（旧 -> 新）。dashboard 的 memory 日志源用它。
func RecentLogs() []LogEntry {
	return ringLogs.snapshot()
}

// LogsTruncated 环形缓冲是否已经绕过一圈（最早的日志已被覆盖）。
// dashboard 用它填 logs 响应的 truncated 字段，避免前端把「只有最近 2000 条」
// 当成「全部日志」。
func LogsTruncated() bool {
	ringLogs.mu.RLock()
	defer ringLogs.mu.RUnlock()
	return ringLogs.full
}

// ringCore 把 zap 日志写进环形缓冲的 Core 实现。
//
// Enabled 恒 true（>= Debug）：级别过滤交给查询侧（/api/dashboard/logs?level=），
// 这样改配置里的日志级别不会让 dashboard 里少掉一类历史记录。
type ringCore struct{}

func newRingCore() zapcore.Core { return ringCore{} }

func (c ringCore) Enabled(lvl zapcore.Level) bool {
	return lvl >= zapcore.DebugLevel
}

// With 不保留 field：缓冲里要的是人读的一行，不是结构化上下文
func (c ringCore) With(_ []zapcore.Field) zapcore.Core { return c }

func (c ringCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}

func (c ringCore) Write(ent zapcore.Entry, _ []zapcore.Field) error {
	caller := ""
	if ent.Caller.Defined {
		caller = ent.Caller.TrimmedPath()
	}
	ringLogs.add(LogEntry{
		Time:    formatRingTime(ent.Time),
		Level:   ent.Level.String(),
		Caller:  caller,
		Message: ent.Message,
	})
	return nil
}

func (c ringCore) Sync() error { return nil }

// logLocation 缓存配置时区。加载失败时退回本地时区（与 logger.init 的口径一致）。
var logLocation struct {
	once sync.Once
	loc  *time.Location
}

func formatRingTime(t time.Time) string {
	logLocation.once.Do(func() {
		loc, err := time.LoadLocation(config.GlobalConfig.System.Timezone)
		if err != nil {
			loc = time.Local
		}
		logLocation.loc = loc
	})
	loc := logLocation.loc
	if loc == nil {
		loc = time.Local
	}
	return t.In(loc).Format("2006-01-02 15:04:05")
}
