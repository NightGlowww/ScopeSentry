// task-------------------------------------
// @file      : task.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2025/6/14 19:40
// -------------------------------------------

package task

import (
	taskHandler "github.com/Autumn-27/ScopeSentry/internal/api/handlers/task"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/common"
	"github.com/Autumn-27/ScopeSentry/internal/models"
)

func registerTaskRoutes() models.RouteGroup {
	return models.RouteGroup{
		Path: "/",
		Routes: []models.Route{
			{
				Method:      "POST",
				Path:        "",
				Handler:     taskHandler.List,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "detail",
				Handler:     taskHandler.TaskDetail,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "add",
				Handler:     taskHandler.AddTask,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "delete",
				Handler:     taskHandler.DeleteTask,
				Middlewares: common.WithAuth(),
			},
			{
				// 按当前筛选批量删除（必须带搜索词或状态）
				Method:      "POST",
				Path:        "deleteByFilter",
				Handler:     taskHandler.DeleteByFilter,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "retest",
				Handler:     taskHandler.RetestTask,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "stop",
				Handler:     taskHandler.StopTask,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "start",
				Handler:     taskHandler.StartTask,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "dispatch-stats",
				Handler:     taskHandler.DispatchRejectStats,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "progress/info",
				Handler:     taskHandler.ProgressInfo,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "sync",
				Handler:     taskHandler.SyncToProject,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "target/pause",
				Handler:     taskHandler.PauseTarget,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "target/resume",
				Handler:     taskHandler.ResumeTarget,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "GET",
				Path:        "names",
				Handler:     taskHandler.GetAllTaskNames,
				Middlewares: common.WithAuth(),
			},
		},
	}
}
