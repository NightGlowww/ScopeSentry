package mcp

import (
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	mcprepo "github.com/Autumn-27/ScopeSentry/internal/repositories/mcp"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// MCP 调用审计查询与导出。

const (
	// maxExportRows CSV 导出的硬上限：走游标流式写，不把结果攒进内存
	maxExportRows = 100000
	// exportFlushEvery 每写这么多行 flush 一次，边生成边下发
	exportFlushEvery = 1000
)

var logRepository = mcprepo.NewLogRepository()

// parseTimeParam 解析 RFC3339 或 "2006-01-02 15:04:05"
func parseTimeParam(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return &t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", raw, time.Local); err == nil {
		return &t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", raw, time.Local); err == nil {
		return &t, nil
	}
	return nil, fmt.Errorf("时间格式非法: %s", raw)
}

// bindLogQuery 统一解析日志筛选参数
func bindLogQuery(c *gin.Context) (models.McpLogQuery, error) {
	var q models.McpLogQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		return q, err
	}
	q.KeyID = c.Query("keyId")
	q.Tool = c.Query("tool")
	q.Status = c.Query("status")

	from, err := parseTimeParam(c.Query("from"))
	if err != nil {
		return q, err
	}
	to, err := parseTimeParam(c.Query("to"))
	if err != nil {
		return q, err
	}
	q.From, q.To = from, to
	return q, nil
}

// ListLogs 查询 MCP 调用日志
// @Summary MCP 调用日志
// @Tags MCP
// @Router /api/mcp/logs [get]
func ListLogs(c *gin.Context) {
	q, err := bindLogQuery(c)
	if err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	pageIndex, pageSize, err := parsePageParams(q.PageIndex, q.PageSize)
	if err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	q.PageIndex, q.PageSize = pageIndex, pageSize

	logs, total, err := logRepository.ListLogs(c.Request.Context(), q)
	if err != nil {
		logger.Error("mcp governance: list logs failed", zap.Error(err))
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, gin.H{
		"list":      logs,
		"total":     total,
		"pageIndex": pageIndex,
		"pageSize":  pageSize,
	}, "api.success")
}

// ExportLogs 导出 MCP 调用日志 CSV
// @Summary 导出 MCP 调用日志
// @Tags MCP
// @Router /api/mcp/logs/export [get]
func ExportLogs(c *gin.Context) {
	q, err := bindLogQuery(c)
	if err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	// 导出不分页，但受 maxExportRows 硬上限约束
	q.PageIndex, q.PageSize = 1, 0

	filename := fmt.Sprintf("mcp_call_logs_%s.csv", time.Now().Format("20060102_150405"))
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
	// Excel 打开 UTF-8 CSV 需要 BOM
	if _, err := c.Writer.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return
	}

	w := csv.NewWriter(c.Writer)
	_ = w.Write([]string{"keyId", "keyPrefix", "tool", "status", "durationMs", "clientIp", "error", "createdAt"})

	written := 0
	iterErr := logRepository.IterateLogs(c.Request.Context(), q, maxExportRows, func(row *models.McpCallLog) error {
		record := []string{
			row.KeyID,
			row.KeyPrefix,
			row.Tool,
			row.Status,
			strconv.FormatInt(row.DurationMS, 10),
			row.ClientIP,
			row.Error,
			row.CreatedAt.Format(time.RFC3339),
		}
		if err := w.Write(record); err != nil {
			return err
		}
		written++
		if written%exportFlushEvery == 0 {
			w.Flush()
			c.Writer.Flush()
		}
		return nil
	})
	w.Flush()
	c.Writer.Flush()
	if iterErr != nil && !errors.Is(iterErr, c.Request.Context().Err()) {
		// 已经开始下发响应，无法再改状态码，只能记日志
		logger.Error("mcp governance: export logs aborted", zap.Int("written", written), zap.Error(iterErr))
	}
}
