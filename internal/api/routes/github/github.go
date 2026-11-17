package github

import (
	githubhandler "github.com/Autumn-27/ScopeSentry/internal/api/handlers/github"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/common"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/gin-gonic/gin"
)

// RegisterGithubRoutes GitHub 泄露 / 威胁情报面（对标 ARL-Next github_* 路由）。
//
// 全部走 JWT（WithAuth）；pageSize>200 的请求由 handler 直接 400。
func RegisterGithubRoutes(api *gin.RouterGroup) {
	routes := models.RouteGroup{
		Path: "/github",
		Groups: []models.RouteGroup{
			{
				Path: "/task",
				Routes: []models.Route{
					{Method: "POST", Path: "/list", Handler: githubhandler.ListTasks, Middlewares: common.WithAuth()},
					{Method: "POST", Path: "/create", Handler: githubhandler.CreateTask, Middlewares: common.WithAuth()},
					{Method: "POST", Path: "/stop", Handler: githubhandler.StopTasks, Middlewares: common.WithAuth()},
					{Method: "POST", Path: "/delete", Handler: githubhandler.DeleteTasks, Middlewares: common.WithAuth()},
				},
			},
			{
				Path: "/scheduler",
				Routes: []models.Route{
					{Method: "POST", Path: "/list", Handler: githubhandler.ListSchedulers, Middlewares: common.WithAuth()},
					{Method: "POST", Path: "/run", Handler: githubhandler.RunSchedulers, Middlewares: common.WithAuth()},
					{Method: "POST", Path: "/stop", Handler: githubhandler.StopSchedulers, Middlewares: common.WithAuth()},
					{Method: "POST", Path: "/update", Handler: githubhandler.UpdateScheduler, Middlewares: common.WithAuth()},
					{Method: "POST", Path: "/recover", Handler: githubhandler.RecoverSchedulers, Middlewares: common.WithAuth()},
				},
			},
			{
				Path: "/result",
				Routes: []models.Route{
					{Method: "POST", Path: "/list", Handler: githubhandler.ListResults, Middlewares: common.WithAuth()},
					{Method: "POST", Path: "/overview", Handler: githubhandler.OverviewResults, Middlewares: common.WithAuth()},
					{Method: "POST", Path: "/stats", Handler: githubhandler.StatsResults, Middlewares: common.WithAuth()},
					{Method: "POST", Path: "/triage", Handler: githubhandler.TriageResults, Middlewares: common.WithAuth()},
					{Method: "POST", Path: "/ignored_repos", Handler: githubhandler.IgnoredRepos, Middlewares: common.WithAuth()},
					{Method: "GET", Path: "/export", Handler: githubhandler.ExportResults, Middlewares: common.WithAuth()},
				},
			},
			{
				Path: "/monitor_result",
				Routes: []models.Route{
					{Method: "POST", Path: "/list", Handler: githubhandler.ListMonitorResults, Middlewares: common.WithAuth()},
				},
			},
			{
				Path: "/threat",
				Routes: []models.Route{
					{Method: "POST", Path: "/cve_history", Handler: githubhandler.CVEHistory, Middlewares: common.WithAuth()},
					{Method: "POST", Path: "/hackers_target", Handler: githubhandler.HackersTarget, Middlewares: common.WithAuth()},
					{Method: "POST", Path: "/tools_target", Handler: githubhandler.ToolsTarget, Middlewares: common.WithAuth()},
					{Method: "POST", Path: "/cve_config", Handler: githubhandler.CVEConfig, Middlewares: common.WithAuth()},
					{Method: "POST", Path: "/hackers_config", Handler: githubhandler.HackersConfig, Middlewares: common.WithAuth()},
					{Method: "POST", Path: "/tools_config", Handler: githubhandler.ToolsConfig, Middlewares: common.WithAuth()},
					{Method: "POST", Path: "/cve_run_once", Handler: githubhandler.CVERunOnce, Middlewares: common.WithAuth()},
					{Method: "POST", Path: "/hackers_run_once", Handler: githubhandler.HackersRunOnce, Middlewares: common.WithAuth()},
					{Method: "POST", Path: "/tools_run_once", Handler: githubhandler.ToolsRunOnce, Middlewares: common.WithAuth()},
					{Method: "POST", Path: "/token_status", Handler: githubhandler.TokenStatus, Middlewares: common.WithAuth()},
				},
			},
		},
	}
	common.RegisterRouteGroup(api, routes)
}
