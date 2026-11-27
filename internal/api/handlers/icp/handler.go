package icp

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	icprepo "github.com/Autumn-27/ScopeSentry/internal/repositories/icp"
	icpsvc "github.com/Autumn-27/ScopeSentry/internal/services/icp"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ICP 路由 handler。全部走 WithAuth（见 routes/icp），分页上限 200（超过一律 400）。
//
// 错误映射约定：
//   - 任务不存在 / ID 格式非法 → 404（未知 taskId 统一按「资源不存在」处理）；
//   - 参数问题（缺 taskId、pageSize>200、type/provider 非法、非暂停态 resume）→ 400；
//   - 其余 → 500。

func svc() *icpsvc.Service { return icpsvc.DefaultService() }

// parsePageParams 分页参数归一：pageSize 上限 200，超过报错（handler 转 400）
func parsePageParams(pageIndex, pageSize int) (int, int, error) {
	if pageIndex <= 0 {
		pageIndex = 1
	}
	if pageSize <= 0 {
		pageSize = models.IcpDefaultPageSize
	}
	if pageSize > models.IcpMaxPageSize {
		return 0, 0, fmt.Errorf("pageSize 不能超过 %d", models.IcpMaxPageSize)
	}
	return pageIndex, pageSize, nil
}

// fail 统一错误 → HTTP 状态码
func fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, icprepo.ErrTaskNotFound), errors.Is(err, icprepo.ErrInvalidID):
		response.NotFound(c, "api.not_found", err)
	case errors.Is(err, icpsvc.ErrTaskBusy):
		response.BadRequestWithError(c, "api.bad_request", err)
	case errors.Is(err, icpsvc.ErrInvalidInput),
		errors.Is(err, icpsvc.ErrEmptyQueries),
		errors.Is(err, icpsvc.ErrInvalidType),
		errors.Is(err, icpsvc.ErrInvalidProvider),
		errors.Is(err, icpsvc.ErrTaskNotPaused),
		errors.Is(err, icpsvc.ErrProviderUnavailable):
		response.BadRequestWithError(c, "api.bad_request", err)
	default:
		logger.Error("icp: handler failed", zap.Error(err))
		response.InternalServerError(c, "api.server_error", err)
	}
}

// taskListRequest 任务列表入参
type taskListRequest struct {
	State     *int   `json:"state"`
	Type      string `json:"type"`
	Keyword   string `json:"keyword"`
	Provider  string `json:"provider"`
	PageIndex int    `json:"pageIndex"`
	PageSize  int    `json:"pageSize"`
}

// assetListRequest 结果列表入参
type assetListRequest struct {
	TaskID    string `json:"taskId"`
	PageIndex int    `json:"pageIndex"`
	PageSize  int    `json:"pageSize"`
}

// ListTasks 任务列表
// @Summary ICP 任务列表
// @Tags ICP
// @Router /api/icp/task/list [post]
func ListTasks(c *gin.Context) {
	var req taskListRequest
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
	list, total, err := svc().ListTasks(c.Request.Context(), models.IcpTaskListQuery{
		State:     req.State,
		Type:      strings.TrimSpace(req.Type),
		Keyword:   strings.TrimSpace(req.Keyword),
		Provider:  strings.TrimSpace(req.Provider),
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

// CreateTask 新建任务
// @Summary 新建 ICP 查询任务
// @Tags ICP
// @Router /api/icp/task/create [post]
func CreateTask(c *gin.Context) {
	var req models.IcpTaskCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	task, err := svc().CreateTask(c.Request.Context(), req)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, task, "api.success")
}

// StopTask 暂停任务
// @Summary 暂停 ICP 任务
// @Tags ICP
// @Router /api/icp/task/stop [post]
func StopTask(c *gin.Context) {
	var req models.IcpTaskIDRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	ids := req.All()
	if len(ids) == 0 {
		response.BadRequestWithError(c, "api.required.ids", nil)
		return
	}
	updated, err := svc().Stop(c.Request.Context(), ids)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"updated": updated, "ids": ids}, "api.success")
}

// RestartTask 重启任务（后台执行，状态轮询）
// @Summary 重启 ICP 任务
// @Tags ICP
// @Router /api/icp/task/restart [post]
func RestartTask(c *gin.Context) {
	var req models.IcpTaskIDRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	taskID := req.Single()
	if taskID == "" {
		response.BadRequestWithError(c, "api.required.task_id", nil)
		return
	}
	task, err := svc().Restart(c.Request.Context(), taskID)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, task, "api.success")
}

// ResumeTask 从暂停处继续
// @Summary 恢复 ICP 任务
// @Tags ICP
// @Router /api/icp/task/resume [post]
func ResumeTask(c *gin.Context) {
	var req models.IcpTaskIDRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	taskID := req.Single()
	if taskID == "" {
		response.BadRequestWithError(c, "api.required.task_id", nil)
		return
	}
	task, err := svc().Resume(c.Request.Context(), taskID)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, task, "api.success")
}

