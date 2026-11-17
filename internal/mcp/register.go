package mcp

import (
	"net/http"

	"github.com/Autumn-27/ScopeSentry/internal/api/middleware"
	"github.com/Autumn-27/ScopeSentry/internal/services/mcpguard"
	"github.com/gin-gonic/gin"
	mcpSDK "github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterRoutes 注册 MCP Streamable HTTP 路由。
//
// 治理挂载点：/mcp 组统一过 MCPAPIKeyMiddleware（key 状态 / 过期 / scope /
// 工具白名单 / 限速 / 审计），通过后才进入 SDK handler。
// 中间件把「该 key 可用的工具集合」写进 request context，这里取出来传给
// newServer 做 tools/list 裁剪（nil = 不限制）。
func RegisterRoutes(router *gin.Engine) {
	handler := mcpSDK.NewStreamableHTTPHandler(
		func(req *http.Request) *mcpSDK.Server {
			return newServer(mcpguard.PermittedToolsFrom(req.Context()))
		},
		&mcpSDK.StreamableHTTPOptions{Stateless: true},
	)

	mcpGroup := router.Group("/mcp")
	mcpGroup.Use(middleware.MCPAPIKeyMiddleware())
	mcpGroup.Any("", gin.WrapH(handler))
	mcpGroup.Any("/*filepath", gin.WrapH(handler))
}
