package httpstatus

import (
	"errors"
	"fmt"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	svc "github.com/Autumn-27/ScopeSentry/internal/services/assets/httpstatus"
	"github.com/gin-gonic/gin"
)

var service = svc.NewService()

// List HTTP 状态码视图
// @Summary HTTP 状态码视图
// @Tags 资产
// @Router /api/assets/http_status/list [post]
func List(c *gin.Context) {
	var req models.HTTPStatusListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	result, err := service.List(c.Request.Context(), req)
	if err != nil {
		if errors.Is(err, svc.ErrInvalidStatus) {
			response.BadRequestWithError(c, "api.bad_request", err)
			return
		}
		logger.Error(fmt.Sprintf("http status list: %v", err))
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, result, "api.success")
}
