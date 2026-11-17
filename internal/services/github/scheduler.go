package github

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	githubrepo "github.com/Autumn-27/ScopeSentry/internal/repositories/github"
	corescheduler "github.com/Autumn-27/ScopeSentry/internal/scheduler"
	"go.uber.org/zap"
)

// GitHub 情报调度器。
//
// 复用仓库自带的 gocron 调度器（internal/scheduler）：注册一个 handler，
// 每分钟 tick 一次，tick 内部按「配置间隔」判断每个任务 / 情报来源是否到点。
// 这样既不改动扫描任务的调度语义，也能让 github 的周期在重启后自动恢复
// （job 文档落在 scheduled 集合里，由 loadJobs 重新装载）。
//
// 测试与单次触发走 Tick / RunDue，不依赖 gocron。

const (
	// JobHandlerName 注册到内置调度器的 handler 名
	JobHandlerName = "github_intel"
	// JobDocumentID scheduled 集合里的任务 ID
	JobDocumentID = "github_intel_tick"
	// JobCron 每分钟一次
	JobCron = "* * * * *"
	// tickTimeBudget 单次 tick 的总预算，避免拖死调度器
	tickTimeBudget = 10 * time.Minute
	// dueTaskLimit 单次 tick 最多跑多少个到期任务
	dueTaskLimit = 20
)

// TickResult 一次 tick 的结果
type TickResult struct {
	Threats []*ThreatRunSummary `json:"threats"`
	Tasks   []*RunSummary       `json:"tasks"`
	Errors  []string            `json:"errors,omitempty"`
}

// Scheduler GitHub 情报调度器
type Scheduler struct {
	svc      *Service
	once     sync.Once
	stop     chan struct{}
	mu       sync.Mutex
	stopOnce sync.Once
}

var (
	defaultScheduler *Scheduler
	schedulerOnce    sync.Once
)

// NewScheduler 构造调度器
func NewScheduler(svc *Service) *Scheduler {
	if svc == nil {
		svc = DefaultService()
	}
	return &Scheduler{svc: svc, stop: make(chan struct{})}
}

// DefaultScheduler 进程内单例
func DefaultScheduler() *Scheduler {
	schedulerOnce.Do(func() {
		defaultScheduler = NewScheduler(DefaultService())
	})
	return defaultScheduler
}

// Start 注册到内置调度器（幂等）。
//
// 注册失败（例如单测环境没有初始化全局调度器、或 Mongo 不可用）时只记日志，
// 不影响 HTTP 面；此时 tick 仍可由 RunDue 手动触发。
func (s *Scheduler) Start() {
	s.once.Do(func() {
		if mongodb.DB != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := githubrepo.EnsureIndexes(ctx, mongodb.DB); err != nil {
				logger.Error("github intel: ensure indexes failed", zap.Error(err))
			}
			cancel()
		}

		global := corescheduler.GetGlobalScheduler()
		if global == nil {
			logger.Warn("github intel: global scheduler not initialized, scheduler tick disabled")
			return
		}
		global.RegisterHandler(JobHandlerName, func(_ string, _ string) error {
			ctx, cancel := context.WithTimeout(context.Background(), tickTimeBudget)
			defer cancel()
			res, err := s.Tick(ctx)
			if err != nil {
				return err
			}
			logger.Info("github intel: tick done",
				zap.Int("threats", len(res.Threats)), zap.Int("tasks", len(res.Tasks)))
			return nil
		})
		job := &corescheduler.Job{
			ID:        JobDocumentID,
			Name:      "GitHub Intel Tick",
			Schedule:  JobCron,
			Handler:   JobHandlerName,
			CycleType: "cron",
		}
		if err := global.AddJob(job); err != nil {
			logger.Error("github intel: register scheduler job failed", zap.Error(err))
			return
		}
		logger.Info("github intel: scheduler job registered", zap.String("cron", JobCron))
	})
}

// Stop 停止调度器（job 文档保留，重启后自动恢复）
func (s *Scheduler) Stop() {
	s.stopOnce.Do(func() {
		close(s.stop)
	})
}

// Tick 一次调度：先跑到点的情报来源，再跑到点的周期任务。
func (s *Scheduler) Tick(ctx context.Context) (*TickResult, error) {
	result := &TickResult{Threats: []*ThreatRunSummary{}, Tasks: []*RunSummary{}}

	cfg, err := s.svc.threats.GetConfig(ctx)
	if err != nil {
		return result, err
	}
	for _, kind := range []string{models.GithubThreatKindCVE, models.GithubThreatKindHacker, models.GithubThreatKindTools} {
		src := sourceOf(cfg, kind)
		if !src.Enabled || !due(src.NextRunAt, time.Now()) {
			continue
		}
		summary, runErr := s.svc.RunThreatSource(ctx, kind)
		if runErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", kind, runErr))
			continue
		}
		result.Threats = append(result.Threats, summary)
	}

	tasks, err := s.svc.tasks.DueScheduled(ctx, time.Now(), dueTaskLimit)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}
	for _, task := range tasks {
		// 已暂停 / 正在运行的跳过（Claim 里还有一层状态互斥）
		if task.State == models.GithubTaskStatePaused {
			continue
		}
		summary, runErr := s.svc.RunTask(ctx, task.ID.Hex())
		if runErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", task.ID.Hex(), runErr))
			continue
		}
		result.Tasks = append(result.Tasks, summary)
	}
	return result, nil
}

// due 是否到点（未设置 nextRunAt 视为立即可跑）
func due(next *time.Time, now time.Time) bool {
	if next == nil || next.IsZero() {
		return true
	}
	return !next.After(now)
}

func sourceOf(cfg *models.GithubThreatConfig, kind string) models.GithubThreatSource {
	switch kind {
	case models.GithubThreatKindCVE:
		return cfg.CVE
	case models.GithubThreatKindHacker:
		return cfg.Hacker
	case models.GithubThreatKindTools:
		return cfg.Tools
	default:
		return models.GithubThreatSource{}
	}
}
