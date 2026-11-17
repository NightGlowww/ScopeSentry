package assets

import (
	statfingerhandler "github.com/Autumn-27/ScopeSentry/internal/api/handlers/assets/statfinger"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/common"
	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// registerStatFingerRoutes 指纹统计读模型（对标 ARL-Next assetStatFinger）。
// 数据从 asset.technologies 现算，不依赖任何回填集合。
func registerStatFingerRoutes() models.RouteGroup {
	return models.RouteGroup{
		Path: "/stat_finger",
		Routes: []models.Route{
			{
				Method:      "POST",
				Path:        "/list",
				Handler:     statfingerhandler.ListTechnologies,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "/assets",
				Handler:     statfingerhandler.ListAssetsByTechnology,
				Middlewares: common.WithAuth(),
			},
		},
	}
}
