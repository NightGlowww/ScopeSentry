package assets

import (
	"github.com/Autumn-27/ScopeSentry/internal/api/handlers/assets/statistics"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/common"
	"github.com/Autumn-27/ScopeSentry/internal/models"
)

func registerStatisticsRoutes() models.RouteGroup {
	return models.RouteGroup{
		Path: "/statistics",
		Routes: []models.Route{
			{
				Method:      "GET",
				Path:        "",
				Handler:     statistics.GetAssetStatisticsData,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "/port",
				Handler:     statistics.GetAssetPortStatistics,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "/service",
				Handler:     statistics.GetAssetServiceStatistics,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "/app",
				Handler:     statistics.GetAssetAppStatistics,
				Middlewares: common.WithAuth(),
			},
			{
				// handler 和 service 早就写好了，只是路由没注册：
				// 项目概况页的「标题」面板因此永远显示空（2026-11-27 实测）
				Method:      "POST",
				Path:        "/title",
				Handler:     statistics.GetAssetTitleStatistics,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "/icon",
				Handler:     statistics.GetAssetIconStatistics,
				Middlewares: common.WithAuth(),
			},
		},
	}
}
