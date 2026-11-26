// @title           ScopeSentry API
// @version         1.0
// @description     ScopeSentry 是一个安全扫描平台
// @termsOfService  http://swagger.io/terms/

// @contact.name   API Support
// @contact.url    http://www.swagger.io/support
// @contact.email  support@swagger.io

// @license.name  Apache 2.0
// @license.url   http://www.apache.org/licenses/LICENSE-2.0.html

// @host      localhost:8082
// @BasePath  /api

// @securityDefinitions.apikey  ApiKeyAuth
// @in                         header
// @name                       Authorization
// @description               Bearer token for authentication

package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"github.com/Autumn-27/ScopeSentry/internal/scheduler/jobs"
	"io/fs"
	"strconv"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/services/assetcollapse"
	"github.com/Autumn-27/ScopeSentry/internal/services/intel/versionprobe"
	"github.com/Autumn-27/ScopeSentry/internal/services/intelfeed"
	"github.com/Autumn-27/ScopeSentry/internal/services/jev"
	taskreconcile "github.com/Autumn-27/ScopeSentry/internal/services/task/reconcile"
	"github.com/Autumn-27/ScopeSentry/internal/services/vulnverify"
	"github.com/Autumn-27/ScopeSentry/internal/services/vulnverify/autoclean"
	"github.com/Autumn-27/ScopeSentry/internal/services/vulnverify/pocquality"
	"github.com/Autumn-27/ScopeSentry/internal/services/wafcontext"
	"github.com/Autumn-27/ScopeSentry/internal/services/watchdog"
	"github.com/Autumn-27/ScopeSentry/internal/worker"

	"github.com/Autumn-27/ScopeSentry/internal/bootstrap"
	"github.com/Autumn-27/ScopeSentry/internal/config"
	"github.com/Autumn-27/ScopeSentry/internal/scheduler"
	"github.com/Autumn-27/ScopeSentry/internal/update"

	"github.com/Autumn-27/ScopeSentry/internal/logger"

	_ "github.com/Autumn-27/ScopeSentry/internal/database/mongodb"

	_ "github.com/Autumn-27/ScopeSentry/internal/database/redis"

	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/constants"

	_ "github.com/Autumn-27/ScopeSentry/internal/bootstrap"

	imageRoutes "github.com/Autumn-27/ScopeSentry/internal/api/handlers/images"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes"
	scopesentrymcp "github.com/Autumn-27/ScopeSentry/internal/mcp"
	iconrepo "github.com/Autumn-27/ScopeSentry/internal/repositories/assets/icon"
	dictionarysvc "github.com/Autumn-27/ScopeSentry/internal/services/dictionary"
	githubsvc "github.com/Autumn-27/ScopeSentry/internal/services/github"
	redisLogSubscriber "github.com/Autumn-27/ScopeSentry/internal/services/redis_log_subscriber"
	schedulerSvc "github.com/Autumn-27/ScopeSentry/internal/services/task/scheduler"
	taskSvc "github.com/Autumn-27/ScopeSentry/internal/services/task/task"

	"github.com/Autumn-27/ScopeSentry/docs"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"
)

//go:embed static/*
var embeddedFiles embed.FS

