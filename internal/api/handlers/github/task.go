package github

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	githubrepo "github.com/Autumn-27/ScopeSentry/internal/repositories/github"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// GitHub 采集任务（对标 ARL-Next github_task.py）

var taskRepo = githubrepo.NewTaskRepository()

// taskListRequest 任务列表入参
type taskListRequest struct {
	State     *int   `json:"state"`
	Type      string `json:"type"`
	Keyword   string `json:"keyword"`
	Scheduled *bool  `json:"scheduled"`
	PageIndex int    `json:"pageIndex"`
	PageSize  int    `json:"pageSize"`
}

// ListTasks 任务列表
// @Summary GitHub 采集任务列表
// @Tags GitHub
// @Router /api/github/task/list [post]
func ListTasks(c *gin.Context) {
	var req taskListRequest
	// 空 body 等价于默认分页：只有真的带了 body 才校验
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

	query := models.GithubTaskListQuery{
		State:     req.State,
		Type:      strings.TrimSpace(req.Type),
		Keyword:   strings.TrimSpace(req.Keyword),
		PageIndex: pageIndex,
		PageSize:  pageSize,
	}
	if req.Scheduled != nil {
		query.ScheduledOnly = *req.Scheduled
	}

	list, total, err := taskRepo.List(c.Request.Context(), query)
	if err != nil {
		logger.Error("github intel: list tasks failed", zap.Error(err))
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

// CreateTask 新建任务
// @Summary 新建 GitHub 采集任务
// @Tags GitHub
// @Router /api/github/task/create [post]
func CreateTask(c *gin.Context) {
	var req models.GithubTaskCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	name := strings.TrimSpace(req.Name)
	query := strings.TrimSpace(req.Query)
	keywords := []string(models.NormalizeStringList(req.Keywords))
	if name == "" {
		name = firstNonEmpty(query, models.JoinKeywords(keywords))
	}
	if name == "" {
		response.BadRequestWithError(c, "api.bad_request", errors.New("name 与 query 不能同时为空"))
		return
	}
	if query == "" && len(keywords) == 0 {
		response.BadRequestWithError(c, "api.bad_request", errors.New("query 或 keywords 至少填一个"))
		return
	}
	taskType := strings.TrimSpace(req.Type)
	switch taskType {
	case models.GithubTypeCodeLeak, models.GithubTypeCVE, models.GithubTypeHacker:
	case "":
		taskType = models.GithubTypeCodeLeak
	default:
		response.BadRequestWithError(c, "api.bad_request", errors.New("type 只能是 code_leak / cve / hacker"))
		return
	}
	if req.IntervalMinutes < 0 {
		response.BadRequestWithError(c, "api.bad_request", errors.New("intervalMinutes 不能为负"))
		return
	}

	task := &models.GithubTask{
		Name:      name,
		Query:     query,
		Keywords:  keywords,
		Type:      taskType,
		State:     models.GithubTaskStateWaiting,
		Project:   strings.TrimSpace(req.Project),
		CreatedAt: time.Now(),
		Config: models.GithubTaskConfig{
			TokenRef:        strings.TrimSpace(req.TokenRef),
			IntervalMinutes: req.IntervalMinutes,
			MaxPages:        req.MaxPages,
		},
	}
	if err := taskRepo.Create(c.Request.Context(), task); err != nil {
		logger.Error("github intel: create task failed", zap.Error(err))
		response.InternalServerError(c, "api.server_error", err)
		return
	}
	response.Success(c, task, "api.success")
}

// StopTasks 暂停任务
// @Summary 暂停 GitHub 采集任务
// @Tags GitHub
// @Router /api/github/task/stop [post]
func StopTasks(c *gin.Context) {
	var req models.GithubIDListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	ids := normalizeIDs(req.AllIDs())
	if len(ids) == 0 {
		response.BadRequestWithError(c, "api.bad_request", errors.New("ids 不能为空"))
		return
	}
	updated, err := svc().StopTasks(c.Request.Context(), ids)
	if err != nil {
		status := http.StatusInternalServerError
		key := "api.server_error"
		if errors.Is(err, githubrepo.ErrInvalidID) {
			status, key = http.StatusBadRequest, "api.bad_request"
		}
		logger.Error("github intel: stop tasks failed", zap.Error(err))
		c.JSON(status, response.Response{Code: status, Message: key, Data: err.Error()})
		return
	}
	response.Success(c, gin.H{"updated": updated, "ids": ids}, "api.success")
}

// DeleteTasks 删除任务（连带删除其结果）
// @Summary 删除 GitHub 采集任务
// @Tags GitHub
// @Router /api/github/task/delete [post]
func DeleteTasks(c *gin.Context) {
	var req models.GithubIDListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	ids := normalizeIDs(req.AllIDs())
	if len(ids) == 0 {
		response.BadRequestWithError(c, "api.bad_request", errors.New("ids 不能为空"))
		return
	}
	tasks, results, err := svc().DeleteTasks(c.Request.Context(), ids)
	if err != nil {
		logger.Error("github intel: delete tasks failed", zap.Error(err))
		if errors.Is(err, githubrepo.ErrInvalidID) {
			response.BadRequestWithError(c, "api.bad_request", err)
			return
		}
		response.InternalServerError(c, "api.server_error", err)
		return
	}
	// 一条都没删掉说明这些 id 根本不存在：返回 404，而不是 200 + 全零
	if tasks == 0 && results == 0 {
		response.NotFound(c, "api.not_found", fmt.Errorf("%w: %s", githubrepo.ErrTaskNotFound, strings.Join(ids, ",")))
		return
	}
	response.Success(c, gin.H{"deletedTasks": tasks, "deletedResults": results, "ids": ids}, "api.success")
}
