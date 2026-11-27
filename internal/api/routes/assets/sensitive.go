// assets-------------------------------------
// @file      : sensitive.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2025/5/17 11:33
// -------------------------------------------

package assets

import (
	"github.com/Autumn-27/ScopeSentry/internal/api/handlers/assets/sensitive"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/common"
	"github.com/Autumn-27/ScopeSentry/internal/models"
)

func registerSensitivveRoutes() models.RouteGroup {
	return models.RouteGroup{
		Path: "/sensitive",
		Routes: []models.Route{
			{
				Method:      "POST",
				Path:        "",
				Handler:     sensitive.GetSensitiveInfo,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "/number",
				Handler:     sensitive.GetSensitiveInfoNumber,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "/body",
				Handler:     sensitive.GetSensitiveInfoBody,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "/names",
				Handler:     sensitive.GetSensitiveInfoName,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "/info",
				Handler:     sensitive.GetSensitiveMatchInfo,
				Middlewares: common.WithAuth(),
			},
			{
				// 数据删除。注意不能挂在 /sensitive/delete：那个路径属于「敏感信息规则」
				// （routes/sensitive_rule 也挂在 /api/sensitive 下），历史上就是因为撞在一起，
				// 敏感信息页的删除按钮实际删的是规则（2026-11-28 实测）。
				Method:      "POST",
				Path:        "/data/delete",
				Handler:     sensitive.DeleteData,
				Middlewares: common.WithAuth(),
			},
		},
	}
}
