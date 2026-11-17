package icp

import (
	icphandler "github.com/Autumn-27/ScopeSentry/internal/api/handlers/icp"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/common"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/gin-gonic/gin"
)

// RegisterIcpRoutes ICP 企业主体 ↔ 域名查询面（对标 ARL-Next icp.py 的 task/asset/sync 系列）。
//
// 全部走 JWT（WithAuth）；pageSize>200 的请求由 handler 直接 400。
func RegisterIcpRoutes(api *gin.RouterGroup) {
	routes := models.RouteGroup{
		Path: "/icp",
		Routes: []models.Route{
			// 任务链
			{Method: "POST", Path: "/task/list", Handler: icphandler.ListTasks, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/task/create", Handler: icphandler.CreateTask, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/task/stop", Handler: icphandler.StopTask, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/task/restart", Handler: icphandler.RestartTask, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/task/resume", Handler: icphandler.ResumeTask, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/task/delete", Handler: icphandler.DeleteTask, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/task/log", Handler: icphandler.TaskLog, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/task/statistics", Handler: icphandler.TaskStatistics, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/task/overview_count", Handler: icphandler.OverviewCount, Middlewares: common.WithAuth()},

			// provider / 结果 / 回填 / 导出
			{Method: "POST", Path: "/tyc_check", Handler: icphandler.TycCheck, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/asset", Handler: icphandler.ListAssets, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/sync", Handler: icphandler.Sync, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/backfill_details", Handler: icphandler.BackfillDetails, Middlewares: common.WithAuth()},
			{Method: "GET", Path: "/export", Handler: icphandler.Export, Middlewares: common.WithAuth()},
		},
	}
	common.RegisterRouteGroup(api, routes)
}
