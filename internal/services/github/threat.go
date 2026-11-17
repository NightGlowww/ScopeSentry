package github

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/repositories/github"
	"go.uber.org/zap"
)

// 威胁情报来源运行（对标 ARL-Next github_threat.py 的 cve / hackers / tools 三来源）。

// ThreatRunSummary 单个来源一次运行的统计
type ThreatRunSummary struct {
	Kind      string     `json:"kind"`
	Queries   int        `json:"queries"`
	Scanned   int64      `json:"scanned"`
	NewFound  int64      `json:"newFound"`
	Stopped   bool       `json:"stopped"`
	Reason    string     `json:"reason,omitempty"`
	NextRunAt *time.Time `json:"nextRunAt,omitempty"`
}

// ThreatSource 取来源配置
func (s *Service) ThreatSource(ctx context.Context, kind string) (models.GithubThreatSource, error) {
	cfg, err := s.threats.GetConfig(ctx)
	if err != nil {
		return models.GithubThreatSource{}, err
	}
	switch kind {
	case models.GithubThreatKindCVE:
		return cfg.CVE, nil
	case models.GithubThreatKindHacker:
		return cfg.Hacker, nil
	case models.GithubThreatKindTools:
		return cfg.Tools, nil
	default:
		return models.GithubThreatSource{}, fmt.Errorf("unknown threat source: %s", kind)
	}
}

// SaveThreatSource 保存来源配置（enabled / keywords / intervalHours / tokenRef / targets）
func (s *Service) SaveThreatSource(ctx context.Context, kind string, src models.GithubThreatSource, operator string) error {
	if _, err := github.SourceField(kind); err != nil {
		return err
	}
	return s.threats.SaveSource(ctx, kind, src)
}

// RunThreatSource 跑一次情报来源。
//
// 三来源语义：
//   - cve：按关键词搜 GitHub issues（CVE 公告 / POC 讨论），命中写 threat_history；
//   - hacker：按 targets 里的攻击者 ID 搜其仓库（user:<id>），命中写 threat_history；
//   - tools：按 targets 里的 owner/repo 查最新 release，有 tag 就记一条。
//
// 限速处理与任务一致：额度不足立即停止并写 nextRunAt。
func (s *Service) RunThreatSource(ctx context.Context, kind string) (*ThreatRunSummary, error) {
	src, err := s.ThreatSource(ctx, kind)
	if err != nil {
		return nil, err
	}
	summary := &ThreatRunSummary{Kind: kind}

	queries := s.threatQueries(kind, src)
	summary.Queries = len(queries)
	if len(queries) == 0 {
		next := time.Now().Add(time.Duration(src.IntervalHours) * time.Hour)
		if touchErr := s.threats.TouchSourceRun(ctx, kind, time.Now(), &next, "没有配置关键词或目标"); touchErr != nil {
			logger.Error("github intel: touch threat source failed", zap.String("kind", kind), zap.Error(touchErr))
		}
		summary.NextRunAt = &next
		summary.Reason = "没有配置关键词或目标"
		return summary, nil
	}

	client := s.clientFor(ctx, src.TokenRef)

	for _, q := range queries {
		switch kind {
		case models.GithubThreatKindTools:
			// tools：q 是 owner/repo，直接查最新 release
			rel, relErr := client.LatestRelease(ctx, q)
			if relErr != nil {
				if _, stop := s.handleThreatErr(relErr, summary, src, ctx, kind); stop {
					return summary, nil
				}
				logger.Warn("github intel: tools release failed", zap.String("repo", q), zap.Error(relErr))
				continue
			}
			if rel == nil || rel.TagName == "" {
				continue
			}
			summary.Scanned++
			item := &models.GithubThreatHistory{
				Kind:        kind,
				Key:         q + "@" + rel.TagName,
				Title:       fmt.Sprintf("%s 发布 %s", q, rel.TagName),
				URL:         rel.HTMLURL,
				Repo:        q,
				Description: truncateText(rel.Body, 500),
				Severity:    "medium",
				Keyword:     q,
			}
			if isNew, upErr := s.threats.UpsertHistory(ctx, item); upErr != nil {
				return summary, upErr
			} else if isNew {
				summary.NewFound++
			}
		default:
			res, searchErr := client.Search(ctx, threatSearchKind(kind), q, 1, 50)
			if searchErr != nil {
				if _, stop := s.handleThreatErr(searchErr, summary, src, ctx, kind); stop {
					return summary, nil
				}
				logger.Warn("github intel: threat search failed", zap.String("kind", kind), zap.String("query", q), zap.Error(searchErr))
				continue
			}
			for _, it := range res.Items {
				summary.Scanned++
				item := &models.GithubThreatHistory{
					Kind:        kind,
					Key:         threatKey(kind, it),
					Title:       firstNonEmpty(it.Title, it.Path, it.Repo),
					URL:         it.URL,
					Repo:        it.Repo,
					Description: truncateText(it.Snippet, 500),
					Severity:    ScoreSeverity(it.Repo, it.Path, it.Snippet),
					Keyword:     q,
				}
				isNew, upErr := s.threats.UpsertHistory(ctx, item)
				if upErr != nil {
					return summary, upErr
				}
				if isNew {
					summary.NewFound++
				}
			}
		}
	}

	next := time.Now().Add(time.Duration(src.IntervalHours) * time.Hour)
	summary.NextRunAt = &next
	if err := s.threats.TouchSourceRun(ctx, kind, time.Now(), &next,
		fmt.Sprintf("scanned=%d new=%d", summary.Scanned, summary.NewFound)); err != nil {
		logger.Error("github intel: touch threat source failed", zap.String("kind", kind), zap.Error(err))
	}
	return summary, nil
}

