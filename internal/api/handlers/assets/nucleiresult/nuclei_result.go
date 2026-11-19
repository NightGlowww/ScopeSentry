package nucleiresult

import (
	"errors"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	nucleiservice "github.com/Autumn-27/ScopeSentry/internal/services/assets/nucleiresult"
	"github.com/gin-gonic/gin"
)

var nucleiResultService nucleiservice.Service

func init() {
	nucleiResultService = nucleiservice.NewService()
}

// UpdateStatus 更新一条 Nuclei 结果的处置状态（误报标记 / 确认）。
// @Summary      更新Nuclei结果状态
// @Tags         Assets
// @Accept       json
// @Produce      json
// @Router       /assets/nuclei_result/status [post]
func UpdateStatus(c *gin.Context) {
	var req struct {
		ID     string `json:"id" binding:"required"`
		Status int    `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	if err := nucleiResultService.UpdateStatus(c.Request.Context(), req.ID, req.Status); err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, nil, "api.success")
}

// UpdateStatusBatch 批量更新处置状态（勾选多条一次标误报/确认）。
// @Summary      批量更新Nuclei结果状态
// @Tags         Assets
// @Accept       json
// @Produce      json
// @Router       /assets/nuclei_result/status/batch [post]
func UpdateStatusBatch(c *gin.Context) {
	var req struct {
		IDs    []string `json:"ids" binding:"required"`
		Status int      `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	n, err := nucleiResultService.UpdateStatusMany(c.Request.Context(), req.IDs, req.Status)
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, map[string]interface{}{"updated": n}, "api.success")
}

// List Nuclei 结果列表（只读视图，对标 ARL-Next assetNucleiResult）
//
// @Summary      Nuclei 结果列表
// @Description  读 vulnerability 集合的只读视图：templateId->vulnid、severity->level、matchedAt->time；支持 project/severity/templateId/keyword 过滤与分页
// @Tags         资产
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        request  body      models.NucleiResultListRequest  true  "查询条件"
// @Success      200      {object}  response.SuccessResponse{data=models.NucleiResultListResult}
// @Failure      400      {object}  response.BadRequestResponse
// @Failure      401      {object}  response.UnauthorizedResponse
// @Failure      500      {object}  response.InternalServerErrorResponse
// @Router       /api/assets/nuclei_result/list [post]
func List(c *gin.Context) {
	var req models.NucleiResultListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	result, err := nucleiResultService.List(c.Request.Context(), req)
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}

	response.Success(c, result, "api.success")
}

// Detail Nuclei 结果详情
//
// @Summary      Nuclei 结果详情
// @Description  按 id 返回单条完整记录（含 request/response）；id 格式错误 400，记录不存在 404
// @Tags         资产
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        request  body      models.NucleiResultDetailRequest  true  "id"
// @Success      200      {object}  response.SuccessResponse{data=models.NucleiResultDetail}
// @Failure      400      {object}  response.BadRequestResponse
// @Failure      401      {object}  response.UnauthorizedResponse
// @Failure      404      {object}  response.NotFoundResponse
// @Failure      500      {object}  response.InternalServerErrorResponse
// @Router       /api/assets/nuclei_result/detail [post]
func Detail(c *gin.Context) {
	var req models.NucleiResultDetailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	result, err := nucleiResultService.GetDetail(c.Request.Context(), req.ID)
	if err != nil {
		switch {
		case errors.Is(err, nucleiservice.ErrInvalidID):
			response.BadRequest(c, "api.bad_request", err)
		case errors.Is(err, nucleiservice.ErrNotFound):
			response.NotFound(c, "api.not_found", err)
		default:
			response.InternalServerError(c, "api.error", err)
		}
		return
	}

	response.Success(c, result, "api.success")
}
