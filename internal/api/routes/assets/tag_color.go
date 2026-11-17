package assets

import (
	tagcolor "github.com/Autumn-27/ScopeSentry/internal/api/handlers/assets/tagcolor"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/common"
	"github.com/Autumn-27/ScopeSentry/internal/models"
)

func registerTagColorRoutes() models.RouteGroup {
	return models.RouteGroup{
		Path: "/tag_color",
		Routes: []models.Route{
			{
				Method:      "POST",
				Path:        "/list",
				Handler:     tagcolor.List,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "/set",
				Handler:     tagcolor.Set,
				Middlewares: common.WithAuth(),
			},
		},
	}
}
