package fofa

import (
	fofahandler "github.com/Autumn-27/ScopeSentry/internal/api/handlers/fofa"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/common"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/gin-gonic/gin"
)

// RegisterFofaRoutes FOFA 批量建任务面（对标 ARL-Next taskFofa.py）。
//
// 全部走 JWT（WithAuth）；导入历史 pageSize>200 的请求由 handler 直接 400。
func RegisterFofaRoutes(api *gin.RouterGroup) {
	routes := models.RouteGroup{
		Path: "/fofa",
		Routes: []models.Route{
			{Method: "POST", Path: "/status", Handler: fofahandler.Status, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/search", Handler: fofahandler.Search, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/task", Handler: fofahandler.CreateTasks, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/task/list", Handler: fofahandler.ListImports, Middlewares: common.WithAuth()},
		},
	}
	common.RegisterRouteGroup(api, routes)
}
