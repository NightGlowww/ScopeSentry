// Package routes -----------------------------
// @file      : system.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2025/5/3 22:17
// -------------------------------------------
package system

import (
	"github.com/Autumn-27/ScopeSentry/internal/api/handlers/system"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/common"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/gin-gonic/gin"
)

func RegisterSystemRoutes(api *gin.RouterGroup) {
	// 定义用户路由组
	systemRoutes := models.RouteGroup{
		Path: "/system",
		Routes: []models.Route{
			// 需要认证和user:read权限的路由
			{
				Method:      "GET",
				Path:        "version",
				Handler:     system.GetSystemVersion,
				Middlewares: common.WithAuth(),
			},
			{
				// 产出质量看板：24h 任务/漏洞、LLM 判定分布、隔离区按模板聚合。
				Method:      "GET",
				Path:        "quality-report",
				Handler:     system.QualityReport,
				Middlewares: common.WithAuth(),
			},
			{
				// 立即跑一轮 LLM 批量判定（调参与运维用）。
				Method:      "POST",
				Path:        "jev-run",
				Handler:     system.JevRunOnce,
				Middlewares: common.WithAuth(),
			},
			{
				// 配置自检：模板完整性 / subfinder provider / github 监控 / 节点 / LLM。
				// 只读，不改任何东西。
				Method:      "GET",
				Path:        "config-check",
				Handler:     system.ConfigCheck,
				Middlewares: common.WithAuth(),
			},
		},
	}

	// 注册用户路由组
	common.RegisterRouteGroup(api, systemRoutes)
}
