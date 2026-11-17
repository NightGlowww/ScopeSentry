package assets

import (
	sitehandler "github.com/Autumn-27/ScopeSentry/internal/api/handlers/assets/site"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/common"
	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// registerSiteRoutes 站点读模型路由（对标 ARL-Next assetSite 的列表 / 子域链路）。
// delete/export 本轮不做：ARL 的 delete 会连带删底层资产，语义待定。
func registerSiteRoutes() models.RouteGroup {
	return models.RouteGroup{
		Path: "/site",
		Routes: []models.Route{
			{
				Method:      "GET",
				Path:        "/list",
				Handler:     sitehandler.ListSites,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "/list",
				Handler:     sitehandler.ListSites,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "/chain",
				Handler:     sitehandler.GetChain,
				Middlewares: common.WithAuth(),
			},
		},
	}
}
