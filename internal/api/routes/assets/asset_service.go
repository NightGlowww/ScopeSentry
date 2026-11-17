package assets

import (
	sv "github.com/Autumn-27/ScopeSentry/internal/api/handlers/assets/assetserviceview"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/common"
	"github.com/Autumn-27/ScopeSentry/internal/models"
)

func registerAssetServiceRoutes() models.RouteGroup {
	return models.RouteGroup{
		Path: "/service",
		Routes: []models.Route{
			{Method: "POST", Path: "/list", Handler: sv.List, Middlewares: common.WithAuth()},
		},
	}
}
