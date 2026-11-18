package middleware

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	auditrepo "github.com/Autumn-27/ScopeSentry/internal/repositories/audit"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// 服务端操作日志：只抓「写」请求，异步批量落 audit_logs。
//
// 设计取舍：
//   - 不阻塞请求：进 channel，满了直接丢（日志永远不能成为可用性瓶颈）；
//   - 只记 POST/PUT/DELETE/PATCH，GET 不记，避免把浏览行为灌满集合；
//   - 用户名在 c.Next() 之后取——鉴权中间件在路由级，此时已经写入 context。

var (
	auditOnce   sync.Once
	auditCh     = make(chan models.AuditLog, 2048)
	auditWriter = auditrepo.NewRepository()
)

// AuditLog 记录写请求的操作日志。
func AuditLog() gin.HandlerFunc {
	auditOnce.Do(func() {
		go auditWorker()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := auditWriter.EnsureIndexes(ctx); err != nil {
			logger.Warn("audit: ensure indexes failed", zap.Error(err))
		}
	})

	return func(c *gin.Context) {
		if !shouldAudit(c.Request.Method, c.Request.URL.Path) {
			c.Next()
			return
		}
		start := time.Now()
		c.Next()

		user := ""
		if v, ok := c.Get("username"); ok {
			if s, ok := v.(string); ok {
				user = s
			}
		}
		entry := models.AuditLog{
			User:       user,
			Method:     c.Request.Method,
			Path:       c.Request.URL.Path,
			Status:     c.Writer.Status(),
			DurationMS: time.Since(start).Milliseconds(),
			IP:         c.ClientIP(),
			UA:         c.Request.UserAgent(),
			At:         time.Now(),
		}
		select {
		case auditCh <- entry:
		default:
			// 队列满：丢日志，不影响请求
		}
	}
}

func shouldAudit(method, path string) bool {
	switch method {
	case "POST", "PUT", "DELETE", "PATCH":
	default:
		return false
	}
	if !strings.HasPrefix(path, "/api/") {
		return false
	}
	switch {
	case path == "/api/user/login":
		return false
	case path == "/api/audit/list":
		// 审计查询自身不记，否则每查一次就多一条噪声
		return false
	case strings.HasPrefix(path, "/api/dictionary/upload_large"):
		return false
	}
	return true
}

func auditWorker() {
	batch := make([]models.AuditLog, 0, 200)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := auditWriter.InsertMany(ctx, batch); err != nil {
			logger.Warn("audit: insert failed", zap.Int("count", len(batch)), zap.Error(err))
		}
		cancel()
		batch = batch[:0]
	}

	for {
		select {
		case entry := <-auditCh:
			batch = append(batch, entry)
			if len(batch) >= cap(batch) {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}
