package dashboard

import (
	dashboardhandler "github.com/Autumn-27/ScopeSentry/internal/api/handlers/dashboard"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/common"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/gin-gonic/gin"
)

// RegisterDashboardRoutes 仪表盘路由（对标 ARL-Next dashboard.py）。
//
// 全部只读、全部 WithAuth；sysinfo 用 GET（无请求体），其余用 POST（带查询条件）。
func RegisterDashboardRoutes(api *gin.RouterGroup) {
	dashboardGroup := models.RouteGroup{
		Path: "/dashboard",
		Routes: []models.Route{
			{Method: "POST", Path: "/stats", Handler: dashboardhandler.Stats, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/trend", Handler: dashboardhandler.Trend, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/logs", Handler: dashboardhandler.Logs, Middlewares: common.WithAuth()},
			{Method: "GET", Path: "/sysinfo", Handler: dashboardhandler.SysInfo, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/widgets", Handler: dashboardhandler.Widgets, Middlewares: common.WithAuth()},
		},
	}
	common.RegisterRouteGroup(api, dashboardGroup)
}
