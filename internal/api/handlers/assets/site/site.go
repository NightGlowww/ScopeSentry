package site

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/site"
	"github.com/gin-gonic/gin"
)

var siteService site.Service

func init() {
	siteService = site.NewService()
}

// ListSites 站点列表（对标 ARL-Next assetSite 列表）
//
// @Summary      站点资产列表
// @Description  按 host+port 聚合 asset 集合，返回站点行（assetCount/lastSeen 为聚合值）；支持 keyword/project/分页
// @Tags         资产
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        request  body      models.SiteListRequest  false  "查询条件（GET 时走 query 参数）"
// @Success      200      {object}  response.SuccessResponse{data=models.SiteListResult}
// @Failure      400      {object}  response.BadRequestResponse
// @Failure      500      {object}  response.InternalServerErrorResponse
// @Router       /api/assets/site/list [post]
// @Router       /api/assets/site/list [get]
func ListSites(c *gin.Context) {
	var req models.SiteListRequest
	// GET 走 query、POST 走 JSON，两条路径共用同一套 model 与校验
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

	result, err := siteService.ListSites(c.Request.Context(), req)
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}

	response.Success(c, result, "api.success")
}

// GetChain 子域链路（对标 ARL-Next assetSite.subdomain_chain）
//
// @Summary      站点子域链路
// @Description  返回该 host 的子域链路（host 精确命中 + 同 rootDomain 的子域），按时间倒序，最多 500 条
// @Tags         资产
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        request  body      models.SiteChainRequest  true  "host"
// @Success      200      {object}  response.SuccessResponse{data=models.SiteChainResult}
// @Failure      400      {object}  response.BadRequestResponse
// @Failure      500      {object}  response.InternalServerErrorResponse
// @Router       /api/assets/site/chain [post]
func GetChain(c *gin.Context) {
	var req models.SiteChainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	result, err := siteService.GetChain(c.Request.Context(), req.Host)
	if err != nil {
		if errors.Is(err, site.ErrEmptyHost) {
			logger.Warn(fmt.Sprintf("site chain rejected empty host: %v", err))
			response.BadRequest(c, "api.bad_request", err)
			return
		}
		response.InternalServerError(c, "api.error", err)
		return
	}

	response.Success(c, result, "api.success")
}
