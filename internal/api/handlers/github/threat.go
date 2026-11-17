package github

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// GitHub 威胁情报面（对标 ARL-Next github_threat.py）。
//
// 三来源 cve / hackers / tools：
//   - *_config    读/写单例 github_threat_config 里对应来源（开关 / 关键词 / 间隔 / tokenRef / targets）
//   - *_target    hackers 与 tools 的监控目标增删查（存在来源 targets 数组里）
//   - *_run_once  立即跑一次（同步，返回本次统计）
//   - cve_history 情报发现历史
//   - token_status token 有效性与额度

// cveHistoryRequest 历史查询入参
type cveHistoryRequest struct {
	Keyword   string `json:"keyword"`
	Limit     int    `json:"limit"`
	PageIndex int    `json:"pageIndex"`
	PageSize  int    `json:"pageSize"`
}

// CVEHistory CVE 情报历史
// @Summary CVE 情报历史
// @Tags GitHub
// @Router /api/github/threat/cve_history [post]
func CVEHistory(c *gin.Context) {
	var req cveHistoryRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.BadRequestWithError(c, "api.bad_request", err)
			return
		}
	}
	if req.PageSize > 0 {
		if _, _, err := parsePageParams(req.PageIndex, req.PageSize); err != nil {
			response.BadRequestWithError(c, "api.bad_request", err)
			return
		}
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	items, err := threatRepo.ListHistory(c.Request.Context(), models.GithubThreatKindCVE, limit)
	if err != nil {
		logger.Error("github intel: cve history failed", zap.Error(err))
		response.InternalServerError(c, "api.server_error", err)
		return
	}
	total, err := threatRepo.CountHistory(c.Request.Context(), models.GithubThreatKindCVE, req.Keyword)
	if err != nil {
		logger.Error("github intel: cve history count failed", zap.Error(err))
		response.InternalServerError(c, "api.server_error", err)
		return
	}
	response.Success(c, gin.H{"list": items, "total": total, "limit": limit}, "api.success")
}

// HackersTarget 攻击者监控目标：action = list | add | remove
// @Summary 攻击者监控目标
// @Tags GitHub
// @Router /api/github/threat/hackers_target [post]
func HackersTarget(c *gin.Context) {
	manageTargets(c, models.GithubThreatKindHacker)
}

// ToolsTarget 工具仓库监控目标：action = list | add | remove
// @Summary 工具仓库监控目标
// @Tags GitHub
// @Router /api/github/threat/tools_target [post]
func ToolsTarget(c *gin.Context) {
	manageTargets(c, models.GithubThreatKindTools)
}

func manageTargets(c *gin.Context, kind string) {
	var req models.GithubTargetRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.BadRequestWithError(c, "api.bad_request", err)
			return
		}
	}
	action := req.Action
	if action == "" {
		action = "list"
	}
	ctx := c.Request.Context()

	src, err := svc().ThreatSource(ctx, kind)
	if err != nil {
		logger.Error("github intel: read threat source failed", zap.String("kind", kind), zap.Error(err))
		response.InternalServerError(c, "api.server_error", err)
		return
	}

	switch action {
	case "list":
		response.Success(c, gin.H{"items": src.Targets, "total": len(src.Targets), "kind": kind}, "api.success")
	case "add", "remove":
		targets := normalizeTargets(kind, req.AllTargets())
		if len(targets) == 0 {
			response.BadRequestWithError(c, "api.bad_request", errors.New("targets 不能为空"))
			return
		}
		if action == "add" {
			src.Targets = mergeTargets(src.Targets, targets)
		} else {
			src.Targets = minusTargets(src.Targets, targets)
		}
		if err := svc().SaveThreatSource(ctx, kind, src, ""); err != nil {
			logger.Error("github intel: save targets failed", zap.String("kind", kind), zap.Error(err))
			response.InternalServerError(c, "api.server_error", err)
			return
		}
		response.Success(c, gin.H{
			"items": src.Targets, "total": len(src.Targets), "kind": kind, "action": action,
		}, "api.success")
	default:
		response.BadRequestWithError(c, "api.bad_request", errors.New("action 只能是 list / add / remove"))
	}
}

