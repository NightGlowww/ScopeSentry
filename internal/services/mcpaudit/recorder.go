package mcpaudit

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	mcprepo "github.com/Autumn-27/ScopeSentry/internal/repositories/mcp"
	"github.com/Autumn-27/ScopeSentry/internal/services/mcpguard"
	"go.uber.org/zap"
)

// MCP 调用审计写入器。
//
// 约束：主请求路径绝不能被 Mongo 写阻塞，所以 Record 只做一次非阻塞入队；
// 后台单 goroutine 按「满 N 条 或 每 500ms」批量 InsertMany。
// 队列满时直接丢弃并计数（降级），MCP 调用本身仍然成功。
// 写入失败同样只记日志并丢弃该批，不影响调用方。
const (
	auditQueueSize   = 4096
	auditBatchSize   = 200
	auditFlushPeriod = 500 * time.Millisecond
	auditWriteTIMOUT = 5 * time.Second
	janitorPeriod    = 10 * time.Minute
)

// logWriter 便于单测替换（生产实现是 repositories/mcp）
type logWriter interface {
	InsertLogs(ctx context.Context, logs []models.McpCallLog) error
	TrimLogs(ctx context.Context, keep int64) (int64, error)
}

type auditItem struct {
	log models.McpCallLog
	ack chan struct{}
}

type recorder struct {
	writer logWriter

	ch   chan auditItem
	quit chan struct{}
	once sync.Once

	dropped  atomic.Int64
	written  atomic.Int64
	failures atomic.Int64
}

func newRecorder(writer logWriter) *recorder {
	return &recorder{
		writer: writer,
		ch:     make(chan auditItem, auditQueueSize),
		quit:   make(chan struct{}),
	}
}

var defaultRecorder = newRecorder(mcprepo.NewLogRepository())

// Record 非阻塞入队（队列满即丢弃，绝不阻塞 MCP 主请求）
func Record(entry models.McpCallLog) {
	defaultRecorder.record(entry)
}

// Flush 等待当前已入队的记录落库（集成测试与优雅退出用）
func Flush(ctx context.Context) error {
	return defaultRecorder.flush(ctx)
}

func (r *recorder) record(entry models.McpCallLog) {
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
	r.ensure()
	select {
	case r.ch <- auditItem{log: entry}:
	default:
		r.dropped.Add(1)
	}
}

func (r *recorder) flush(ctx context.Context) error {
	r.ensure()
	ack := make(chan struct{})
	select {
	case r.ch <- auditItem{ack: ack}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-ack:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *recorder) ensure() {
	r.once.Do(func() {
		go r.loop()
		go r.janitor()
	})
}

// loop 批量写主循环
func (r *recorder) loop() {
	ticker := time.NewTicker(auditFlushPeriod)
	defer ticker.Stop()

	batch := make([]models.McpCallLog, 0, auditBatchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), auditWriteTIMOUT)
		if err := r.writer.InsertLogs(ctx, batch); err != nil {
			r.failures.Add(int64(len(batch)))
			logger.Warn("mcp audit: flush failed, batch dropped", zap.Int("size", len(batch)), zap.Error(err))
		} else {
			r.written.Add(int64(len(batch)))
		}
		cancel()
		batch = batch[:0]
	}

	for {
		select {
		case it := <-r.ch:
			if it.ack != nil {
				flush()
				close(it.ack)
				continue
			}
			batch = append(batch, it.log)
			if len(batch) >= auditBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-r.quit:
			// 退出前把队列里已有的记录尽量写完
			for {
				select {
				case it := <-r.ch:
					if it.ack != nil {
						flush()
						close(it.ack)
						continue
					}
					batch = append(batch, it.log)
				default:
					flush()
					return
				}
			}
		}
	}
}

// janitor 按配置的保留条数裁剪历史日志（默认 10 万条）
func (r *recorder) janitor() {
	ticker := time.NewTicker(janitorPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			keep := mcpguard.Default().LogRetention(ctx)
			deleted, err := r.writer.TrimLogs(ctx, keep)
			cancel()
			if err != nil {
				logger.Warn("mcp audit: trim logs failed", zap.Error(err))
				continue
			}
			if deleted > 0 {
				logger.Info("mcp audit: trimmed old call logs", zap.Int64("deleted", deleted), zap.Int64("keep", keep))
			}
		case <-r.quit:
			return
		}
	}
}

// stop 停掉后台 goroutine（仅单测使用）
func (r *recorder) stop() {
	select {
	case <-r.quit:
	default:
		close(r.quit)
	}
}

// stats 观测计数（dropped 用于确认降级没有静默丢太多）
func (r *recorder) stats() (written, dropped, failures int64) {
	return r.written.Load(), r.dropped.Load(), r.failures.Load()
}
