package assets

import (
	hs "github.com/Autumn-27/ScopeSentry/internal/api/handlers/assets/httpstatus"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/common"
	"github.com/Autumn-27/ScopeSentry/internal/models"
)

func registerHTTPStatusRoutes() models.RouteGroup {
	return models.RouteGroup{
		Path: "/http_status",
		Routes: []models.Route{
			{Method: "POST", Path: "/list", Handler: hs.List, Middlewares: common.WithAuth()},
		},
	}
}
