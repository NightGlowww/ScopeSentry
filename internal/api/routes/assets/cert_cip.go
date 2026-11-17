package assets

import (
	assetoverviewhandler "github.com/Autumn-27/ScopeSentry/internal/api/handlers/assets/assetoverview"
	certHandler "github.com/Autumn-27/ScopeSentry/internal/api/handlers/assets/cert"
	cipHandler "github.com/Autumn-27/ScopeSentry/internal/api/handlers/assets/cip"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/common"
	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// registerCertRoutes 证书读模型（对标 ARL-Next assetCert）。
// 数据从 asset 的 TLS 字段现算，不依赖任何回填集合。
func registerCertRoutes() models.RouteGroup {
	return models.RouteGroup{
		Path: "/cert",
		Routes: []models.Route{
			{
				Method:      "POST",
				Path:        "/list",
				Handler:     certHandler.List,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "/detail",
				Handler:     certHandler.Detail,
				Middlewares: common.WithAuth(),
			},
		},
	}
}

// registerCIPRoutes ICP/主体读模型（对标 ARL-Next assetCip）。
// 数据从 RootDomain 的 company/icp 字段现算。
func registerCIPRoutes() models.RouteGroup {
	return models.RouteGroup{
		Path: "/cip",
		Routes: []models.Route{
			{
				Method:      "POST",
				Path:        "/list",
				Handler:     cipHandler.List,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "/detail",
				Handler:     cipHandler.Detail,
				Middlewares: common.WithAuth(),
			},
		},
	}
}

// registerAssetOverviewRoutes 资产族数量总览。
func registerAssetOverviewRoutes() models.RouteGroup {
	return models.RouteGroup{
		Path: "/asset_overview",
		Routes: []models.Route{
			{
				Method:      "POST",
				Path:        "/count",
				Handler:     assetoverviewhandler.Count,
				Middlewares: common.WithAuth(),
			},
		},
	}
}
