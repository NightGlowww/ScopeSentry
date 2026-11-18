// Package project_aggregation 提供项目详情页 4 个 Tab 所需的聚合接口。
//
// 路由组 /api/project_aggregation/*（Go 版此前完全未注册，前端 7 个调用全部 404）。
package project_aggregation

import (
	"errors"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/services/projectaggregation"
	"github.com/gin-gonic/gin"
)

var aggregationService projectaggregation.Service

func init() {
	aggregationService = projectaggregation.NewService()
}

// bind 绑定请求体；聚合接口都接受同一套 {id, filter, fq, search, pageIndex, pageSize, startTime, endTime}
func bind(c *gin.Context) (models.ProjectAggregationRequest, bool) {
	var req models.ProjectAggregationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return req, false
	}
	return req, true
}

// fail 把 service 层错误映射成 HTTP 状态：400 参数问题 / 404 项目不存在 / 500 其它
func fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, projectaggregation.ErrInvalidProjectID),
		errors.Is(err, projectaggregation.ErrInvalidTimeRange),
		errors.Is(err, projectaggregation.ErrPageSizeTooLarge):
		response.BadRequestWithError(c, "api.bad_request", err)
	case errors.Is(err, projectaggregation.ErrProjectNotFound):
		response.NotFound(c, "api.not_found", err)
	default:
		response.InternalServerError(c, "api.error", err)
	}
}

// GetProjectInfo godoc
// @Summary 项目详情页 - 基础信息
// @Tags 项目聚合
// @Param data body models.ProjectAggregationRequest true "项目 id"
// @Success 200 {object} response.Response{data=models.ProjectAggregationInfo}
// @Router /api/project_aggregation/project/info [post]
func GetProjectInfo(c *gin.Context) {
	req, ok := bind(c)
	if !ok {
		return
	}
	data, err := aggregationService.Info(c, req)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, data, "")
}

// GetProjectAssetCount godoc
// @Summary 项目详情页 - 资产数量汇总
// @Router /api/project_aggregation/project/asset/count [post]
func GetProjectAssetCount(c *gin.Context) {
	req, ok := bind(c)
	if !ok {
		return
	}
	data, err := aggregationService.AssetCount(c, req)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, data, "")
}

// GetProjectVulStatistics godoc
// @Summary 项目详情页 - 漏洞等级分布
// @Router /api/project_aggregation/project/vul/statistics [post]
func GetProjectVulStatistics(c *gin.Context) {
	req, ok := bind(c)
	if !ok {
		return
	}
	data, err := aggregationService.VulStatistics(c, req)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, data, "")
}

// GetProjectVulData godoc
// @Summary 项目详情页 - 漏洞列表
// @Router /api/project_aggregation/project/vul/data [post]
func GetProjectVulData(c *gin.Context) {
	req, ok := bind(c)
	if !ok {
		return
	}
	data, err := aggregationService.VulData(c, req)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, data, "")
}

// GetProjectSubdomainData godoc
// @Summary 项目详情页 - 子域名列表
// @Router /api/project_aggregation/project/subdomain/data [post]
func GetProjectSubdomainData(c *gin.Context) {
	req, ok := bind(c)
	if !ok {
		return
	}
	data, err := aggregationService.SubdomainData(c, req)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, data, "")
}

// GetProjectPortData godoc
// @Summary 项目详情页 - 端口聚合
// @Router /api/project_aggregation/project/port/data [post]
func GetProjectPortData(c *gin.Context) {
	req, ok := bind(c)
	if !ok {
		return
	}
	data, err := aggregationService.PortData(c, req)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, data, "")
}

// GetProjectServiceData godoc
// @Summary 项目详情页 - 服务聚合
// @Router /api/project_aggregation/project/service/data [post]
func GetProjectServiceData(c *gin.Context) {
	req, ok := bind(c)
	if !ok {
		return
	}
	data, err := aggregationService.ServiceData(c, req)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, data, "")
}
