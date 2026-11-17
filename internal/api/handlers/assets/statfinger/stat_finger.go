package statfinger

import (
	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	statfingerservice "github.com/Autumn-27/ScopeSentry/internal/services/assets/statfinger"
	"github.com/gin-gonic/gin"
)

var statFingerService statfingerservice.Service

func init() {
	statFingerService = statfingerservice.NewService()
}

// ListTechnologies 指纹统计列表（对标 ARL-Next assetStatFinger 列表）
//
// @Summary      指纹统计列表
// @Description  从 asset.technologies 聚合技术名、出现次数、涉及资产数、最近出现时间与前 5 个样例 host；支持 keyword/project/分页，按 count 倒序
// @Tags         资产
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        request  body      models.StatFingerListRequest  true  "查询条件"
// @Success      200      {object}  response.SuccessResponse{data=models.StatFingerListResult}
// @Failure      400      {object}  response.BadRequestResponse
// @Failure      401      {object}  response.UnauthorizedResponse
// @Failure      500      {object}  response.InternalServerErrorResponse
// @Router       /api/assets/stat_finger/list [post]
func ListTechnologies(c *gin.Context) {
	var req models.StatFingerListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	result, err := statFingerService.ListTechnologies(c.Request.Context(), req)
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}

	response.Success(c, result, "api.success")
}

// ListAssetsByTechnology 按技术名反查资产
//
// @Summary      按技术反查资产
// @Description  返回使用该技术的资产列表（host/port/url/title 等摘要），按 time 倒序分页
// @Tags         资产
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        request  body      models.StatFingerAssetsRequest  true  "technology + 分页"
// @Success      200      {object}  response.SuccessResponse{data=models.StatFingerAssetsResult}
// @Failure      400      {object}  response.BadRequestResponse
// @Failure      401      {object}  response.UnauthorizedResponse
// @Failure      500      {object}  response.InternalServerErrorResponse
// @Router       /api/assets/stat_finger/assets [post]
func ListAssetsByTechnology(c *gin.Context) {
	var req models.StatFingerAssetsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	result, err := statFingerService.ListAssetsByTechnology(c.Request.Context(), req)
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}

	response.Success(c, result, "api.success")
}
