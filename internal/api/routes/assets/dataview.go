package assets

import (
	dv "github.com/Autumn-27/ScopeSentry/internal/api/handlers/assets/dataview"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/common"
	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// registerFileLeakRoutes 文件泄露（对标 ARL-Next asset_fileleak）
func registerFileLeakRoutes() models.RouteGroup {
	return models.RouteGroup{
		Path: "/fileleak",
		Routes: []models.Route{
			{Method: "POST", Path: "/list", Handler: dv.ListFileLeak, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/delete", Handler: dv.DeleteFileLeak, Middlewares: common.WithAuth()},
		},
	}
}

// registerNpocServiceRoutes NPoC 服务（对标 ARL-Next asset_npoc_service）
func registerNpocServiceRoutes() models.RouteGroup {
	return models.RouteGroup{
		Path: "/npoc_service",
		Routes: []models.Route{
			{Method: "POST", Path: "/list", Handler: dv.ListNpoc, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/delete", Handler: dv.DeleteNpoc, Middlewares: common.WithAuth()},
		},
	}
}
