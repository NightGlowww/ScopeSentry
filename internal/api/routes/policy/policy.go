package policy

import (
	policyhandler "github.com/Autumn-27/ScopeSentry/internal/api/handlers/policy"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/common"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/gin-gonic/gin"
)

// RegisterPolicyRoutes 扫描策略面（对标 ARL-Next policy.py 的可复用扫描参数）。
//
// 全部走 JWT（WithAuth）；pageSize>200 由 handler 直接 400。
func RegisterPolicyRoutes(api *gin.RouterGroup) {
	routes := models.RouteGroup{
		Path: "/policy",
		Routes: []models.Route{
			{Method: "POST", Path: "/list", Handler: policyhandler.List, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/create", Handler: policyhandler.Create, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/update", Handler: policyhandler.Update, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/delete", Handler: policyhandler.Delete, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/detail", Handler: policyhandler.Detail, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/apply", Handler: policyhandler.Apply, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/test", Handler: policyhandler.Test, Middlewares: common.WithAuth()},
		},
	}
	common.RegisterRouteGroup(api, routes)
}
