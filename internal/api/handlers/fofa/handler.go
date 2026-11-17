package fofa

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	fofasvc "github.com/Autumn-27/ScopeSentry/internal/services/fofa"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// FOFA 路由 handler。全部走 WithAuth（见 routes/fofa），
// 导入历史分页上限 200（超过一律 400）。
//
// 错误映射约定：
//   - 参数问题（缺 query / size 超限 / createMode 非法 / pageSize>200）→ 400；
//   - provider 不可用（未配置 key / 显式 none）→ 400（带明确原因）；
//   - provider 业务错误（FOFA 返回 error:true / 4xx）→ 400；provider 5xx → 500；
//   - 其余 → 500。

func svc() *fofasvc.Service { return fofasvc.DefaultService() }

// parsePageParams 分页参数归一：pageSize 上限 200，超过报错（handler 转 400）
func parsePageParams(pageIndex, pageSize int) (int, int, error) {
	if pageIndex <= 0 {
		pageIndex = 1
	}
	if pageSize <= 0 {
		pageSize = models.FofaDefaultPageSize
	}
	if pageSize > models.FofaMaxPageSize {
		return 0, 0, fmt.Errorf("pageSize 不能超过 %d", models.FofaMaxPageSize)
	}
	return pageIndex, pageSize, nil
}

// fail 统一错误 → HTTP 状态码
func fail(c *gin.Context, err error) {
	var providerErr *fofasvc.ProviderError
	switch {
	case errors.As(err, &providerErr):
		// provider 5xx 视为上游故障，其余（查询语法 / key 无效 / 429）按请求错误处理
		if providerErr.StatusCode >= 500 {
			logger.Error("fofa: provider failed", zap.Int("status", providerErr.StatusCode), zap.Error(err))
			response.InternalServerError(c, "api.server_error", err)
			return
		}
		response.BadRequestWithError(c, "api.bad_request", err)
	case errors.Is(err, fofasvc.ErrInvalidInput),
		errors.Is(err, fofasvc.ErrQueryRequired),
		errors.Is(err, fofasvc.ErrPreviewSizeExceeded),
		errors.Is(err, fofasvc.ErrImportSizeExceeded),
		errors.Is(err, fofasvc.ErrInvalidCreateMode),
		errors.Is(err, fofasvc.ErrTooManyTasks),
		errors.Is(err, fofasvc.ErrNoHosts),
		errors.Is(err, fofasvc.ErrProviderUnavailable):
		response.BadRequestWithError(c, "api.bad_request", err)
	default:
		logger.Error("fofa: handler failed", zap.Error(err))
		response.InternalServerError(c, "api.server_error", err)
	}
}

// Status provider 状态（available / reason / 余额）
// @Summary FOFA provider 状态
// @Tags FOFA
// @Router /api/fofa/status [post]
func Status(c *gin.Context) {
	response.Success(c, svc().ProviderStatus(c.Request.Context()), "api.success")
}

// Search 查询预览（单页，size ≤ 100）
// @Summary FOFA 查询预览
// @Tags FOFA
// @Router /api/fofa/search [post]
func Search(c *gin.Context) {
	var req models.FofaSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	page, err := svc().Search(c.Request.Context(), req)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, page, "api.success")
}

// CreateTasks 从查询批量建任务（per-host / single）
// @Summary FOFA 批量建任务
// @Tags FOFA
// @Router /api/fofa/task [post]
func CreateTasks(c *gin.Context) {
	var req models.FofaTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	result, err := svc().CreateTasks(c.Request.Context(), req)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, result, "api.success")
}

// ListImports 导入历史（createdAt 倒序分页）
// @Summary FOFA 导入历史
// @Tags FOFA
// @Router /api/fofa/task/list [post]
func ListImports(c *gin.Context) {
	var req models.FofaImportListQuery
	// 空 body 等价于默认分页：只有带了 body 才校验
	if c.Request.ContentLength > 0 {
		var payload struct {
			Query     string `json:"query"`
			PageIndex int    `json:"pageIndex"`
			PageSize  int    `json:"pageSize"`
		}
		if err := c.ShouldBindJSON(&payload); err != nil {
			response.BadRequestWithError(c, "api.bad_request", err)
			return
		}
		req.Query = strings.TrimSpace(payload.Query)
		req.PageIndex = payload.PageIndex
		req.PageSize = payload.PageSize
	}
	pageIndex, pageSize, err := parsePageParams(req.PageIndex, req.PageSize)
	if err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	req.PageIndex, req.PageSize = pageIndex, pageSize
	list, total, err := svc().ListImports(c.Request.Context(), req)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{
		"list":      list,
		"total":     total,
		"pageIndex": pageIndex,
		"pageSize":  pageSize,
	}, "api.success")
}
