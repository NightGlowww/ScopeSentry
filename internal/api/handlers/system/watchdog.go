package system

// 手动跑一轮流水线巡检（僵尸节点自愈、断流告警）。
// 给运维和验证用：不必等 30 分钟的周期。

import (
	"context"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/services/watchdog"
	"github.com/gin-gonic/gin"
)

// WatchdogRun 立即执行一轮巡检并返回结果。
func WatchdogRun(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	res, err := watchdog.Check(ctx)
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, res, "api.success")
}
