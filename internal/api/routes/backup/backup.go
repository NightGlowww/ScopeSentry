// backup-------------------------------------
// @file      : backup.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2026/11/18 01:32
// -------------------------------------------

package backup

import (
	backupHandler "github.com/Autumn-27/ScopeSentry/internal/api/handlers/backup"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/common"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/gin-gonic/gin"
)

// RegisterBackupRoutes 注册 /api/backup 全量备份与恢复路由
func RegisterBackupRoutes(api *gin.RouterGroup) {
	routes := models.RouteGroup{
		Path: "/backup",
		Routes: []models.Route{
			{
				Method:      "POST",
				Path:        "/create",
				Handler:     backupHandler.Create,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "GET",
				Path:        "/list",
				Handler:     backupHandler.List,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "GET",
				Path:        "/status",
				Handler:     backupHandler.Status,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "GET",
				Path:        "/download/:name",
				Handler:     backupHandler.Download,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "DELETE",
				Path:        "/:name",
				Handler:     backupHandler.Delete,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "/restore",
				Handler:     backupHandler.Restore,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "/upload",
				Handler:     backupHandler.Upload,
				Middlewares: common.WithAuth(),
			},
		},
	}
	common.RegisterRouteGroup(api, routes)
}
