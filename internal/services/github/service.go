package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	githubrepo "github.com/Autumn-27/ScopeSentry/internal/repositories/github"
	"go.mongodb.org/mongo-driver/bson"
	"go.uber.org/zap"
)

// 业务编排层：任务运行（限速 / 互斥 / 去重入库）、威胁情报来源运行、token 解析。

// ErrEmptyQuery 任务没有可用查询串
var ErrEmptyQuery = errors.New("github task query is empty")

// RunSummary 一次任务运行的结果
type RunSummary struct {
	TaskID    string     `json:"taskId"`
	Type      string     `json:"type"`
	Query     string     `json:"query"`
	Pages     int        `json:"pages"`
	Scanned   int64      `json:"scanned"`
	NewFound  int64      `json:"newFound"`
	Stopped   bool       `json:"stopped"`
	Reason    string     `json:"reason,omitempty"`
	NextRunAt *time.Time `json:"nextRunAt,omitempty"`
	State     int        `json:"state"`
}

// Option 服务构造选项
type Option func(*Service)

// WithBaseURL 覆盖 GitHub API 基址（GHE / 单测假服务）
func WithBaseURL(base string) Option {
	return func(s *Service) { s.baseURL = base }
}

// WithHTTPClient 注入 http.Client（单测用）
func WithHTTPClient(hc *http.Client) Option {
	return func(s *Service) { s.httpClient = hc }
}

// WithRateFloor 覆盖限速水位（剩余低于该值即停止翻页）
func WithRateFloor(floor int) Option {
	return func(s *Service) { s.rateFloor = floor }
}

// Service GitHub 情报服务
type Service struct {
	tasks      githubrepo.TaskRepository
	results    githubrepo.ResultRepository
	threats    githubrepo.ThreatRepository
	baseURL    string
	httpClient *http.Client
	rateFloor  int

	// locks 任务级互斥：同一 task 不并发（进程内锁 + DB 状态双保险）
	locks sync.Map
}

