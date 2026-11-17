package assets

import (
	iphandler "github.com/Autumn-27/ScopeSentry/internal/api/handlers/assets/ip"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/common"
	"github.com/Autumn-27/ScopeSentry/internal/models"
)

func registerIPRoutes() models.RouteGroup {
	return models.RouteGroup{
		Path: "/ip",
		Routes: []models.Route{
			{
				Method:      "POST",
				Path:        "",
				Handler:     iphandler.GetIPAssets,
				Middlewares: common.WithAuth(),
			},
			// IP 读模型（对标 ARL-Next assetIP）：直接从 asset 集合现算，不依赖 IPAsset 回填
			{
				Method:      "GET",
				Path:        "/list",
				Handler:     iphandler.ListIPs,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "/list",
				Handler:     iphandler.ListIPs,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "/domains",
				Handler:     iphandler.GetDomainsByIP,
				Middlewares: common.WithAuth(),
			},
		},
	}
}
