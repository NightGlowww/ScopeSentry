package assets

import (
	"github.com/Autumn-27/ScopeSentry/internal/api/handlers/assets/asset"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/common"
	"github.com/Autumn-27/ScopeSentry/internal/models"
)

func registerAssetRoutes() models.RouteGroup {
	return models.RouteGroup{
		Path: "/asset",
		Routes: []models.Route{
			{
				Method:      "POST",
				Path:        "",
				Handler:     asset.GetAssets,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "/add",
				Handler:     asset.AddAsset,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "/add_batch",
				Handler:     asset.AddAssets,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "/remark",
				Handler:     asset.SetRemark,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "/detail",
				Handler:     asset.GetAssetByID,
				Middlewares: common.WithAuth(),
			},
			{
				// 资产截图：前端一直调的是 POST /api/assets/screenshot {id}，
				// 而 handler 只按 GET /api/assets/{id}/screenshot 写过、且从未注册 ——
				// 等于这个功能一直是 404（2026-11-28 实测）。这里按前端的契约注册。
				Method:      "POST",
				Path:        "/screenshot",
				Handler:     asset.GetScreenshot,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "/changelog",
				Handler:     asset.GetChangeLog,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "/card",
				Handler:     asset.GetAssetCardData,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "/hosts",
				Handler:     asset.GetHostsByRootDomain,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "/by-host",
				Handler:     asset.GetAssetsByHost,
				Middlewares: common.WithAuth(),
			},
		},
	}
}
