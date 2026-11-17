package ip

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	ipservice "github.com/Autumn-27/ScopeSentry/internal/services/assets/ip"
	"github.com/gin-gonic/gin"
)

var ipReadModelService ipservice.ReadModelService

func init() {
	ipReadModelService = ipservice.NewReadModelService()
}

// ListIPs IP 资产列表（对标 ARL-Next assetIP 列表）
//
// @Summary      IP 资产列表
// @Description  按 ip 聚合 asset 集合，返回端口数/端口摘要、域名数/域名摘要；支持 keyword/ip 前缀/分页
// @Tags         资产
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        request  body      models.IPListRequest  false  "查询条件（GET 时走 query 参数）"
// @Success      200      {object}  response.SuccessResponse{data=models.IPListResult}
// @Failure      400      {object}  response.BadRequestResponse
// @Failure      500      {object}  response.InternalServerErrorResponse
// @Router       /api/assets/ip/list [post]
// @Router       /api/assets/ip/list [get]
func ListIPs(c *gin.Context) {
	var req models.IPListRequest
	var err error
	if c.Request.Method == http.MethodGet {
		err = c.ShouldBindQuery(&req)
	} else {
		err = c.ShouldBindJSON(&req)
	}
	if err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	result, err := ipReadModelService.ListIPs(c.Request.Context(), req)
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}

	response.Success(c, result, "api.success")
}

// GetDomainsByIP IP 反查域名（对标 ARL-Next assetIP 的 export_domain / ip_domain_detail 口径）
//
// @Summary      IP 反查域名
// @Description  返回该 IP 上解析到的域名列表（来自 asset.host 与 subdomain.ip），带来源标记
// @Tags         资产
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        request  body      models.IPDomainsRequest  true  "ip"
// @Success      200      {object}  response.SuccessResponse{data=models.IPDomainsResult}
// @Failure      400      {object}  response.BadRequestResponse
// @Failure      500      {object}  response.InternalServerErrorResponse
// @Router       /api/assets/ip/domains [post]
func GetDomainsByIP(c *gin.Context) {
	var req models.IPDomainsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	result, err := ipReadModelService.ListDomainsByIP(c.Request.Context(), req.IP)
	if err != nil {
		if errors.Is(err, ipservice.ErrEmptyIP) {
			logger.Warn(fmt.Sprintf("ip domains rejected empty ip: %v", err))
			response.BadRequest(c, "api.bad_request", err)
			return
		}
		response.InternalServerError(c, "api.error", err)
		return
	}

	response.Success(c, result, "api.success")
}
