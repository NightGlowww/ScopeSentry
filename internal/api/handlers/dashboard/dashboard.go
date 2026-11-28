package dashboard

import (
	"errors"
	"io"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	dashboardservice "github.com/Autumn-27/ScopeSentry/internal/services/dashboard"
	"github.com/gin-gonic/gin"
)

// bindOptionalJSON 解析请求体；空 body 视为「全用默认值」。
//
// 仪表盘的卡片接口允许前端不带 body 直接 POST（等价于全库 / 默认 7 天 / 第一页），
// 这类请求在 Gin 里会被 ShouldBindJSON 报成 io.EOF，不能当 400 打回去。
// 真正的格式错误（坏 JSON、字段越界）仍然照常 400。
func bindOptionalJSON(c *gin.Context, dst interface{}) error {
	if err := c.ShouldBindJSON(dst); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

var dashboardService dashboardservice.Service

func init() {
	dashboardService = dashboardservice.NewService()
}

// Stats 仪表盘总量统计（对标 ARL-Next /dashboard/stats）
//
// @Summary      仪表盘总量统计
// @Description  资产 / 子域 / URL / 漏洞 / 敏感信息 / 任务 / 节点的总量，project 可选；全部走 countDocuments，不拉文档
// @Tags         仪表盘
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        request  body      models.DashboardStatsRequest  true  "查询条件（project 可选）"
// @Success      200      {object}  response.SuccessResponse{data=models.DashboardStats}
// @Failure      400      {object}  response.BadRequestResponse
// @Failure      401      {object}  response.UnauthorizedResponse
// @Failure      500      {object}  response.InternalServerErrorResponse
// @Router       /api/dashboard/stats [post]
func Stats(c *gin.Context) {
	var req models.DashboardStatsRequest
	if err := bindOptionalJSON(c, &req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	result, err := dashboardService.Stats(c.Request.Context(), req)
	if err != nil {
		// 项目标识写错必须明说：回 200 + 全 0 会让调用方以为「这个项目没有资产」
		if errors.Is(err, dashboardservice.ErrProjectNotFound) {
			response.NotFound(c, "api.not_found", err)
			return
		}
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, result, "api.success")
}

// Trend 仪表盘按天趋势（对标 ARL-Next /dashboard/trend）
//
// @Summary      仪表盘趋势
// @Description  近 7 / 30 天每天新增资产、新增漏洞、完成任务数；没有数据的日期返回 0（服务端补零），时区按配置（默认 Asia/Shanghai）
// @Tags         仪表盘
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        request  body      models.DashboardTrendRequest  true  "days=7|30，project 可选"
// @Success      200      {object}  response.SuccessResponse{data=models.DashboardTrend}
// @Failure      400      {object}  response.BadRequestResponse
// @Failure      401      {object}  response.UnauthorizedResponse
// @Failure      500      {object}  response.InternalServerErrorResponse
// @Router       /api/dashboard/trend [post]
func Trend(c *gin.Context) {
	var req models.DashboardTrendRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	result, err := dashboardService.Trend(c.Request.Context(), req)
	if err != nil {
		if errors.Is(err, dashboardservice.ErrProjectNotFound) {
			response.NotFound(c, "api.not_found", err)
			return
		}
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, result, "api.success")
}

// Logs 服务端日志（对标 ARL-Next /dashboard/logs）
//
// @Summary      服务端日志
// @Description  读进程内环形缓冲或配置的日志文件；level 精确过滤、keyword 子串过滤，按时间倒序分页（pageSize 上限 200）
// @Tags         仪表盘
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        request  body      models.DashboardLogsRequest  true  "level/keyword/pageIndex/pageSize"
// @Success      200      {object}  response.SuccessResponse{data=models.DashboardLogsResult}
// @Failure      400      {object}  response.BadRequestResponse
// @Failure      401      {object}  response.UnauthorizedResponse
// @Failure      500      {object}  response.InternalServerErrorResponse
// @Router       /api/dashboard/logs [post]
func Logs(c *gin.Context) {
	var req models.DashboardLogsRequest
	if err := bindOptionalJSON(c, &req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	result, err := dashboardService.Logs(c.Request.Context(), req)
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, result, "api.success")
}

// SysInfo 系统信息（对标 ARL-Next /dashboard/sysinfo）
//
// @Summary      系统信息
// @Description  CPU / 内存 / 磁盘 / 运行时长时间 / Go 版本 / 协程数；Linux 读 /proc，其它平台返回 available=false 并说明原因
// @Tags         仪表盘
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Success      200  {object}  response.SuccessResponse{data=models.DashboardSysInfo}
// @Failure      401  {object}  response.UnauthorizedResponse
// @Failure      500  {object}  response.InternalServerErrorResponse
// @Router       /api/dashboard/sysinfo [get]
func SysInfo(c *gin.Context) {
	result, err := dashboardService.SysInfo(c.Request.Context())
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, result, "api.success")
}

// Widgets 多卡片聚合（对标 ARL-Next /dashboard/widgets）
//
// @Summary      仪表盘多卡片
// @Description  一次返回 stats + trend + sysinfo，避免前端为每张卡片各发一个请求
// @Tags         仪表盘
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        request  body      models.DashboardWidgetsRequest  true  "days/project/includeSysInfo"
// @Success      200      {object}  response.SuccessResponse{data=models.DashboardWidgets}
// @Failure      400      {object}  response.BadRequestResponse
// @Failure      401      {object}  response.UnauthorizedResponse
// @Failure      500      {object}  response.InternalServerErrorResponse
// @Router       /api/dashboard/widgets [post]
func Widgets(c *gin.Context) {
	var req models.DashboardWidgetsRequest
	if err := bindOptionalJSON(c, &req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	result, err := dashboardService.Widgets(c.Request.Context(), req)
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, result, "api.success")
}
