package console

import (
	consolehandler "github.com/Autumn-27/ScopeSentry/internal/api/handlers/console"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/common"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/gin-gonic/gin"
)

// RegisterConsoleRoutes 控制台信息路由（对标 ARL-Next console.py 的 console/info）。
func RegisterConsoleRoutes(api *gin.RouterGroup) {
	consoleGroup := models.RouteGroup{
		Path: "/console",
		Routes: []models.Route{
			{Method: "GET", Path: "/info", Handler: consolehandler.Info, Middlewares: common.WithAuth()},
		},
	}
	common.RegisterRouteGroup(api, consoleGroup)
}
