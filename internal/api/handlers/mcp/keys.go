package mcp

import (
	"errors"
	"strconv"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/services/apikey"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
)

// MCP Key 治理接口（对标 ARL-Next mcp keys）。

var keyService = apikey.NewService()

// keyIDParam 取路径里的 key id 并校验格式。
// 以前只判空：随便写一段非 ObjectID（例如把 /update 当路径）会一路走到 mongo，
// 报 500「the provided hex string is not a valid ObjectID」——那是参数问题，不是服务端故障。
func keyIDParam(c *gin.Context) (string, bool) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		response.BadRequestWithError(c, "api.required.id", nil)
		return "", false
	}
	if _, err := primitive.ObjectIDFromHex(id); err != nil {
		response.BadRequestWithError(c, "api.bad_request.id", nil)
		return "", false
	}
	return id, true
}

// respondMCPError 统一错误出口：可判定的转 400/404，其余 500
func respondMCPError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, apikey.ErrNotFound):
		// 资源不存在是 404，不是 400：客户端要能区分「参数写错了」和「这条记录没了」
		response.NotFound(c, "api.mcp.key.not_found", nil)
	case errors.Is(err, apikey.ErrInvalid):
		response.BadRequestWithError(c, "api.bad_request", err)
	default:
		logger.Error("mcp governance: request failed", zap.Error(err))
		response.InternalServerError(c, "api.error", err)
	}
}

// parsePageParams 解析分页参数；pageSize 上限 200，超出即 400
func parsePageParams(pageIndex, pageSize int) (int, int, error) {
	if pageIndex <= 0 {
		pageIndex = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > models.McpMaxPageSize {
		return 0, 0, errors.New("pageSize 不能超过 " + strconv.Itoa(models.McpMaxPageSize))
	}
	return pageIndex, pageSize, nil
}

// ListKeys 获取 MCP Key 列表
// @Summary MCP Key 列表
// @Tags MCP
// @Router /api/mcp/keys [get]
func ListKeys(c *gin.Context) {
	var req models.McpKeyListQuery
	if err := c.ShouldBindQuery(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	pageIndex, pageSize, err := parsePageParams(req.PageIndex, req.PageSize)
	if err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}

	keys, total, err := keyService.ListPage(c.Request.Context(), req.Keyword, pageIndex, pageSize)
	if err != nil {
		respondMCPError(c, err)
		return
	}
	response.Success(c, gin.H{
		"list":      keys,
		"total":     total,
		"pageIndex": pageIndex,
		"pageSize":  pageSize,
	}, "api.success")
}

// CreateKey 新建 MCP Key（明文 key 仅本次返回）
// @Summary 新建 MCP Key
// @Tags MCP
// @Router /api/mcp/keys [post]
func CreateKey(c *gin.Context) {
	var req models.McpKeyCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	operator, _ := c.Get("username")
	createdBy, _ := operator.(string)

	result, err := keyService.CreateWithOptions(c.Request.Context(), &req, createdBy)
	if err != nil {
		respondMCPError(c, err)
		return
	}
	response.Success(c, result, "api.success")
}

// UpdateKey 编辑 MCP Key 的治理字段
// @Summary 编辑 MCP Key
// @Tags MCP
// @Router /api/mcp/keys/{id} [post]
func UpdateKey(c *gin.Context) {
	id, ok := keyIDParam(c)
	if !ok {
		return
	}
	var req models.McpKeyUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	updated, err := keyService.Update(c.Request.Context(), id, &req)
	if err != nil {
		respondMCPError(c, err)
		return
	}
	response.Success(c, updated, "api.success")
}

// SetKeyStatus 启用 / 禁用
// @Summary 启用禁用 MCP Key
// @Tags MCP
// @Router /api/mcp/keys/{id}/status [post]
func SetKeyStatus(c *gin.Context) {
	id, ok := keyIDParam(c)
	if !ok {
		return
	}
	var req models.McpKeyStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	if req.Enabled == nil {
		response.BadRequestWithError(c, "api.required.enabled", nil)
		return
	}
	if err := keyService.SetEnabled(c.Request.Context(), id, *req.Enabled); err != nil {
		respondMCPError(c, err)
		return
	}
	response.Success(c, gin.H{"id": id, "enabled": *req.Enabled}, "api.success")
}

// DeleteKey 删除 MCP Key
// @Summary 删除 MCP Key
// @Tags MCP
// @Router /api/mcp/keys/{id} [delete]
func DeleteKey(c *gin.Context) {
	id, ok := keyIDParam(c)
	if !ok {
		return
	}
	deleted, err := keyService.Delete(c.Request.Context(), id)
	if err != nil {
		respondMCPError(c, err)
		return
	}
	if deleted == 0 {
		// 以前对任意 id 都回 deleted:true，撤销一个写错的 key 名时会以为已撤销（2026-11-28 实测）
		response.NotFound(c, "api.mcp.key.not_found", nil)
		return
	}
	response.Success(c, gin.H{"id": id, "deleted": true}, "api.success")
}