// handleThreatErr 限速错误的统一收尾；返回 (是否已处理, 是否应终止整轮)
func (s *Service) handleThreatErr(err error, summary *ThreatRunSummary, src models.GithubThreatSource,
	ctx context.Context, kind string) (*ThreatRunSummary, bool) {
	var rlErr *RateLimitedError
	if errors.As(err, &rlErr) {
		next := rlErr.Reset
		summary.Stopped = true
		summary.Reason = fmt.Sprintf("rate limit 不足，停止采集（%s 恢复）", next.Format(time.RFC3339))
		summary.NextRunAt = &next
		if touchErr := s.threats.TouchSourceRun(ctx, kind, time.Now(), &next, summary.Reason); touchErr != nil {
			logger.Error("github intel: touch threat source failed", zap.String("kind", kind), zap.Error(touchErr))
		}
		return summary, true
	}
	return nil, false
}

// ThreatQueries 暴露给 handler 的查询预览（run_once 前的自检用）
func (s *Service) ThreatQueries(ctx context.Context, kind string) ([]string, error) {
	src, err := s.ThreatSource(ctx, kind)
	if err != nil {
		return nil, err
	}
	return s.threatQueries(kind, src), nil
}

func (s *Service) threatQueries(kind string, src models.GithubThreatSource) []string {
	out := make([]string, 0, len(src.Keywords)+len(src.Targets))
	switch kind {
	case models.GithubThreatKindHacker:
		for _, t := range src.Targets {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			out = append(out, "user:"+t)
		}
	case models.GithubThreatKindTools:
		for _, t := range src.Targets {
			t = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(t, "https://github.com/"), "/"))
			if t == "" {
				continue
			}
			out = append(out, t)
		}
	default:
		for _, kw := range src.Keywords {
			kw = strings.TrimSpace(kw)
			if kw == "" {
				continue
			}
			out = append(out, kw)
		}
	}
	return out
}

func threatSearchKind(kind string) string {
	if kind == models.GithubThreatKindHacker {
		return SearchKindRepositories
	}
	return SearchKindIssues
}

func threatKey(kind string, item SearchItem) string {
	return strings.Join([]string{kind, item.Repo, item.Path, item.Commit}, "|")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func truncateText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// TokenStatusResult token_status 端点返回体
type TokenStatusResult struct {
	Status  string     `json:"status"` // valid | rate_limited | invalid | missing | error
	Message string     `json:"msg"`
	Source  string     `json:"source"`
	Core    *RateLimit `json:"core,omitempty"`
	Search  *RateLimit `json:"search,omitempty"`
}

// TokenStatus 检查 token 状态与额度（对标 ARL-Next /github_threat/token_status）
func (s *Service) TokenStatus(ctx context.Context) *TokenStatusResult {
	token, source := s.ResolveToken(ctx)
	if token == "" {
		return &TokenStatusResult{Status: "missing", Message: "未配置 Token", Source: source}
	}
	client := NewClient(ClientOptions{
		BaseURL:    s.baseURL,
		Token:      token,
		HTTPClient: s.httpClient,
		RateFloor:  s.rateFloor,
	})
	snap, err := client.GetRateLimit(ctx)
	if err != nil {
		if errors.Is(err, ErrUnauthorized) {
			return &TokenStatusResult{Status: "invalid", Message: "Token 无效或已过期", Source: source}
		}
		return &TokenStatusResult{Status: "error", Message: "网络请求失败: " + err.Error(), Source: source}
	}
	res := &TokenStatusResult{Status: "valid", Source: source, Core: &snap.Core, Search: &snap.Search}
	res.Message = fmt.Sprintf("生效中 (Core: %d/%d, Search: %d/%d)",
		snap.Core.Remaining, snap.Core.Limit, snap.Search.Remaining, snap.Search.Limit)
	if snap.Core.Remaining <= 0 || (snap.Search.Limit > 0 && snap.Search.Remaining <= 0) {
		res.Status = "rate_limited"
		res.Message = fmt.Sprintf("限流中 (Core: %d, Search: %d)", snap.Core.Remaining, snap.Search.Remaining)
	}
	return res
}
