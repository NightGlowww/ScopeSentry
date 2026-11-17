package dictionary

import (
	dictHandler "github.com/Autumn-27/ScopeSentry/internal/api/handlers/dictionary"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/common"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/gin-gonic/gin"
)

func RegisterDictionaryRoutes(api *gin.RouterGroup) {
	// /api/dictionary/*：对标 ARL-Next 的字典能力（dictionary.py），
	// 与 /manage、/port 两个历史分组共存，历史接口保持不变。
	dictionaryGroup := models.RouteGroup{
		Path: "/dictionary",
		Routes: []models.Route{
			{Method: "POST", Path: "/list", Handler: dictHandler.ListDictionaries, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/preview", Handler: dictHandler.Preview, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/search", Handler: dictHandler.Search, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/append", Handler: dictHandler.Append, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/delete_entries", Handler: dictHandler.DeleteEntries, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/delete_file", Handler: dictHandler.DeleteFile, Middlewares: common.WithAuth()},
			{Method: "GET", Path: "/download", Handler: dictHandler.DownloadByKind, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/create", Handler: dictHandler.CreateDictionary, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/upload_large", Handler: dictHandler.UploadLarge, Middlewares: common.WithAuth()},
			{Method: "GET", Path: "/upload_status", Handler: dictHandler.UploadStatus, Middlewares: common.WithAuth()},
		},
		Groups: []models.RouteGroup{
			{
				Path: "/manage",
				Routes: []models.Route{
					{Method: "GET", Path: "/list", Handler: dictHandler.List, Middlewares: common.WithAuth()},
					{Method: "POST", Path: "/create", Handler: dictHandler.Create, Middlewares: common.WithAuth()},
					{Method: "GET", Path: "/download", Handler: dictHandler.Download, Middlewares: common.WithAuth()},
					{Method: "POST", Path: "/delete", Handler: dictHandler.Delete, Middlewares: common.WithAuth()},
					{Method: "POST", Path: "/save", Handler: dictHandler.Save, Middlewares: common.WithAuth()},
				},
			},
			{
				Path: "/port",
				Routes: []models.Route{
					{Method: "POST", Path: "/data", Handler: dictHandler.GetPortData, Middlewares: common.WithAuth()},
					{Method: "POST", Path: "/upgrade", Handler: dictHandler.UpgradePortDict, Middlewares: common.WithAuth()},
					{Method: "POST", Path: "/add", Handler: dictHandler.AddPortDict, Middlewares: common.WithAuth()},
					{Method: "POST", Path: "/delete", Handler: dictHandler.DeletePortDict, Middlewares: common.WithAuth()},
				},
			},
		},
	}
	common.RegisterRouteGroup(api, dictionaryGroup)

	// /api/dict_cdn/*：CDN 字典（kind=cdn），对标 cdn_dict.py
	cdnGroup := models.RouteGroup{
		Path: "/dict_cdn",
		Routes: []models.Route{
			{Method: "POST", Path: "/list", Handler: dictHandler.CDNList, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/save", Handler: dictHandler.CDNSave, Middlewares: common.WithAuth()},
		},
	}
	common.RegisterRouteGroup(api, cdnGroup)

	// /api/dict_brute/*：爆破字典（kind=brute），对标 brute_dict.py
	bruteGroup := models.RouteGroup{
		Path: "/dict_brute",
		Routes: []models.Route{
			{Method: "POST", Path: "/list", Handler: dictHandler.BruteList, Middlewares: common.WithAuth()},
			{Method: "POST", Path: "/save", Handler: dictHandler.BruteSave, Middlewares: common.WithAuth()},
		},
	}
	common.RegisterRouteGroup(api, bruteGroup)
}
