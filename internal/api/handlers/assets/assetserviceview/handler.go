package assetserviceview

import (
	"fmt"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	svc "github.com/Autumn-27/ScopeSentry/internal/services/assets/assetserviceview"
	"github.com/gin-gonic/gin"
)

var service = svc.NewService()

// List 服务维度视图
// @Summary 服务维度聚合
// @Tags 资产
// @Router /api/assets/service/list [post]
func List(c *gin.Context) {
	var req models.AssetServiceListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	result, err := service.List(c.Request.Context(), req)
	if err != nil {
		logger.Error(fmt.Sprintf("asset service list: %v", err))
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, result, "api.success")
}
