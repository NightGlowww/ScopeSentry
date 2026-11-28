package github

import (
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	githubrepo "github.com/Autumn-27/ScopeSentry/internal/repositories/github"
	githubsvc "github.com/Autumn-27/ScopeSentry/internal/services/github"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// GitHub 结果面（对标 ARL-Next github_result.py / github_monitor_result.py）

const (
	// maxExportRows 导出硬上限（流式写，不把结果攒进内存）
	maxExportRows = 100000
	// exportFlushEvery 每写这么多行 flush 一次
	exportFlushEvery = 500
)

var (
	resultRepo = githubrepo.NewResultRepository()
	threatRepo = githubrepo.NewThreatRepository()
)

// resultListRequest 结果列表入参
type resultListRequest struct {
	TaskID    string `json:"taskId"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	Project   string `json:"project"`
	Keyword   string `json:"keyword"`
	PageIndex int    `json:"pageIndex"`
	PageSize  int    `json:"pageSize"`
}

func (r resultListRequest) toQuery(source string) models.GithubResultListQuery {
	return models.GithubResultListQuery{
		TaskID:  r.TaskID,
		Type:    r.Type,
		Status:  r.Status,
		Project: r.Project,
		Keyword: r.Keyword,
		Source:  source,
	}
}

// ListResults 结果列表（默认只看任务采集的结果，source=task）
// @Summary GitHub 采集结果列表
// @Tags GitHub
// @Router /api/github/result/list [post]
func ListResults(c *gin.Context) {
	listResults(c, models.GithubSourceTask)
}

// ListMonitorResults 外部监控结果列表（source=monitor，结构与 result 一致）
// @Summary GitHub 监控结果列表
// @Tags GitHub
// @Router /api/github/monitor_result/list [post]
func ListMonitorResults(c *gin.Context) {
	listResults(c, models.GithubSourceMonitor)
}

func listResults(c *gin.Context, source string) {
	var req resultListRequest
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
	query := req.toQuery(source)
	query.PageIndex = pageIndex
	query.PageSize = pageSize

	list, total, err := resultRepo.List(c.Request.Context(), query)
	if err != nil {
		logger.Error("github intel: list results failed", zap.Error(err))
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

// OverviewResults 按 type / status 统计
// @Summary GitHub 结果总览
// @Tags GitHub
// @Router /api/github/result/overview [post]
func OverviewResults(c *gin.Context) {
	var req resultListRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.BadRequestWithError(c, "api.bad_request", err)
			return
		}
	}
	query := req.toQuery("")
	rows, total, err := resultRepo.Overview(c.Request.Context(), query)
	if err != nil {
		logger.Error("github intel: overview failed", zap.Error(err))
		response.InternalServerError(c, "api.server_error", err)
		return
	}
	// 再给两份便捷视图，前端不用二次聚合
	byType := make(map[string]int64)
	byStatus := make(map[string]int64)
	for _, row := range rows {
		byType[row.Type] += row.Count
		byStatus[row.Status] += row.Count
	}
	response.Success(c, gin.H{
		"items":    rows,
		"total":    total,
		"byType":   byType,
		"byStatus": byStatus,
	}, "api.success")
}

// StatsResults 按天趋势
// @Summary GitHub 结果趋势
// @Tags GitHub
// @Router /api/github/result/stats [post]
func StatsResults(c *gin.Context) {
	var req struct {
		resultListRequest
		Days int `json:"days"`
	}
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.BadRequestWithError(c, "api.bad_request", err)
			return
		}
	}
	if req.Days <= 0 {
		req.Days = 7
	}
	items, err := resultRepo.DailyStats(c.Request.Context(), req.toQuery(""), req.Days)
	if err != nil {
		logger.Error("github intel: stats failed", zap.Error(err))
		response.InternalServerError(c, "api.server_error", err)
		return
	}
	response.Success(c, gin.H{"items": items, "days": req.Days}, "api.success")
}

// TriageResults 批量研判
// @Summary GitHub 结果研判
// @Tags GitHub
// @Router /api/github/result/triage [post]
func TriageResults(c *gin.Context) {
	var req models.GithubResultTriageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	status, err := normalizeStatus(req.Status)
	if err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	ids := normalizeIDs(req.IDs)
	if len(ids) == 0 {
		response.BadRequestWithError(c, "api.required.ids", nil)
		return
	}
	operator, _ := c.Get("username")
	operatorName, _ := operator.(string)
	matched, modified, err := resultRepo.Triage(c.Request.Context(), ids, status, operatorName)
	if err != nil {
		if errors.Is(err, githubrepo.ErrInvalidID) {
			response.BadRequestWithError(c, "api.bad_request", err)
			return
		}
		logger.Error("github intel: triage failed", zap.Error(err))
		response.InternalServerError(c, "api.server_error", err)
		return
	}
	// matched=0：结果 id 一条都不存在 → 404（此前一律 200 + updated:0）
	if matched == 0 {
		response.NotFound(c, "api.not_found", fmt.Errorf("%w: %s", githubrepo.ErrResultNotFound, strings.Join(ids, ",")))
		return
	}
	response.Success(c, gin.H{"updated": modified, "matched": matched, "status": status, "ids": ids}, "api.success")
}

// IgnoredRepos 黑名单仓库：action = list | add | remove
// @Summary GitHub 噪声仓库黑名单
// @Tags GitHub
// @Router /api/github/result/ignored_repos [post]
func IgnoredRepos(c *gin.Context) {
	var req models.GithubIgnoredReposRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.BadRequestWithError(c, "api.bad_request", err)
			return
		}
	}
	// 逻辑收到服务层：MCP 工具与 REST 共用同一段，避免两份实现慢慢分叉
	res, err := githubsvc.DefaultService().IgnoredRepos(c.Request.Context(), req.Action, req.Repo, req.Reason, req.Purge)
	if err != nil {
		if errors.Is(err, githubsvc.ErrInvalidAction) {
			response.BadRequestWithError(c, "api.github.invalid_action", nil)
			return
		}
		if strings.Contains(err.Error(), "repo is required") {
			response.BadRequestWithError(c, "api.required.repo", err)
			return
		}
		logger.Error("github intel: ignored repos failed", zap.Error(err))
		response.InternalServerError(c, "api.server_error", err)
		return
	}
	response.Success(c, res, "api.success")
}

// ExportResults 流式导出 CSV
// @Summary GitHub 结果导出
// @Tags GitHub
// @Router /api/github/result/export [get]
func ExportResults(c *gin.Context) {
	query := models.GithubResultListQuery{
		TaskID:  c.Query("taskId"),
		Type:    c.Query("type"),
		Status:  c.Query("status"),
		Project: c.Query("project"),
		Keyword: c.Query("keyword"),
		Source:  c.Query("source"),
	}

	filename := fmt.Sprintf("github_results_%s.csv", time.Now().Format("20060102_150405"))
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
	// Excel 打开 UTF-8 CSV 需要 BOM
	if _, err := c.Writer.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return
	}

	w := csv.NewWriter(c.Writer)
	_ = w.Write([]string{
		"taskId", "type", "repo", "path", "url", "commit", "title",
		"snippet", "severity", "status", "triagedBy", "triagedAt", "createdAt",
	})

	written := 0
	iterErr := resultRepo.Iterate(c.Request.Context(), query, maxExportRows, func(row *models.GithubResult) error {
		record := []string{
			row.TaskID,
			row.Type,
			row.Repo,
			row.Path,
			row.URL,
			row.Commit,
			row.Title,
			row.Snippet,
			row.Severity,
			row.Status,
			row.TriagedBy,
			formatTime(row.TriagedAt),
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
		logger.Error("github intel: export aborted", zap.Int("written", written), zap.Error(iterErr))
	}
}

func formatTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
