package console

import (
	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	dashboardservice "github.com/Autumn-27/ScopeSentry/internal/services/dashboard"
	"github.com/Autumn-27/ScopeSentry/internal/utils/hoststat"
	"github.com/gin-gonic/gin"
)

// 控制台信息复用 dashboard 的读模型（同一批计数口径、同一套依赖探活），
// 单独开一个 handler 包只是为了让路由前缀与 ARL-Next 的 /console/info 对齐。
var dashboardService dashboardservice.Service

func init() {
	dashboardService = dashboardservice.NewService()
}

// Info 控制台信息（对标 ARL-Next console/info）
//
// @Summary      控制台信息
// @Description  版本、启动时间、运行时长、节点数、任务数、Mongo/Redis 连接状态
// @Tags         控制台
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Success      200  {object}  response.SuccessResponse{data=models.ConsoleInfo}
// @Failure      401  {object}  response.UnauthorizedResponse
// @Failure      500  {object}  response.InternalServerErrorResponse
// @Router       /api/console/info [get]
func Info(c *gin.Context) {
	result, err := dashboardService.ConsoleInfo(c.Request.Context())
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	// 主机资源：磁盘/负载/内存，前端系统状态页直接展示
	h := hoststat.Stat("/")
	result.HostStats = &models.HostStats{
		CPUs:                h.CPUs,
		Load1:               h.Load1,
		DiskFreeBytes:       h.DiskFreeBytes,
		DiskTotalBytes:      h.DiskTotalBytes,
		DiskFreePercent:     h.DiskFreePercent,
		MemAvailablePercent: h.MemAvailablePercent,
	}
	result.HostWarnings = hoststat.Warnings(h)
	response.Success(c, result, "api.success")
}