func main() {
	// 设置 Gin 模式
	if config.GlobalConfig.Server.Mode == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	err := os.Setenv("TZ", config.GlobalConfig.System.Timezone)
	if err != nil {
		return
	}
	if constants.Version == "" {
		constants.Version = "dev"
	}
	fmt.Printf("version: %v\n", constants.Version)
	PrintPlugin()
	err = update.Update()
	if err != nil {
		logger.Error("Update failed", zap.Error(err))
	}
	bootstrap.GetProjectList()
	// 初始化计划任务
	scheduler.InitializeGlobalScheduler()
	scheduler.GetGlobalScheduler().Start()
	defer scheduler.GetGlobalScheduler().Stop()

	// GitHub 情报调度：注册到内置 gocron（每分钟 tick，按各自配置间隔执行）
	githubScheduler := githubsvc.DefaultScheduler()
	githubScheduler.Start()
	defer githubScheduler.Stop()

	// 字典行索引补建：老字典只有文件、没有行索引，列表 lineCount 会一直是 0
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		n, err := dictionarysvc.NewManageService().BackfillEntryIndex(ctx)
		if err != nil {
			logger.Warn("dictionary: backfill entry index failed", zap.Error(err))
			return
		}
		if n > 0 {
			logger.Info("dictionary: backfilled entry index", zap.Int("count", n))
		}
	}()

	// 页面监控定时任务自愈：ScheduledTasks 里 page_monitoring.state=true 但 scheduled 里没有 job 时补注册。
	// 历史默认不注册，用户加了监控 URL 也永远不会被调度。
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := schedulerSvc.NewService().EnsurePageMonitoringJob(ctx); err != nil {
			logger.Warn("ensure page_monitoring job failed", zap.Error(err))
		}
	}()

	// 启动自愈：把「状态=运行中但没人跑」的任务重新投递给节点。
	// 场景：发布/重启服务端时，Redis 里的目标列表或队列消息过期，任务会停在
	// progress<100 且无人消费，用户只看到「运行中 0%」，只能手动暂停再开始。
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if n, err := taskSvc.NewService().ResumeRunningTasks(ctx); err != nil {
			logger.Warn("resume running tasks failed", zap.Error(err))
		} else if n > 0 {
			logger.Info(fmt.Sprintf("resumed %d running task(s) after restart", n))
		}
	}()

	// POC 每日同步自愈：设置 POC_SYNC_URL 后，确保 scheduledTasks 里有 poc_sync 记录。
	// 不设置时什么都不做（默认不联网）。
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := jobs.EnsurePOCSyncJob(ctx); err != nil {
			logger.Warn("ensure poc_sync job failed", zap.Error(err))
		}
	}()

	// 任务进度巡检：每 30 秒重算运行中任务的进度与终态，并把僵尸任务判 failed。
	// 此前进度只在「有人打开任务列表」或节点上报 Task end 时才更新，没人看页面就一直显示 0%。
	go func() {
		svc := taskSvc.NewService()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			if err := svc.TaskProgress(ctx); err != nil {
				logger.Warn("task progress tick failed", zap.Error(err))
			}
			// 90 分钟：实测单模块（nuclei 全量模板）跑 20-60 分钟，15 分钟会把正常扫描打成失败。
			// 需要更激进/更保守可用 STALE_TASK_MINUTES 覆盖。
			staleAfter := 90 * time.Minute
			if v := strings.TrimSpace(os.Getenv("STALE_TASK_MINUTES")); v != "" {
				if mins, perr := strconv.Atoi(v); perr == nil && mins >= 15 && mins <= 720 {
					staleAfter = time.Duration(mins) * time.Minute
				}
			}
			if n, err := svc.SweepStaleTasks(ctx, staleAfter); err == nil && n > 0 {
				logger.Info("stale tasks marked failed", zap.Int("count", n))
			}
			cancel()
		}
	}()

	// 启动 Redis 日志订阅服务（在后台运行）
	go redisLogSubscriber.SubscribeLogChannel()
	// 创建路由
	router := routes.SetupRouter()
	scopesentrymcp.RegisterRoutes(router)
	gin.DisableConsoleColor()
	//router.Use(logger.GinLogger(), logger.GinRecovery())

	// 准备前端文件系统
	frontendFS, _ := fs.Sub(embeddedFiles, "static")
	// 准备静态资源文件系统（assets目录）
	assetsFS, _ := fs.Sub(embeddedFiles, "static/assets")
	// 准备其他静态资源文件系统
	cssFS, _ := fs.Sub(embeddedFiles, "static/css")
	dracoFS, _ := fs.Sub(embeddedFiles, "static/draco")
	jsFS, _ := fs.Sub(embeddedFiles, "static/js")
	libFS, _ := fs.Sub(embeddedFiles, "static/lib")
	pluginsFS, _ := fs.Sub(embeddedFiles, "static/plugins")
	pngFS, _ := fs.Sub(embeddedFiles, "static/png")

	// 注册 Swagger 路由（在静态文件之前，避免冲突）
	docs.SwaggerInfo.BasePath = "/api"
	router.GET("/api/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// 注册静态资源路由（/assets 映射到 static/assets 目录）
	router.StaticFS("/assets", http.FS(assetsFS)) // /assets 对应前端 js/css

	// 注册其他静态资源路由
	router.StaticFS("/css", http.FS(cssFS))         // /css 对应 static/css 目录
	router.StaticFS("/draco", http.FS(dracoFS))     // /draco 对应 static/draco 目录
	router.StaticFS("/js", http.FS(jsFS))           // /js 对应 static/js 目录
	router.StaticFS("/lib", http.FS(libFS))         // /lib 对应 static/lib 目录
	router.StaticFS("/plugins", http.FS(pluginsFS)) // /plugins 对应 static/plugins 目录
	router.StaticFS("/png", http.FS(pngFS))         // /png 对应 static/png 目录

	// 注册 /map 路由，映射到 map.html
	router.GET("/map", func(c *gin.Context) {
		data, err := fs.ReadFile(frontendFS, "map.html")
		if err != nil {
			c.String(http.StatusInternalServerError, "Failed to read map.html")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", data)
	})

	// 注册 /favicon.ico 路由
	router.GET("/favicon.ico", func(c *gin.Context) {
		data, err := fs.ReadFile(frontendFS, "favicon.ico")
		if err != nil {
			c.String(http.StatusNotFound, "Favicon not found")
			return
		}
		c.Data(http.StatusOK, "image/x-icon", data)
	})

	// /images 下截图走本地目录；网站图标由扫描节点写进共享 MongoDB（icon 集合），
	// 服务端在首次请求时回源并缓存，见 internal/api/handlers/images。
	imageRoutes.RegisterRoutes(router, config.GlobalConfig.System.ImgDir, loadIconContent)

	// 注册根路径路由，直接返回文件内容，避免重定向
	router.GET("/", func(c *gin.Context) {
		data, err := fs.ReadFile(frontendFS, "index.html")
		if err != nil {
			c.String(http.StatusInternalServerError, "Failed to read index.html")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", data)
	})

	// 处理所有未匹配的路由，返回前端首页（用于SPA路由）
	router.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		// 排除 API 路径和静态资源路径
		if strings.HasPrefix(path, "/api") || strings.HasPrefix(path, "/mcp") {
			c.JSON(http.StatusNotFound, gin.H{"error": "Not found"})
			return
		}
		// 先看内嵌静态目录里有没有这个文件（logo.png / robots.txt 等根文件以前会掉到
		// SPA 兜底里，浏览器拿到的是 text/html，<img> 解码失败显示成裂图）。
		if rel := strings.TrimPrefix(path, "/"); rel != "" {
			if info, err := fs.Stat(frontendFS, rel); err == nil && !info.IsDir() {
				c.FileFromFS(rel, http.FS(frontendFS))
				return
			}
		}
		// 如果请求的是静态资源但未找到，返回404而不是首页
		if strings.HasPrefix(path, "/assets") || strings.HasPrefix(path, "/uploads") ||
			strings.HasPrefix(path, "/css") || strings.HasPrefix(path, "/draco") ||
			strings.HasPrefix(path, "/js") || strings.HasPrefix(path, "/lib") ||
			strings.HasPrefix(path, "/plugins") || strings.HasPrefix(path, "/png") ||
			strings.HasPrefix(path, "/images") {
			c.JSON(http.StatusNotFound, gin.H{"error": "File not found"})
			return
		}
		// 其他路径返回前端首页（SPA路由）
		data, err := fs.ReadFile(frontendFS, "index.html")
		if err != nil {
			c.String(http.StatusInternalServerError, "Failed to read index.html")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", data)
	})

	// 创建 HTTP 服务器
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", config.GlobalConfig.Server.Port),
		Handler: router,
	}

	// 运行资产处理
	worker.Run()

	// 漏洞证据复验：把模板 matcher 对着已存响应重算，不成立的隔离后删除。
	// 2026-11-24 起：76 条历史发现按判据复验全部不成立，人工清了；这里让后续自动清。
	autoclean.Start(context.Background())
	pocquality.Start(context.Background())
	assetcollapse.Start(context.Background())
	wafcontext.Start(context.Background())
	// 复核结果推进业务状态：把已 verified 的记录标成「已确认」，把模板已排除的挪进隔离区。
	// 与 autoclean 的循环里那次调用是幂等的，这里保证即使 VULN_VERIFY=off 也能跑一次。
	if _, _, err := vulnverify.PromoteOnce(context.Background()); err != nil {
		logger.Error("vulnverify promote: " + err.Error())
	}
	// Jev 第二裁判：机械复验判不了的候选（skipped/weak）交给它按概率落状态。
	jev.Start(context.Background())
	// 流水线断流告警：任务在跑却没产出时，30 分钟内就能在 quality-report 里看到。
	// 2026-11-26 的教训——两天没产出，靠人问才发现。
	watchdog.Start(context.Background())
	// 多源情报订阅：CISA KEV / Wiz / PortSwigger / 先知社区 / MSRC / Project Zero。
	// 每 30 分钟检查一次，各源按自己的 intervalHours 到期才抓。
	intelfeed.Start(context.Background())
	// 任务状态对账：把「排队中 / 孤儿」从「运行中」里摘出来。
	taskreconcile.Start(context.Background())
	// 版本补全：intel_for_assets 的价值全在 with_version 上，而存量响应头大多没有版本号。
	// 这里按 20 分钟一批给「有指纹没版本」的资产主动读一次版本。
	versionprobe.Start(context.Background(), os.Getenv("VERSIONPROBE") == "off")

	// 在 goroutine 中启动服务器
	go func() {
		logger.Info("Starting server", zap.Int("port", config.GlobalConfig.Server.Port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Failed to start server", zap.Error(err))
		}
	}()

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("Shutting down server...")

	// 设置关闭超时
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 优雅关闭服务器
	if err := srv.Shutdown(ctx); err != nil {
		logger.Fatal("Server forced to shutdown", zap.Error(err))
	}

	logger.Info("Server exiting")
}

func Banner() {
	banner := "   _____                         _____            _              \n  / ____|                       / ____|          | |             \n | (___   ___ ___  _ __   ___  | (___   ___ _ __ | |_ _ __ _   _ \n  \\___ \\ / __/ _ \\| '_ \\ / _ \\  \\___ \\ / _ \\ '_ \\| __| '__| | | |\n  ____) | (_| (_) | |_) |  __/  ____) |  __/ | | | |_| |  | |_| |\n |_____/ \\___\\___/| .__/ \\___| |_____/ \\___|_| |_|\\__|_|   \\__, |\n                  | |                                       __/ |\n                  |_|                                      |___/ "
	fmt.Println(banner)
}

// loadIconContent 把仓库层的「没有这条记录」翻译成 HTTP 层的 404。
//
// 扫描节点（ScopeSentry-Scan）与服务端不共享文件系统：favicon 只会写进共享 MongoDB 的
// icon 集合，服务端磁盘上的 images/icon 目录长期是空的。查询在
// internal/repositories/assets/icon，解码与缓存策略在 internal/api/handlers/images。
func loadIconContent(ctx context.Context, hash string) (string, error) {
	content, err := iconrepo.NewRepository().FindContent(ctx, hash)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return "", imageRoutes.ErrIconNotFound
		}
		return "", err
	}
	return content, nil
}

func PrintPlugin() {
	fmt.Println(strings.Repeat("=", 50))
	fmt.Println("✨✨✨ IMPORTANT NOTICE: Please review the Plugin Key below ✨✨✨")
	fmt.Println(strings.Repeat("=", 50))
	fmt.Printf("🔑 Plugin Key: %s\n", config.GlobalConfig.System.PluginKey)
	fmt.Println(strings.Repeat("=", 50))
	fmt.Println("✅ Ensure the Plugin Key is correctly copied!")
}
