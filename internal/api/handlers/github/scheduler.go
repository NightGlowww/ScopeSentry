package github

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	githubrepo "github.com/Autumn-27/ScopeSentry/internal/repositories/github"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// GitHub 周期调度面（对标 ARL-Next github_scheduler.py）。
//
// 语义：scheduler 视图 = 带 config.intervalMinutes>0 的 github_task。
// 不额外建集合，避免同一件事两处存；stop/recover 直接操作任务状态机：
//   - stop    → state = 2（暂停，tick 跳过）
//   - recover → state = 0（待开始，清空 nextRunAt 立即可以跑）
//   - run     → 立刻同步跑一次

// schedulerListRequest 调度列表入参
type schedulerListRequest struct {
	State     *int   `json:"state"`
	Keyword   string `json:"keyword"`
	PageIndex int    `json:"pageIndex"`
	PageSize  int    `json:"pageSize"`
}

// ListSchedulers 周期任务列表
// @Summary GitHub 周期任务列表
// @Tags GitHub
// @Router /api/github/scheduler/list [post]
func ListSchedulers(c *gin.Context) {
	var req schedulerListRequest
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
	list, total, err := taskRepo.List(c.Request.Context(), models.GithubTaskListQuery{
		State:         req.State,
		Keyword:       strings.TrimSpace(req.Keyword),
		ScheduledOnly: true,
		PageIndex:     pageIndex,
		PageSize:      pageSize,
	})
	if err != nil {
		logger.Error("github intel: list schedulers failed", zap.Error(err))
		response.InternalServerError(c, "api.server_error", err)
		return
	}
	response.Success(c, gin.H{
		"list":      list,
		"total":     total,
		"pageIndex": pageIndex,
		"pageSize":  pageSize,
	}, "api.success")
}

// RunSchedulers 立即运行选中的周期任务（同步执行，返回每次运行的统计）
// @Summary 立即运行 GitHub 周期任务
// @Tags GitHub
// @Router /api/github/scheduler/run [post]
func RunSchedulers(c *gin.Context) {
	var req models.GithubIDListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	ids := normalizeIDs(req.AllIDs())
	if len(ids) == 0 {
		response.BadRequestWithError(c, "api.required.ids", nil)
		return
	}
	summaries := make([]interface{}, 0, len(ids))
	failures := make([]gin.H, 0)
	for _, id := range ids {
		summary, err := svc().RunTask(c.Request.Context(), id)
		if err != nil {
			switch {
			case errors.Is(err, githubrepo.ErrTaskBusy):
				failures = append(failures, gin.H{"id": id, "error": "任务正在运行"})
			case errors.Is(err, githubrepo.ErrTaskNotFound), errors.Is(err, githubrepo.ErrInvalidID):
				failures = append(failures, gin.H{"id": id, "error": "任务不存在"})
			default:
				logger.Error("github intel: run scheduler failed", zap.String("id", id), zap.Error(err))
				failures = append(failures, gin.H{"id": id, "error": err.Error()})
			}
			continue
		}
		summaries = append(summaries, summary)
	}
	response.Success(c, gin.H{"runs": summaries, "failures": failures}, "api.success")
}

// StopSchedulers 暂停周期任务
// @Summary 暂停 GitHub 周期任务
// @Tags GitHub
// @Router /api/github/scheduler/stop [post]
func StopSchedulers(c *gin.Context) {
	var req models.GithubIDListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	ids := normalizeIDs(req.AllIDs())
	if len(ids) == 0 {
		response.BadRequestWithError(c, "api.required.ids", nil)
		return
	}
	matched, modified, err := svc().StopTasks(c.Request.Context(), ids)
	if err != nil {
		if errors.Is(err, githubrepo.ErrInvalidID) {
			response.BadRequestWithError(c, "api.bad_request", err)
			return
		}
		response.InternalServerError(c, "api.server_error", err)
		return
	}
	// matched=0 → 404：id 一条都不存在，不能回「已暂停」
	if matched == 0 {
		response.NotFound(c, "api.not_found", fmt.Errorf("%w: %s", githubrepo.ErrTaskNotFound, strings.Join(ids, ",")))
		return
	}
	response.Success(c, gin.H{"updated": modified, "matched": matched, "ids": ids}, "api.success")
}

// RecoverSchedulers 恢复被暂停的周期任务
// @Summary 恢复 GitHub 周期任务
// @Tags GitHub
// @Router /api/github/scheduler/recover [post]
func RecoverSchedulers(c *gin.Context) {
	var req models.GithubIDListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	ids := normalizeIDs(req.AllIDs())
	if len(ids) == 0 {
		response.BadRequestWithError(c, "api.required.ids", nil)
		return
	}
	recovered, err := svc().RecoverTasks(c.Request.Context(), ids)
	if err != nil {
		if errors.Is(err, githubrepo.ErrInvalidID) {
			response.BadRequestWithError(c, "api.bad_request", err)
			return
		}
		logger.Error("github intel: recover schedulers failed", zap.Error(err))
		c.JSON(http.StatusBadRequest, response.Response{
			Code:    http.StatusBadRequest,
			Message: "api.bad_request",
			Data:    err.Error(),
		})
		return
	}
	response.Success(c, gin.H{"recovered": recovered, "ids": ids}, "api.success")
}

// UpdateScheduler 修改周期任务
// @Summary 修改 GitHub 周期任务
// @Tags GitHub
// @Router /api/github/scheduler/update [post]
func UpdateScheduler(c *gin.Context) {
	var req models.GithubSchedulerUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	id := strings.TrimSpace(req.TaskID())
	if id == "" {
		response.BadRequestWithError(c, "api.required.id", nil)
		return
	}
	patch := githubrepo.SchedulePatch{}
	if req.Name != nil {
		patch.Name = req.Name
	}
	if req.Query != nil {
		patch.Query = req.Query
	}
	if req.Keywords != nil {
		patch.Keywords = []string(models.NormalizeStringList(req.Keywords))
		patch.KeywordsSet = true
	}
	patch.IntervalMinutes = req.IntervalMinutes
	patch.MaxPages = req.MaxPages
	patch.TokenRef = req.TokenRef

	if err := taskRepo.UpdateSchedule(c.Request.Context(), id, patch); err != nil {
		if errors.Is(err, githubrepo.ErrTaskNotFound) {
			response.NotFound(c, "api.not_found", err)
			return
		}
		if errors.Is(err, githubrepo.ErrInvalidID) {
			response.BadRequestWithError(c, "api.bad_request", err)
			return
		}
		logger.Error("github intel: update scheduler failed", zap.Error(err))
		response.InternalServerError(c, "api.server_error", err)
		return
	}
	task, err := taskRepo.Get(c.Request.Context(), id)
	if err != nil {
		response.InternalServerError(c, "api.server_error", err)
		return
	}
	response.Success(c, task, "api.success")
}
