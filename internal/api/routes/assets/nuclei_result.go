package assets

import (
	nucleiresulthandler "github.com/Autumn-27/ScopeSentry/internal/api/handlers/assets/nucleiresult"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/common"
	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// registerNucleiResultRoutes Nuclei 结果读模型（对标 ARL-Next assetNucleiResult）。
// 只读 vulnerability 集合，不改扫描侧的写入链路。
func registerNucleiResultRoutes() models.RouteGroup {
	return models.RouteGroup{
		Path: "/nuclei_result",
		Routes: []models.Route{
			{
				Method:      "POST",
				Path:        "/list",
				Handler:     nucleiresulthandler.List,
				Middlewares: common.WithAuth(),
			},
			{
				Method:      "POST",
				Path:        "/detail",
				Handler:     nucleiresulthandler.Detail,
				Middlewares: common.WithAuth(),
			},
			{
				// 处置状态：误报标记 / 确认存在
				Method:      "POST",
				Path:        "/status",
				Handler:     nucleiresulthandler.UpdateStatus,
				Middlewares: common.WithAuth(),
			},
			{
				// 批量处置：勾选多条一次改状态
				Method:      "POST",
				Path:        "/status/batch",
				Handler:     nucleiresulthandler.UpdateStatusBatch,
				Middlewares: common.WithAuth(),
			},
		},
	}
}
