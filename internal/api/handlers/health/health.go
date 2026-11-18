package health

import (
	"context"
	"net/http"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/constants"
	"github.com/Autumn-27/ScopeSentry/internal/utils/hoststat"
	"github.com/gin-gonic/gin"
)

// PingFunc 允许注入探活实现，便于单测不依赖真实 Mongo/Redis
type PingFunc func(ctx context.Context) error

type checkResult struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func runPing(ctx context.Context, ping PingFunc) checkResult {
	if ping == nil {
		return checkResult{OK: false, Error: "not configured"}
	}
	if err := ping(ctx); err != nil {
		return checkResult{OK: false, Error: err.Error()}
	}
	return checkResult{OK: true}
}

// Handler 返回 /healthz：
// 200 = 依赖都正常；503 = 任一依赖不可用（K8s 的 readiness 依赖这个码）。
func Handler(mongoPing, redisPing PingFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		mongo := runPing(ctx, mongoPing)
		redis := runPing(ctx, redisPing)

		status := "ok"
		code := http.StatusOK
		if !mongo.OK || !redis.OK {
			status = "degraded"
			code = http.StatusServiceUnavailable
		}

		// 主机资源快照：磁盘/负载/内存不足时主动告警，并把磁盘极低视为不健康。
		// 2026-11-19 生产主机失联（sshd 无 banner、HTTP 无响应）就是资源/磁盘层面的问题，
		// 从这里能提前看到，而不是等整机卡死。
		host := hoststat.Stat("/")
		hostWarn := hoststat.Warnings(host)
		if host.DiskTotalBytes > 0 && host.DiskFreePercent < 5 {
			status = "degraded"
			code = http.StatusServiceUnavailable
		}

		c.JSON(code, gin.H{
			"status":  status,
			"version": constants.Version,
			"time":    time.Now().UTC().Format(time.RFC3339),
			"checks": gin.H{
				"mongo": mongo,
				"redis": redis,
			},
			"host":     host,
			"warnings": hostWarn,
		})
	}
}
