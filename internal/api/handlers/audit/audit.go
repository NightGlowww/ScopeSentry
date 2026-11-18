package audit

import (
	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	auditrepo "github.com/Autumn-27/ScopeSentry/internal/repositories/audit"
	"github.com/gin-gonic/gin"
)

var repo = auditrepo.NewRepository()

// List 操作日志列表
// @Summary 服务端操作日志
// @Tags 审计
// @Router /api/audit/list [post]
func List(c *gin.Context) {
	var q models.AuditLogQuery
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&q); err != nil {
			response.BadRequestWithError(c, "api.bad_request", err)
			return
		}
	}
	if q.PageIndex <= 0 {
		q.PageIndex = 1
	}
	if q.PageSize <= 0 {
		q.PageSize = 20
	}
	if q.PageSize > 200 {
		response.BadRequestWithError(c, "api.bad_request", errTooLarge)
		return
	}

	list, total, err := repo.List(c.Request.Context(), q)
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, gin.H{
		"list":      list,
		"total":     total,
		"pageIndex": q.PageIndex,
		"pageSize":  q.PageSize,
	}, "api.success")
}
