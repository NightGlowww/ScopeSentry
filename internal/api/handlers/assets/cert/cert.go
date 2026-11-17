package cert

import (
	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	certservice "github.com/Autumn-27/ScopeSentry/internal/services/assets/cert"
	"github.com/gin-gonic/gin"
)

var certService certservice.Service

func init() {
	certService = certservice.NewService()
}

// List 证书列表（对标 ARL-Next assetCert 列表）
//
// @Summary      证书列表
// @Description  从 asset 的 TLS 字段聚合证书记录，同一证书（指纹或 serial+issuer）合并；hosts 最多 50 个、hostCount 为完整计数；支持 keyword/project/expired 与分页，按 notAfter 升序
// @Tags         资产
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        request  body      models.CertListRequest  true  "查询条件"
// @Success      200      {object}  response.SuccessResponse{data=models.CertListResult}
// @Failure      400      {object}  response.BadRequestResponse
// @Failure      401      {object}  response.UnauthorizedResponse
// @Failure      500      {object}  response.InternalServerErrorResponse
// @Router       /api/assets/cert/list [post]
func List(c *gin.Context) {
	var req models.CertListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	result, err := certService.List(c.Request.Context(), req)
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, result, "api.success")
}

// Detail 证书详情
//
// @Summary      证书详情
// @Description  按列表返回的证书聚合键取单张证书（含关联 host+ip+port）
// @Tags         资产
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        request  body      models.CertDetailRequest  true  "证书 id"
// @Success      200      {object}  response.SuccessResponse{data=models.CertRow}
// @Failure      400      {object}  response.BadRequestResponse
// @Failure      401      {object}  response.UnauthorizedResponse
// @Failure      404      {object}  response.NotFoundResponse
// @Failure      500      {object}  response.InternalServerErrorResponse
// @Router       /api/assets/cert/detail [post]
func Detail(c *gin.Context) {
	var req models.CertDetailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	row, err := certService.Detail(c.Request.Context(), req)
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	if row == nil {
		response.NotFound(c, "api.not_found", nil)
		return
	}
	response.Success(c, row, "api.success")
}