// normalizeTargets 归一监控目标：hacker 去掉 @ 与主页 URL 前缀；tools 归一成 owner/repo
func normalizeTargets(kind string, raw []string) []string {
	out := make([]string, 0, len(raw))
	for _, t := range raw {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		switch kind {
		case models.GithubThreatKindHacker:
			t = strings.TrimPrefix(t, "@")
			t = strings.TrimPrefix(t, "https://github.com/")
			t = strings.TrimPrefix(t, "http://github.com/")
			t = strings.Trim(t, "/")
			if parts := strings.Split(t, "/"); len(parts) > 0 {
				t = parts[0]
			}
		case models.GithubThreatKindTools:
			t = strings.TrimPrefix(t, "https://github.com/")
			t = strings.TrimPrefix(t, "http://github.com/")
			t = strings.TrimSuffix(t, ".git")
			t = strings.Trim(t, "/")
			if len(strings.Split(t, "/")) < 2 {
				continue
			}
		}
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

func mergeTargets(existing, add []string) []string {
	out := make([]string, 0, len(existing)+len(add))
	seen := make(map[string]struct{}, len(existing)+len(add))
	for _, t := range append(append([]string{}, existing...), add...) {
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

func minusTargets(existing, remove []string) []string {
	drop := make(map[string]struct{}, len(remove))
	for _, t := range remove {
		drop[t] = struct{}{}
	}
	out := make([]string, 0, len(existing))
	for _, t := range existing {
		if _, ok := drop[t]; ok {
			continue
		}
		out = append(out, t)
	}
	return out
}

// CVEConfig CVE 来源配置
// @Summary CVE 来源配置
// @Tags GitHub
// @Router /api/github/threat/cve_config [post]
func CVEConfig(c *gin.Context) { manageSourceConfig(c, models.GithubThreatKindCVE) }

// HackersConfig hackers 来源配置
// @Summary hackers 来源配置
// @Tags GitHub
// @Router /api/github/threat/hackers_config [post]
func HackersConfig(c *gin.Context) { manageSourceConfig(c, models.GithubThreatKindHacker) }

// ToolsConfig tools 来源配置
// @Summary tools 来源配置
// @Tags GitHub
// @Router /api/github/threat/tools_config [post]
func ToolsConfig(c *gin.Context) { manageSourceConfig(c, models.GithubThreatKindTools) }

func manageSourceConfig(c *gin.Context, kind string) {
	ctx := c.Request.Context()
	src, err := svc().ThreatSource(ctx, kind)
	if err != nil {
		logger.Error("github intel: read source config failed", zap.String("kind", kind), zap.Error(err))
		response.InternalServerError(c, "api.server_error", err)
		return
	}

	// 无 body = 纯读取
	if c.Request.ContentLength == 0 {
		response.Success(c, gin.H{"kind": kind, "config": src}, "api.success")
		return
	}

	var req models.GithubThreatConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	if req.Enabled != nil {
		src.Enabled = *req.Enabled
	}
	if req.Keywords != nil {
		src.Keywords = []string(models.NormalizeStringList(req.Keywords))
	}
	if req.IntervalHours != nil {
		if *req.IntervalHours <= 0 {
			response.BadRequestWithError(c, "api.bad_request", errors.New("intervalHours 必须大于 0"))
			return
		}
		src.IntervalHours = *req.IntervalHours
	}
	if req.TokenRef != nil {
		src.TokenRef = strings.TrimSpace(*req.TokenRef)
	}
	if req.Targets != nil {
		src.Targets = mergeTargets([]string{}, normalizeTargets(kind, req.Targets))
	}
	if err := svc().SaveThreatSource(ctx, kind, src, ""); err != nil {
		logger.Error("github intel: save source config failed", zap.String("kind", kind), zap.Error(err))
		response.InternalServerError(c, "api.server_error", err)
		return
	}
	response.Success(c, gin.H{"kind": kind, "config": src}, "api.success")
}

// CVERunOnce 立即跑一次 CVE 来源
// @Summary 立即运行 CVE 情报
// @Tags GitHub
// @Router /api/github/threat/cve_run_once [post]
func CVERunOnce(c *gin.Context) { runSourceOnce(c, models.GithubThreatKindCVE) }

// HackersRunOnce 立即跑一次 hackers 来源
// @Summary 立即运行 hackers 情报
// @Tags GitHub
// @Router /api/github/threat/hackers_run_once [post]
func HackersRunOnce(c *gin.Context) { runSourceOnce(c, models.GithubThreatKindHacker) }

// ToolsRunOnce 立即跑一次 tools 来源
// @Summary 立即运行 tools 情报
// @Tags GitHub
// @Router /api/github/threat/tools_run_once [post]
func ToolsRunOnce(c *gin.Context) { runSourceOnce(c, models.GithubThreatKindTools) }

func runSourceOnce(c *gin.Context, kind string) {
	summary, err := svc().RunThreatSource(c.Request.Context(), kind)
	if err != nil {
		logger.Error("github intel: run source once failed", zap.String("kind", kind), zap.Error(err))
		c.JSON(http.StatusBadRequest, response.Response{
			Code:    http.StatusBadRequest,
			Message: "api.bad_request",
			Data:    err.Error(),
		})
		return
	}
	response.Success(c, summary, "api.success")
}

// TokenStatus token 有效性与额度
// @Summary GitHub token 状态
// @Tags GitHub
// @Router /api/github/threat/token_status [post]
func TokenStatus(c *gin.Context) {
	status := svc().TokenStatus(c.Request.Context())
	response.Success(c, status, "api.success")
}
