// assets-------------------------------------
// @file      : asset.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2025/5/8 19:51
// -------------------------------------------

package assets

import (
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/common"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/gin-gonic/gin"
)

func RegisterAssetsRoutes(api *gin.RouterGroup) {
	nodeRoutes := models.RouteGroup{
		Path: "/assets",
		Groups: []models.RouteGroup{
			registerAssetRoutes(),
			registerStatisticsRoutes(),
			registerRootDomainRoutes(),
			registerCommonRoutes(),
			registerSubDomainRoutes(),
			registerAppRoutes(),
			registerMpRoutes(),
			registerUrlRoutes(),
			registerIPRoutes(),
			registerCrawlerRoutes(),
			registerSensitivveRoutes(),
			registerDirscanRoutes(),
			registerVulnerabilityRoutes(),
			registerPageMonitoringRoutes(),
			registerTagColorRoutes(),
			registerAssetGroupRoutes(),
			registerAssetScopeRoutes(),
			registerSiteRoutes(),
			// 资产读模型（对标 ARL-Next）：指纹统计 / Nuclei 结果 / WIH 敏感信息
			registerStatFingerRoutes(),
			registerNucleiResultRoutes(),
			registerWIHRoutes(),
			// 资产读模型（对标 ARL-Next）：证书 / ICP 主体 / 资产族总览
			registerCertRoutes(),
			registerCIPRoutes(),
			registerAssetOverviewRoutes(),
		},
	}

	common.RegisterRouteGroup(api, nodeRoutes)
}
