// project_aggregation-------------------------------------
// 项目详情页聚合接口（Overview / 子域名 / 端口 / 服务）。
// 对应前端 frontend/src/api/ProjectAggregation/index.ts 的 7 个调用点。
// -------------------------------------------

package project_aggregation

import (
	handler "github.com/Autumn-27/ScopeSentry/internal/api/handlers/project_aggregation"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/common"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/gin-gonic/gin"
)

// RegisterProjectAggregationRoutes 注册 /api/project_aggregation 路由组
func RegisterProjectAggregationRoutes(api *gin.RouterGroup) {
	auth := common.WithAuth()
	group := models.RouteGroup{
		Path: "/project_aggregation",
		Routes: []models.Route{
			{Method: "POST", Path: "/project/info", Handler: handler.GetProjectInfo, Middlewares: auth},
			{Method: "POST", Path: "/project/asset/count", Handler: handler.GetProjectAssetCount, Middlewares: auth},
			{Method: "POST", Path: "/project/vul/statistics", Handler: handler.GetProjectVulStatistics, Middlewares: auth},
			{Method: "POST", Path: "/project/vul/data", Handler: handler.GetProjectVulData, Middlewares: auth},
			{Method: "POST", Path: "/project/subdomain/data", Handler: handler.GetProjectSubdomainData, Middlewares: auth},
			{Method: "POST", Path: "/project/port/data", Handler: handler.GetProjectPortData, Middlewares: auth},
			{Method: "POST", Path: "/project/service/data", Handler: handler.GetProjectServiceData, Middlewares: auth},
		},
	}
	common.RegisterRouteGroup(api, group)
}
