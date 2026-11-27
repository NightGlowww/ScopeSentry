package policy

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	policysvc "github.com/Autumn-27/ScopeSentry/internal/services/policy"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// 扫描策略路由 handler。全部走 WithAuth（见 routes/policy），pageSize>200 一律 400。
//
// 错误映射约定：
//   - 策略不存在 / ID 格式非法 → 404（未知 id 统一按「资源不存在」处理）；
//   - 参数问题（缺 id、pageSize>200、名称重复、参数越界、模板不存在）→ 400；
//   - 其余 → 500。

func svc() *policysvc.Service { return policysvc.DefaultService() }

// fail 统一错误 → HTTP 状态码
func fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, policysvc.ErrNotFound), errors.Is(err, policysvc.ErrInvalidID):
		response.NotFound(c, "api.not_found", err)
	case errors.Is(err, policysvc.ErrInvalidInput),
		errors.Is(err, policysvc.ErrInvalidConfig),
		errors.Is(err, policysvc.ErrNameExists),
		errors.Is(err, policysvc.ErrTemplateNotFound):
		response.BadRequestWithError(c, "api.bad_request", err)
	default:
		logger.Error("policy: handler failed", zap.Error(err))
		response.InternalServerError(c, "api.server_error", err)
	}
}

// parsePageParams 分页参数归一：pageSize 上限 200，超过报错（handler 转 400）
func parsePageParams(pageIndex, pageSize int) (int, int, error) {
	if pageIndex <= 0 {
		pageIndex = 1
	}
	if pageSize <= 0 {
		pageSize = models.ScanPolicyDefaultPageSize
	}
	if pageSize > models.ScanPolicyMaxPageSize {
		return 0, 0, fmt.Errorf("pageSize 不能超过 %d", models.ScanPolicyMaxPageSize)
	}
	return pageIndex, pageSize, nil
}

// listRequest 列表入参（keyword 兼容 search 别名）
type listRequest struct {
	Keyword   string `json:"keyword"`
	Search    string `json:"search"`
	Enabled   *bool  `json:"enabled"`
	PageIndex int    `json:"pageIndex"`
	PageSize  int    `json:"pageSize"`
}

// testRequest test 入参
type testRequest struct {
	Config models.ScanPolicyConfig `json:"config"`
}

// List 策略列表
// @Summary 扫描策略列表
// @Tags 扫描策略
// @Router /api/policy/list [post]
func List(c *gin.Context) {
	var req listRequest
	// 空 body 等价于默认分页：只有带了 body 才校验
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.BadRequestWithError(c, "api.bad_request", err)
			return
		}
	}
	pageIndex, pageSize, err := parsePageParams(req.PageIndex, req.PageSize)
	if err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	keyword := req.Keyword
	if strings.TrimSpace(keyword) == "" {
		keyword = req.Search
	}
	list, total, err := svc().List(c.Request.Context(), models.ScanPolicyListQuery{
		Keyword:   strings.TrimSpace(keyword),
		Enabled:   req.Enabled,
		PageIndex: pageIndex,
		PageSize:  pageSize,
	})
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

// Create 新建策略
// @Summary 新建扫描策略
// @Tags 扫描策略
// @Router /api/policy/create [post]
func Create(c *gin.Context) {
	var req models.ScanPolicyCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	created, err := svc().Create(c.Request.Context(), req)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, created, "api.success")
}

// Update 更新策略
// @Summary 更新扫描策略
// @Tags 扫描策略
// @Router /api/policy/update [post]
func Update(c *gin.Context) {
	var req models.ScanPolicyUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	updated, err := svc().Update(c.Request.Context(), req)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, updated, "api.success")
}

// Delete 批量删除策略
// @Summary 删除扫描策略
// @Tags 扫描策略
// @Router /api/policy/delete [post]
func Delete(c *gin.Context) {
	var req models.ScanPolicyIDRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	ids := req.All()
	if len(ids) == 0 {
		response.BadRequestWithError(c, "api.bad_request", errors.New("ids 不能为空"))
		return
	}
	deleted, err := svc().Delete(c.Request.Context(), ids)
	if err != nil {
		fail(c, err)
		return
	}
	// 一条都没删掉 = 策略不存在（2026-11-28 实测：此前回 200 + deleted:0）
	if deleted == 0 {
		response.NotFound(c, "api.not_found", fmt.Errorf("策略不存在: %s", strings.Join(ids, ",")))
		return
	}
	response.Success(c, gin.H{"deleted": deleted, "ids": ids}, "api.success")
}

// Detail 策略详情（含 usage 引用说明）
// @Summary 扫描策略详情
// @Tags 扫描策略
// @Router /api/policy/detail [post]
func Detail(c *gin.Context) {
	var req models.ScanPolicyIDRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	id := req.Single()
	if id == "" {
		response.BadRequestWithError(c, "api.bad_request", errors.New("id 不能为空"))
		return
	}
	detail, err := svc().Detail(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, detail, "api.success")
}

// Apply 预览「策略覆盖模板默认」的合并结果（不写库）
// @Summary 扫描策略合并预览
// @Tags 扫描策略
// @Router /api/policy/apply [post]
func Apply(c *gin.Context) {
	var req models.ScanPolicyApplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	merged, err := svc().Apply(c.Request.Context(), req.PolicyID, req.TaskTemplate)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, merged, "api.success")
}

// Test 校验一组扫描参数（范围 + 模板/插件存在性），返回结构化结果
// @Summary 扫描策略参数校验
// @Tags 扫描策略
// @Router /api/policy/test [post]
func Test(c *gin.Context) {
	var req testRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	result, err := svc().Test(c.Request.Context(), req.Config)
	if err != nil {
		fail(c, err)
		return
	}
	// 参数不合法也返回 200：这是「校验预览」端点，前端按 valid/errors 渲染提示；
	// 真正的写入端点（create/update）才对同样的错误回 400。
	response.Success(c, result, "api.success")
}
