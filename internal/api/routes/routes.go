package routes

import (
	"strings"

	"context"
	"errors"

	"github.com/Autumn-27/ScopeSentry/internal/api/handlers/health"
	"github.com/Autumn-27/ScopeSentry/internal/api/middleware"
	"github.com/Autumn-27/ScopeSentry/internal/api/middleware/compress"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/assets"
	backupRoutes "github.com/Autumn-27/ScopeSentry/internal/api/routes/backup"
	confRoutes "github.com/Autumn-27/ScopeSentry/internal/api/routes/configuration"
	consoleRoutes "github.com/Autumn-27/ScopeSentry/internal/api/routes/console"
	dashboardRoutes "github.com/Autumn-27/ScopeSentry/internal/api/routes/dashboard"
	dictRoutes "github.com/Autumn-27/ScopeSentry/internal/api/routes/dictionary"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/export"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/fingerprint"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/plugin"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/poc"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/project"
	sroutes "github.com/Autumn-27/ScopeSentry/internal/api/routes/sensitive_rule"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/system"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/task"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/user"
	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/database/redis"
	"github.com/gin-gonic/gin"

	apikeyRoutes "github.com/Autumn-27/ScopeSentry/internal/api/routes/apikey"
	fofaRoutes "github.com/Autumn-27/ScopeSentry/internal/api/routes/fofa"
	githubRoutes "github.com/Autumn-27/ScopeSentry/internal/api/routes/github"
	icpRoutes "github.com/Autumn-27/ScopeSentry/internal/api/routes/icp"
	mcpRoutes "github.com/Autumn-27/ScopeSentry/internal/api/routes/mcp"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/node"
)

// loggerWithSkipPaths 自定义日志中间件，支持跳过指定前缀的路径
func loggerWithSkipPaths(skipPrefixes []string) gin.HandlerFunc {
	// 创建默认的日志中间件
	defaultLogger := gin.Logger()

	return func(c *gin.Context) {
		// 检查路径是否需要跳过
		path := c.Request.URL.Path
		for _, prefix := range skipPrefixes {
			if strings.HasPrefix(path, prefix) {
				// 如果路径需要跳过，直接执行下一个中间件，不记录日志
				c.Next()
				return
			}
		}

		// 如果路径不需要跳过，执行默认的日志中间件
		defaultLogger(c)
	}
}

// SetupRouter 设置路由
func SetupRouter() *gin.Engine {
	router := gin.New()

	// 配置日志中间件，跳过 /assets 和 /uploads 路径的日志
	router.Use(loggerWithSkipPaths([]string{"/assets", "/uploads", "/images"}))

	// 添加恢复中间件
	router.Use(gin.Recovery())

	// 添加全局中间件
	router.Use(middleware.I18nMiddleware())
	// 前端内嵌资源：带 hash 的长缓存 + 文本响应 gzip
	router.Use(compress.StaticCache())
	router.Use(compress.Gzip())

	// 探活：K8s readiness/liveness 用；任一依赖不可用返回 503
	router.GET("/healthz", health.Handler(
		func(ctx context.Context) error {
			if mongodb.Client == nil {
				return errors.New("mongo client not initialized")
			}
			return mongodb.Client.Ping(ctx, nil)
		},
		func(ctx context.Context) error {
			if redis.Client == nil {
				return errors.New("redis client not initialized")
			}
			return redis.Client.Ping(ctx).Err()
		},
	))

	// 注册API路由
	api := router.Group("/api")
	{
		// 注册用户路由
		user.RegisterUserRoutes(api)

		// 注册system路由
		system.RegisterSystemRoutes(api)

		// 注册configuration路由
		confRoutes.RegisterConfigurationRoutes(api)

		// 注册asset路由
		assets.RegisterAssetsRoutes(api)

		// 注册fingerprint路由
		fingerprint.RegisterFingerprintRoutes(api)

		// 注册SensitiveRule路由
		sroutes.RegisterSensitiveRuleRoutes(api)

		// 注册节点路由
		node.RegisterNodeRoutes(api)

		task.RegisterTaskRoutes(api)

		project.RegisterProjectRoutes(api)

		poc.RegisterPocRoutes(api)

		export.RegisterExportRoutes(api)

		// 注册全量备份 / 恢复路由
		backupRoutes.RegisterBackupRoutes(api)

		plugin.RegisterPluginRoutes(api)

		// 注册字典路由
		dictRoutes.RegisterDictionaryRoutes(api)

		// 注册 API Key 管理路由
		apikeyRoutes.RegisterApiKeyRoutes(api)

		// 注册 MCP 治理路由（keys / logs / config）
		mcpRoutes.RegisterMcpRoutes(api)

		// 注册 GitHub 泄露 / 威胁情报路由（task / scheduler / result / threat）
		githubRoutes.RegisterGithubRoutes(api)

		// 注册 ICP 企业主体 ↔ 域名查询路由（对标 ARL-Next icp.py）
		icpRoutes.RegisterIcpRoutes(api)

		// 注册 FOFA 批量建任务路由（对标 ARL-Next taskFofa.py）
		fofaRoutes.RegisterFofaRoutes(api)

		// 注册仪表盘路由（对标 ARL-Next dashboard.py）
		dashboardRoutes.RegisterDashboardRoutes(api)

		// 注册控制台信息路由（对标 ARL-Next console.py）
		consoleRoutes.RegisterConsoleRoutes(api)
	}

	return router
}

// 中间件组合函数