// NewService 构造服务
func NewService(opts ...Option) *Service {
	s := &Service{
		tasks:   githubrepo.NewTaskRepository(),
		results: githubrepo.NewResultRepository(),
		threats: githubrepo.NewThreatRepository(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

var (
	defaultService *Service
	defaultOnce    sync.Once
)

// DefaultService 进程内单例
func DefaultService() *Service {
	defaultOnce.Do(func() {
		defaultService = NewService()
	})
	return defaultService
}

func (s *Service) taskLock(id string) *sync.Mutex {
	if mu, ok := s.locks.Load(id); ok {
		return mu.(*sync.Mutex)
	}
	mu, _ := s.locks.LoadOrStore(id, &sync.Mutex{})
	return mu.(*sync.Mutex)
}

// clientFor 按任务配置解析 token 并构造客户端
func (s *Service) clientFor(ctx context.Context, refs ...string) *Client {
	token, _ := s.ResolveToken(ctx, refs...)
	return NewClient(ClientOptions{
		BaseURL:    s.baseURL,
		Token:      token,
		HTTPClient: s.httpClient,
		RateFloor:  s.rateFloor,
	})
}

// TokenResolution token 解析结果
type TokenResolution struct {
	Token  string `json:"-"`
	Source string `json:"source"`
}

// ResolveToken token 解析顺序：
//  1. 任务 / 来源的 tokenRef → 系统配置集合 config 里 name=tokenRef 的 value；
//  2. github_threat_config 里任一来源的 tokenRef；
//  3. 环境变量 GITHUB_TOKEN。
//
// 返回的 Source 用于 token_status 说明「token 到底从哪来」。
func (s *Service) ResolveToken(ctx context.Context, refs ...string) (string, string) {
	candidates := make([]string, 0, len(refs)+1)
	for _, ref := range refs {
		if strings.TrimSpace(ref) != "" {
			candidates = append(candidates, strings.TrimSpace(ref))
		}
	}
	if cfg, err := s.threats.GetConfig(ctx); err == nil {
		candidates = append(candidates,
			strings.TrimSpace(cfg.CVE.TokenRef),
			strings.TrimSpace(cfg.Hacker.TokenRef),
			strings.TrimSpace(cfg.Tools.TokenRef),
		)
	}
	for _, ref := range candidates {
		if ref == "" {
			continue
		}
		if token := s.readSystemToken(ctx, ref); token != "" {
			return token, "config:" + ref
		}
	}
	if token := strings.TrimSpace(getenv(EnvToken)); token != "" {
		return token, "env:" + EnvToken
	}
	return "", "none"
}

// readSystemToken 从系统配置集合 config 读取 name=<ref> 的 value（与 configuration 服务同一存储）
func (s *Service) readSystemToken(ctx context.Context, ref string) string {
	if mongodb.DB == nil {
		return ""
	}
	var doc struct {
		Value string `bson:"value"`
	}
	if err := mongodb.DB.Collection("config").FindOne(ctx, bson.M{"name": ref}).Decode(&doc); err != nil {
		return ""
	}
	return strings.TrimSpace(doc.Value)
}

// searchKind 任务类型 → Search API 类型
func searchKind(taskType string) string {
	switch taskType {
	case models.GithubTypeCVE:
		return SearchKindIssues
	case models.GithubTypeHacker:
		return SearchKindRepositories
	default:
		return SearchKindCode
	}
}

// RunTask 同步跑一次任务。
//
// 语义：
//   - 互斥：进程内锁 + DB 状态（state=1 时拒绝并发）；
//   - 去重：入库走 (taskId,repo,path,commit) upsert，重复命中不增加 newFound；
//   - 限速：Search 响应剩余额度不足时立即停止，写 nextRunAt=reset 并把状态放回待开始；
//   - 统计：stats 为累计值（scanned 累加命中数，newFound 累加新增文档数）。
func (s *Service) RunTask(ctx context.Context, id string) (*RunSummary, error) {
	mu := s.taskLock(id)
	if !mu.TryLock() {
		return nil, githubrepo.ErrTaskBusy
	}
	defer mu.Unlock()

	prev, err := s.tasks.Claim(ctx, id)
	if err != nil {
		return nil, err
	}

	summary := &RunSummary{TaskID: id, Type: prev.Type, Query: prev.EffectiveQuery()}
	stats := prev.Stats

	finish := func(state int, nextRunAt *time.Time, lastErr string) *RunSummary {
		now := time.Now()
		if err := s.tasks.Finish(ctx, id, state, stats, &now, nextRunAt, lastErr); err != nil {
			logger.Error("github intel: finish task failed", zap.String("taskId", id), zap.Error(err))
		}
		summary.State = state
		summary.NextRunAt = nextRunAt
		return summary
	}

	if summary.Query == "" {
		summary.Stopped = true
		summary.Reason = ErrEmptyQuery.Error()
		return finish(models.GithubTaskStateWaiting, nil, ErrEmptyQuery.Error()), ErrEmptyQuery
	}

	client := s.clientFor(ctx, prev.Config.TokenRef)
	kind := searchKind(prev.Type)
	maxPages := prev.Config.MaxPages
	if maxPages <= 0 {
		maxPages = models.GithubDefaultMaxPages
	}
	const perPage = 100

	for page := 1; page <= maxPages; page++ {
		res, searchErr := client.Search(ctx, kind, summary.Query, page, perPage)
		if searchErr != nil {
			var rlErr *RateLimitedError
			if errors.As(searchErr, &rlErr) {
				next := rlErr.Reset
				summary.Stopped = true
				summary.Reason = fmt.Sprintf("rate limit 不足，停止采集（%s 恢复）", next.Format(time.RFC3339))
				logger.Warn("github intel: rate limited, stop task",
					zap.String("taskId", id), zap.Time("reset", next))
				return finish(models.GithubTaskStateWaiting, &next, summary.Reason), nil
			}
			if page == 1 {
				summary.Stopped = true
				summary.Reason = searchErr.Error()
				return finish(models.GithubTaskStateWaiting, nil, searchErr.Error()), searchErr
			}
			// 后续页失败：保留已入库结果，按完成收尾
			logger.Warn("github intel: search page failed, stop paging",
				zap.String("taskId", id), zap.Int("page", page), zap.Error(searchErr))
			break
		}

		summary.Pages = page
		hits := make([]githubrepo.Hit, 0, len(res.Items))
		for _, item := range res.Items {
			hits = append(hits, githubrepo.Hit{
				Repo:     item.Repo,
				Path:     item.Path,
				URL:      item.URL,
				Commit:   item.Commit,
				Title:    item.Title,
				Snippet:  item.Snippet,
				Severity: ScoreSeverity(item.Repo, item.Path, item.Snippet),
				Keyword:  summary.Query,
			})
		}

		up, upErr := s.results.UpsertHits(ctx, id, prev.Type, prev.Project, models.GithubSourceTask, hits)
		if upErr != nil {
			summary.Stopped = true
			summary.Reason = upErr.Error()
			return finish(models.GithubTaskStateWaiting, nil, upErr.Error()), upErr
		}
		summary.Scanned += up.Scanned
		summary.NewFound += up.Inserted
		stats.Scanned += up.Scanned
		stats.NewFound += up.Inserted

		// 先看额度再决定是否继续：额度不足时即便当前页还有数据也要停，
		// 否则下一轮翻页必然 403，nextRunAt 也就写不出来。
		if rl, ok := client.Exhausted(guessResource("/search/"+kind), time.Now()); ok {
			next := rl.Reset
			summary.Stopped = true
			summary.Reason = fmt.Sprintf("rate limit 剩余 %d，停止采集（%s 恢复）", rl.Remaining, next.Format(time.RFC3339))
			logger.Warn("github intel: rate limit exhausted, stop task",
				zap.String("taskId", id), zap.Int("remaining", rl.Remaining), zap.Time("reset", next))
			return finish(models.GithubTaskStateWaiting, &next, summary.Reason), nil
		}

		if len(res.Items) < perPage {
			break
		}
	}

	nextRunAt := nextRunForInterval(prev.Config.IntervalMinutes, time.Now())
	return finish(models.GithubTaskStateDone, nextRunAt, ""), nil
}

// nextRunForInterval 周期任务下一次运行时间；未配置周期返回 nil（一次性任务）
func nextRunForInterval(intervalMinutes int, now time.Time) *time.Time {
	if intervalMinutes <= 0 {
		return nil
	}
	next := now.Add(time.Duration(intervalMinutes) * time.Minute)
	return &next
}

// StopTasks 暂停任务（调度器跳过）
func (s *Service) StopTasks(ctx context.Context, ids []string) (int64, error) {
	return s.tasks.SetState(ctx, ids, models.GithubTaskStatePaused)
}

// RecoverTasks 恢复被暂停的任务：状态回到待开始，并清掉 nextRunAt 让它立刻可跑
func (s *Service) RecoverTasks(ctx context.Context, ids []string) (int64, error) {
	var total int64
	for _, id := range ids {
		task, err := s.tasks.Get(ctx, id)
		if err != nil {
			return total, err
		}
		if task.State != models.GithubTaskStatePaused {
			return total, fmt.Errorf("任务 %s 不是暂停状态，无法恢复", id)
		}
		if err := s.tasks.Finish(ctx, id, models.GithubTaskStateWaiting, task.Stats, task.LastRunAt, nil, ""); err != nil {
			return total, err
		}
		total++
	}
	return total, nil
}

// DeleteTasks 删除任务及其结果
func (s *Service) DeleteTasks(ctx context.Context, ids []string) (int64, int64, error) {
	deleted, err := s.tasks.Delete(ctx, ids)
	if err != nil {
		return 0, 0, err
	}
	removed, err := s.results.DeleteByTaskIDs(ctx, ids)
	if err != nil {
		return deleted, 0, err
	}
	return deleted, removed, nil
}

// getenv 读环境变量（保留一层包装，单测可替换）
var getenv = func(key string) string { return strings.TrimSpace(os.Getenv(key)) }
