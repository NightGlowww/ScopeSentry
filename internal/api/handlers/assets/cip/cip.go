package cip

import (
	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	cipservice "github.com/Autumn-27/ScopeSentry/internal/services/assets/cip"
	"github.com/gin-gonic/gin"
)

var cipService cipservice.Service

func init() {
	cipService = cipservice.NewService()
}

// List ICP/主体列表（对标 ARL-Next assetCip 列表）
//
// @Summary      主体列表
// @Description  从 RootDomain 的 company/icp 聚合主体，按 company 分组统计域名数与样例域名；支持 keyword/project/company 与分页
// @Tags         资产
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        request  body      models.CIPListRequest  true  "查询条件"
// @Success      200      {object}  response.SuccessResponse{data=models.CIPListResult}
// @Failure      400      {object}  response.BadRequestResponse
// @Failure      401      {object}  response.UnauthorizedResponse
// @Failure      500      {object}  response.InternalServerErrorResponse
// @Router       /api/assets/cip/list [post]
func List(c *gin.Context) {
	var req models.CIPListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	result, err := cipService.List(c.Request.Context(), req)
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, result, "api.success")
}

// Detail 主体详情
//
// @Summary      主体详情
// @Description  按根域名反查它所属的 company，返回该主体下的域名列表（分页）
// @Tags         资产
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        request  body      models.CIPDetailRequest  true  "根域名 + 分页"
// @Success      200      {object}  response.SuccessResponse{data=models.CIPDetailResult}
// @Failure      400      {object}  response.BadRequestResponse
// @Failure      401      {object}  response.UnauthorizedResponse
// @Failure      404      {object}  response.NotFoundResponse
// @Failure      500      {object}  response.InternalServerErrorResponse
// @Router       /api/assets/cip/detail [post]
func Detail(c *gin.Context) {
	var req models.CIPDetailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	result, err := cipService.Detail(c.Request.Context(), req)
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	if result == nil {
		response.NotFound(c, "api.not_found", nil)
		return
	}
	response.Success(c, result, "api.success")
}