// DeleteTask 删除任务（连带结果与日志）
// @Summary 删除 ICP 任务
// @Tags ICP
// @Router /api/icp/task/delete [post]
func DeleteTask(c *gin.Context) {
	var req models.IcpTaskIDRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	ids := req.All()
	if len(ids) == 0 {
		response.BadRequestWithError(c, "api.required.ids", nil)
		return
	}
	tasks, results, logs, err := svc().Delete(c.Request.Context(), ids)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{
		"deletedTasks":   tasks,
		"deletedResults": results,
		"deletedLogs":    logs,
		"ids":            ids,
	}, "api.success")
}

// TaskLog 任务最近 N 行日志（默认/上限 200）
// @Summary ICP 任务日志
// @Tags ICP
// @Router /api/icp/task/log [post]
func TaskLog(c *gin.Context) {
	var req models.IcpTaskLogRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	taskID := strings.TrimSpace(req.TaskID)
	if taskID == "" {
		response.BadRequestWithError(c, "api.required.task_id", nil)
		return
	}
	lines, err := svc().Logs(c.Request.Context(), taskID, req.Limit)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{
		"taskId": taskID,
		"total":  len(lines),
		"lines":  lines,
	}, "api.success")
}

// TaskStatistics 统计（taskId 可选：不传则全局汇总）
// @Summary ICP 任务统计
// @Tags ICP
// @Router /api/icp/task/statistics [post]
func TaskStatistics(c *gin.Context) {
	var req models.IcpStatTaskIDRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.BadRequestWithError(c, "api.bad_request", err)
			return
		}
	}
	stats, err := svc().Statistics(c.Request.Context(), strings.TrimSpace(req.TaskID))
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, stats, "api.success")
}

// OverviewCount 各状态任务数
// @Summary ICP 任务状态计数
// @Tags ICP
// @Router /api/icp/task/overview_count [post]
func OverviewCount(c *gin.Context) {
	counts, err := svc().Overview(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, counts, "api.success")
}

// TycCheck provider 状态（available/reason/provider）
// @Summary ICP provider 状态
// @Tags ICP
// @Router /api/icp/tyc_check [post]
func TycCheck(c *gin.Context) {
	response.Success(c, svc().ProviderStatus(c.Request.Context()), "api.success")
}

// ListAssets 任务结果列表
// @Summary ICP 结果列表
// @Tags ICP
// @Router /api/icp/asset [post]
func ListAssets(c *gin.Context) {
	var req assetListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	pageIndex, pageSize, err := parsePageParams(req.PageIndex, req.PageSize)
	if err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	taskID := strings.TrimSpace(req.TaskID)
	if taskID == "" {
		response.BadRequestWithError(c, "api.required.task_id", nil)
		return
	}
	list, total, err := svc().ListResults(c.Request.Context(), models.IcpAssetQuery{
		TaskID:    taskID,
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
		"taskId":    taskID,
	}, "api.success")
}

// Sync 把结果域名写入资产组地契（去重、不覆盖）
// @Summary ICP 结果同步到资产组
// @Tags ICP
// @Router /api/icp/sync [post]
func Sync(c *gin.Context) {
	var req models.IcpSyncRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	taskID := strings.TrimSpace(req.TaskID)
	if taskID == "" {
		response.BadRequestWithError(c, "api.required.task_id", nil)
		return
	}
	summary, err := svc().Sync(c.Request.Context(), taskID, req.GroupID)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, summary, "api.success")
}

// BackfillDetails 补全结果里的 company / icpNo
// @Summary ICP 结果详情补全
// @Tags ICP
// @Router /api/icp/backfill_details [post]
func BackfillDetails(c *gin.Context) {
	var req models.IcpBackfillRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	taskID := strings.TrimSpace(req.TaskID)
	if taskID == "" {
		response.BadRequestWithError(c, "api.required.task_id", nil)
		return
	}
	summary, err := svc().Backfill(c.Request.Context(), taskID)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, summary, "api.success")
}

// Export 导出任务结果 CSV
// @Summary ICP 结果导出
// @Tags ICP
// @Router /api/icp/export [get]
func Export(c *gin.Context) {
	taskID := strings.TrimSpace(c.Query("taskId"))
	if taskID == "" {
		response.BadRequestWithError(c, "api.required.task_id", nil)
		return
	}
	// 先确认任务存在，保证未知 taskId 返回 404 而不是空 CSV
	if _, err := svc().GetTask(c.Request.Context(), taskID); err != nil {
		fail(c, err)
		return
	}

	filename := fmt.Sprintf("icp_results_%s.csv", taskID)
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
	// Excel 打开 UTF-8 CSV 需要 BOM
	if _, err := c.Writer.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return
	}
	if _, err := svc().Export(c.Request.Context(), taskID, c.Writer); err != nil {
		logger.Error("icp: export failed", zap.String("taskId", taskID), zap.Error(err))
	}
}
