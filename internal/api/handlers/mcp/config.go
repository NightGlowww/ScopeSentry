package mcp

import (
	"errors"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/services/mcpguard"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// MCP 全局配置（开关 / 工具白名单 / 限速 / 日志保留）。

var guardService = mcpguard.Default()

// GetConfig 读取全局配置
// @Summary MCP 全局配置
// @Tags MCP
// @Router /api/mcp/config [get]
func GetConfig(c *gin.Context) {
	cfg, err := guardService.Config(c.Request.Context())
	if err != nil {
		logger.Error("mcp governance: read config failed", zap.Error(err))
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, cfg, "api.success")
}

// UpdateConfig 更新全局配置
// @Summary 更新 MCP 全局配置
// @Tags MCP
// @Router /api/mcp/config [post]
func UpdateConfig(c *gin.Context) {
	var req models.McpConfigUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	if req.AllowedTools != nil {
		for _, name := range *req.AllowedTools {
			if strings.TrimSpace(name) == "" {
				response.BadRequestWithError(c, "api.mcp.allowed_tools_blank", nil)
				return
			}
		}
	}
	operator, _ := c.Get("username")
	updatedBy, _ := operator.(string)

	cfg, err := guardService.UpdateConfig(c.Request.Context(), &req, updatedBy)
	if err != nil {
		if errors.Is(err, mcpguard.ErrInvalidConfig) {
			response.BadRequestWithError(c, "api.bad_request", err)
			return
		}
		logger.Error("mcp governance: update config failed", zap.Error(err))
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, cfg, "api.success")
}
