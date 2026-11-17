package assets

import (
	wihhandler "github.com/Autumn-27/ScopeSentry/internal/api/handlers/assets/wih"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/common"
	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// registerWIHRoutes WIH 敏感信息读模型（对标 ARL-Next assetWih）。
// 读现成的 SensitiveResult 集合，不改敏感信息写入链路。
func registerWIHRoutes() models.RouteGroup {
	return models.RouteGroup{
		Path: "/wih",
		Routes: []models.Route{
			{
				Method:      "POST",
				Path:        "/list",
				Handler:     wihhandler.List,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "/detail",
				Handler:     wihhandler.Detail,
				Middlewares: common.WithAuth(),
			},
		},
	}
}
