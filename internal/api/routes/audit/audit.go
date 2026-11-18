package audit

import (
	audithandler "github.com/Autumn-27/ScopeSentry/internal/api/handlers/audit"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/common"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/gin-gonic/gin"
)

// RegisterAuditRoutes 服务端操作日志（只读）。
func RegisterAuditRoutes(api *gin.RouterGroup) {
	routes := models.RouteGroup{
		Path: "/audit",
		Routes: []models.Route{
			{Method: "POST", Path: "/list", Handler: audithandler.List, Middlewares: common.WithAuth()},
		},
	}
	common.RegisterRouteGroup(api, routes)
}
