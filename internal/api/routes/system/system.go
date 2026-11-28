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
				// 各集合占用：解释「库为什么这么大 / 备份为什么这么久」
				Method:      "GET",
				Path:        "storage",
				Handler:     system.Storage,
				Middlewares: common.WithAuth(),
			},
			{
				// 大字段瘦身（SensitiveBody / HttpBody）：默认 dry-run，真正执行要 confirm=SLIM
				Method:      "POST",
				Path:        "storage/slim",
				Handler:     system.SlimStorage,
				Middlewares: common.WithAuth(),
			},
			{
				// 多源情报订阅状态（CISA KEV / Wiz / PortSwigger / 先知 / MSRC / P0）。
				Method:      "GET",
				Path:        "intel-feeds",
				Handler:     system.IntelFeeds,
				Middlewares: common.WithAuth(),
			},
			{
				// 开关情报源 / 改抓取间隔。
				Method:      "POST",
				Path:        "intel-feeds/update",
				Handler:     system.IntelFeedsUpdate,
				Middlewares: common.WithAuth(),
			},
			{
				// 立即跑一轮流水线巡检（僵尸节点自愈 / 断流告警）。
				Method:      "POST",
				Path:        "watchdog-run",
				Handler:     system.WatchdogRun,
				Middlewares: common.WithAuth(),
			},
			{
				// 立即刷新情报源（body.id 省略 = 全部启用源）。
				Method:      "POST",
				Path:        "intel-feeds/run",
				Handler:     system.IntelFeedsRun,
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
