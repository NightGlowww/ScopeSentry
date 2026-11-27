package mcp

import (
	mcphandler "github.com/Autumn-27/ScopeSentry/internal/api/handlers/mcp"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/common"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/gin-gonic/gin"
)

// RegisterMcpRoutes MCP 治理面（对标 ARL-Next mcp keys / logs / config）。
//
// 注意：这组路由是「管理面」，走 JWT（WithAuth）；
// /mcp 本体是「调用面」，走 API Key（internal/api/middleware/apikey.go）。
func RegisterMcpRoutes(api *gin.RouterGroup) {
	routes := models.RouteGroup{
		Path: "/mcp",
		Routes: []models.Route{
			{Method: "GET", Path: "/keys", Handler: mcphandler.ListKeys, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/keys", Handler: mcphandler.CreateKey, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/keys/:id", Handler: mcphandler.UpdateKey, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/keys/:id/status", Handler: mcphandler.SetKeyStatus, Middlewares: common.WithAuth()},
			{Method: "DELETE", Path: "/keys/:id", Handler: mcphandler.DeleteKey, Middlewares: common.WithAuth()},
			{Method: "GET", Path: "/logs", Handler: mcphandler.ListLogs, Middlewares: common.WithAuth()},
			{Method: "GET", Path: "/logs/export", Handler: mcphandler.ExportLogs, Middlewares: common.WithAuth()},
			// 工具 ↔ scope 对照表：建 key 时能看清每个 scope 解锁哪些工具
			{Method: "GET", Path: "/tools", Handler: mcphandler.ToolsCatalog, Middlewares: common.WithAuth()},
			{Method: "GET", Path: "/config", Handler: mcphandler.GetConfig, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/config", Handler: mcphandler.UpdateConfig, Middlewares: common.WithAuth()},
		},
	}
	common.RegisterRouteGroup(api, routes)
}
