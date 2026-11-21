package subdomain

import (
	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/subdomain"
	"github.com/gin-gonic/gin"
)

var subdomainService subdomain.Service

func init() {
	subdomainService = subdomain.NewService()
}

// GetSubdomains godoc
// @Summary 获取子域名列表
// @Description 获取子域名列表，支持分页、排序和过滤
// @Tags 子域名
// @Accept json
// @Produce json
// @Param query body models.SearchRequest true "查询参数"
// @Success 200 {object} response.SuccessResponse{data=object{list=[]models.Subdomain}}
// @Failure 400 {object} response.BadRequestResponse
// @Failure 500 {object} response.InternalServerErrorResponse
// @Router /api/assets/subdomain [post]
func GetSubdomains(c *gin.Context) {
	var query models.SearchRequest
	if err := c.ShouldBindJSON(&query); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	subdomains, err := subdomainService.GetSubdomains(c, query)
	if err != nil {
		response.InternalServerError(c, "api.subdomain.list.failed", err)
		return
	}

	// 一并返回 total：前端分页条与「共 N 条」都要它，且必须跟着同一套筛选（含发现时间）。
	// 只靠 /assets/common/total 那个独立入口，时间筛选后计数不会刷新（2026-11-22）。
	total, err := subdomainService.CountSubdomains(c, query)
	if err != nil {
		response.InternalServerError(c, "api.subdomain.list.failed", err)
		return
	}

	response.Success(c, gin.H{
		"list":  subdomains,
		"total": total,
	}, "api.subdomain.list.success")
}
