package export

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/services/export"
	"github.com/gin-gonic/gin"
)

// 批量导出 / 按任务导出 / 预览（对标 ARL-Next batchExport.py 与 export.py）。
//
// 两个下载接口都是「先校验 + 计数，再下发响应头，最后流式写」：
// 校验失败还能给 400，一旦开始写就只能中断连接（所以行数上限在写之前就判掉）。

// exportRowsHeader 把本次导出的数据行数带给前端（进度/提示用）。
const exportRowsHeader = "X-Export-Rows"

// exportTimeStamp 文件名时间戳 yyyymmddHHMMSS（本地时区）。
const exportTimeStamp = "20060102150405"

var batchExportService export.BatchService

func init() {
	batchExportService = export.NewBatchService()
}

// BatchExport 批量导出为 CSV（流式）
// @Summary 按类型批量导出 CSV
// @Description type 取 site|domain|ip|url|ip_port|cip|wih|asset_ip|asset_domain|asset_site|asset_wih|vulnerability|sensitive，
// @Description 可传 project/taskName/startTime/endTime 收窄，fields 传字段子集；单次最多 500000 行，超过返回 400。
// @Tags export
// @Accept json
// @Produce text/csv
// @Param request body BatchExportRequest true "导出参数"
// @Success 200 {file} file
// @Failure 400 {object} response.BadRequestResponse
// @Router /api/export/batch [post]
func BatchExport(c *gin.Context) {
	var req models.BatchExportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}

	plan, err := batchExportService.Prepare(c.Request.Context(), req)
	if err != nil {
		writeExportError(c, err)
		return
	}

	filename := fmt.Sprintf("%s-%s.csv", plan.Type, time.Now().Format(exportTimeStamp))
	writeDownloadHeaders(c, "text/csv; charset=utf-8", filename, plan.RowCount)

	written, err := plan.Stream(c.Request.Context(), c.Writer)
	if err != nil {
		// 响应头和一部分 CSV 已经下发，状态码改不了了，只能记日志
		logger.Error(fmt.Sprintf("export %s aborted after %d/%d rows: %v", plan.Type, written, plan.RowCount, err))
	}
}

// TaskExport 按任务导出（zip 内每集合一个 CSV）
// @Summary 按任务导出
// @Description 任务存在则把 site/domain/url/vulnerability/wih 五个集合按 taskName 各导一个 CSV，打进一个 zip。
// @Description 任务不存在返回 404；合计超过 500000 行返回 400。
// @Tags export
// @Produce application/zip
// @Param taskId path string true "任务 ID"
// @Success 200 {file} file
// @Failure 400 {object} response.BadRequestResponse
// @Failure 404 {object} response.NotFoundResponse
// @Router /api/export/task/{taskId} [get]
func TaskExport(c *gin.Context) {
	taskID := strings.TrimSpace(c.Param("taskId"))
	if taskID == "" {
		response.BadRequestWithError(c, "api.bad_request", fmt.Errorf("taskId is required"))
		return
	}

	plan, err := batchExportService.PrepareTask(c.Request.Context(), taskID)
	if err != nil {
		writeExportError(c, err)
		return
	}

	filename := fmt.Sprintf(export.TaskExportFileName, safeFileToken(taskID), time.Now().Format(exportTimeStamp))
	writeDownloadHeaders(c, "application/zip", filename, plan.RowCount)

	written, err := plan.Stream(c.Request.Context(), c.Writer)
	if err != nil {
		logger.Error(fmt.Sprintf("task export %s aborted after %d/%d rows: %v", taskID, written, plan.RowCount, err))
	}
}

// PreviewExport 导出预览：前 20 行 + 命中总数
// @Summary 导出预览
// @Description 参数与 /api/export/batch 相同，返回前 20 行、命中总数、本次列与可选字段，供前端确认字段。
// @Tags export
// @Accept json
// @Produce json
// @Param request body BatchExportRequest true "导出参数"
// @Success 200 {object} models.ExportPreviewResult
// @Failure 400 {object} response.BadRequestResponse
// @Router /api/export/preview [post]
func PreviewExport(c *gin.Context) {
	var req models.BatchExportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}

	result, err := batchExportService.Preview(c.Request.Context(), req)
	if err != nil {
		writeExportError(c, err)
		return
	}
	response.Success(c, result, "api.success")
}

// writeDownloadHeaders 统一下发下载响应头。
func writeDownloadHeaders(c *gin.Context, contentType, filename string, rows int64) {
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	c.Header("Cache-Control", "no-store")
	c.Header(exportRowsHeader, strconv.FormatInt(rows, 10))
	c.Status(http.StatusOK)
}

// writeExportError 导出失败原因 -> 状态码。
// 400 的 data 里带具体原因（支持的 type / 未知字段 / 缩小范围提示），前端可直接展示。
func writeExportError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, export.ErrTooManyRows),
		errors.Is(err, export.ErrUnknownExportType),
		errors.Is(err, export.ErrUnknownExportField),
		errors.Is(err, export.ErrInvalidRequest):
		response.BadRequestWithError(c, "api.bad_request", err)
	case errors.Is(err, export.ErrTaskNotFound):
		response.NotFound(c, "api.not_found", err)
	default:
		response.InternalServerError(c, "api.server_error", err)
	}
}

// safeFileToken 把路径参数收进文件名：只留字母数字和 ._-，其它替换掉，
// 避免 Content-Disposition 注入和超长文件名。
func safeFileToken(raw string) string {
	const maxLen = 64
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
		if b.Len() >= maxLen {
			break
		}
	}
	token := b.String()
	if token == "" {
		return "task"
	}
	return token
}
