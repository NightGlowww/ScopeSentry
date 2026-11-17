package assets

import (
	ag "github.com/Autumn-27/ScopeSentry/internal/api/handlers/assets/assetgroup"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/common"
	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// registerAssetGroupRoutes 资产集团（对标 ARL-Next asset_group）
func registerAssetGroupRoutes() models.RouteGroup {
	return models.RouteGroup{
		Path: "/asset_group",
		Routes: []models.Route{
			{Method: "POST", Path: "/list", Handler: ag.ListGroups, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/add", Handler: ag.AddGroup, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/edit", Handler: ag.EditGroup, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/delete", Handler: ag.DeleteGroup, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/reorder", Handler: ag.ReorderGroups, Middlewares: common.WithAuth()},
		},
	}
}

// registerAssetScopeRoutes 资产组地契（对标 ARL-Next asset_scope）
func registerAssetScopeRoutes() models.RouteGroup {
	return models.RouteGroup{
		Path: "/asset_scope",
		Routes: []models.Route{
			{Method: "POST", Path: "/list", Handler: ag.ListScopes, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/add", Handler: ag.AddScope, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/update", Handler: ag.UpdateScope, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/delete", Handler: ag.DeleteScopes, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/batch_move_group", Handler: ag.MoveScopes, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/bind_enterprise", Handler: ag.BindEnterprise, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/clear_update_error", Handler: ag.ClearUpdateErrors, Middlewares: common.WithAuth()},
		},
	}
}
