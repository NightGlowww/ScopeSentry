package assetoverview

import (
	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	overviewservice "github.com/Autumn-27/ScopeSentry/internal/services/assets/assetoverview"
	"github.com/gin-gonic/gin"
)

var overviewService overviewservice.Service

func init() {
	overviewService = overviewservice.NewService()
}

// Count 资产族数量总览
//
// @Summary      资产族数量总览
// @Description  一次返回 asset/subdomain/url/app/mp/vulnerability/sensitive/cert/cip 的数量；物理集合用 EstimatedDocumentCount，cert/cip 走聚合去重计数
// @Tags         资产
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Success      200      {object}  response.SuccessResponse{data=models.AssetOverviewCount}
// @Failure      401      {object}  response.UnauthorizedResponse
// @Failure      500      {object}  response.InternalServerErrorResponse
// @Router       /api/assets/asset_overview/count [post]
func Count(c *gin.Context) {
	result, err := overviewService.Count(c.Request.Context())
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, result, "api.success")
}
