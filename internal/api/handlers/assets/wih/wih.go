package wih

import (
	"errors"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	wihservice "github.com/Autumn-27/ScopeSentry/internal/services/assets/wih"
	"github.com/gin-gonic/gin"
)

var wihService wihservice.Service

func init() {
	wihService = wihservice.NewService()
}

// List WIH 敏感信息列表（对标 ARL-Next assetWih，读 SensitiveResult）
//
// @Summary      WIH 敏感信息列表
// @Description  读 SensitiveResult 的只读视图：type->sid（规则名）、match->命中片段；支持 type/project/keyword 过滤与分页
// @Tags         资产
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        request  body      models.WIHListRequest  true  "查询条件"
// @Success      200      {object}  response.SuccessResponse{data=models.WIHListResult}
// @Failure      400      {object}  response.BadRequestResponse
// @Failure      401      {object}  response.UnauthorizedResponse
// @Failure      500      {object}  response.InternalServerErrorResponse
// @Router       /api/assets/wih/list [post]
func List(c *gin.Context) {
	var req models.WIHListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	result, err := wihService.List(c.Request.Context(), req)
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}

	response.Success(c, result, "api.success")
}

// Detail WIH 敏感信息详情
//
// @Summary      WIH 敏感信息详情
// @Description  按 id 返回单条记录（含完整命中片段）；正文按 bodyId 走 /api/assets/sensitive/body 另取
// @Tags         资产
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        request  body      models.WIHDetailRequest  true  "id"
// @Success      200      {object}  response.SuccessResponse{data=models.WIHDetail}
// @Failure      400      {object}  response.BadRequestResponse
// @Failure      401      {object}  response.UnauthorizedResponse
// @Failure      404      {object}  response.NotFoundResponse
// @Failure      500      {object}  response.InternalServerErrorResponse
// @Router       /api/assets/wih/detail [post]
func Detail(c *gin.Context) {
	var req models.WIHDetailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	result, err := wihService.GetDetail(c.Request.Context(), req.ID)
	if err != nil {
		switch {
		case errors.Is(err, wihservice.ErrInvalidID):
			response.BadRequest(c, "api.bad_request", err)
		case errors.Is(err, wihservice.ErrNotFound):
			response.NotFound(c, "api.not_found", err)
		default:
			response.InternalServerError(c, "api.error", err)
		}
		return
	}

	response.Success(c, result, "api.success")
}
