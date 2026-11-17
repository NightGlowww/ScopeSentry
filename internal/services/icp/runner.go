package icp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.uber.org/zap"
)

// 任务执行器：goroutine + 状态轮询。
//
// 一个 worker 的执行顺序：
//  1. 读任务快照（queries / cursor / stats / provider）；
//  2. 逐个查询：先确认任务仍是 state=1（stop 会把状态改成 2），再调 provider（带重试）；
//  3. 每完成一个查询落一次 cursor + stats，然后看下一个；
//  4. 全部完成 → FinishRun(done)；provider 失败 → FinishRun(paused, lastError)。
//
// 中途被 stop 时 cursor 停在「下一个待处理查询」，resume 直接从这个位置继续，
// 已经入过库的结果不会重跑（就算重跑，唯一索引也会把 found 记成 0）。

// launch 起一个后台 worker；任务级互斥由 DB Claim 保证，这里不再加进程锁
func (s *Service) launch(taskID string) {
	go s.runTask(taskID)
}

func (s *Service) runTask(taskID string) {
	ctx, cancel := context.WithTimeout(context.Background(), s.runTimeout)
	defer cancel()

	task, err := s.tasks.Get(ctx, taskID)
	if err != nil {
		logger.Error("icp: load task for run failed", zap.String("taskId", taskID), zap.Error(err))
		return
	}
	provider := s.providerFor(ctx, task.Provider)
	if available, reason := provider.Status(); !available {
		msg := fmt.Sprintf("provider %s 不可用：%s", provider.Name(), reason)
		s.appendLog(ctx, taskID, IcpLogLevelError, msg)
		_ = s.tasks.FinishRun(ctx, taskID, models.IcpTaskStatePaused, task.Stats, msg)
		return
	}

	stats := task.Stats
	cursor := task.Cursor
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(task.Queries) {
		cursor = len(task.Queries)
	}
	s.appendLog(ctx, taskID, IcpLogLevelInfo, fmt.Sprintf(
		"开始执行：provider=%s type=%s 共 %d 个查询（从第 %d 个开始）",
		provider.Name(), task.Type, len(task.Queries), cursor))

	for ; cursor < len(task.Queries); cursor++ {
		if stopped, state := s.taskState(ctx, taskID); stopped {
			s.appendLog(ctx, taskID, IcpLogLevelWarn, fmt.Sprintf(
				"收到停止信号（state=%d），停在 %d/%d", state, cursor, len(task.Queries)))
			_ = s.tasks.UpdateProgress(ctx, taskID, cursor, stats)
			return
		}

		query := task.Queries[cursor]
		records, err := s.queryWithRetry(ctx, taskID, provider, task.Type, query)
		if err != nil {
			stats.Failed++
			msg := fmt.Sprintf("查询 %q 失败：%v", query, err)
			s.appendLog(ctx, taskID, IcpLogLevelError, msg)
			_ = s.tasks.FinishRun(ctx, taskID, models.IcpTaskStatePaused, stats, msg)
			return
		}

		inserted, err := s.results.UpsertRecords(ctx, taskID, provider.Name(), records)
		if err != nil {
			stats.Failed++
			msg := fmt.Sprintf("查询 %q 入库失败：%v", query, err)
			s.appendLog(ctx, taskID, IcpLogLevelError, msg)
			_ = s.tasks.FinishRun(ctx, taskID, models.IcpTaskStatePaused, stats, msg)
			return
		}

		stats.Total += int64(len(records))
		stats.Done += int64(len(records))
		stats.Found += inserted
		if err := s.tasks.UpdateProgress(ctx, taskID, cursor+1, stats); err != nil {
			s.logWarn("icp: update progress failed", zap.String("taskId", taskID), zap.Error(err))
		}
		s.appendLog(ctx, taskID, IcpLogLevelInfo, fmt.Sprintf(
			"查询 %q：命中 %d 条，新增 %d 条", query, len(records), inserted))
	}

	msg := fmt.Sprintf("任务完成：total=%d done=%d found=%d failed=%d",
		stats.Total, stats.Done, stats.Found, stats.Failed)
	s.appendLog(ctx, taskID, IcpLogLevelInfo, msg)
	if err := s.tasks.FinishRun(ctx, taskID, models.IcpTaskStateDone, stats, ""); err != nil {
		s.logWarn("icp: finish task failed", zap.String("taskId", taskID), zap.Error(err))
	}
}

// taskState 返回任务是否已不在运行中，以及当前状态
func (s *Service) taskState(ctx context.Context, taskID string) (bool, int) {
	task, err := s.tasks.Get(ctx, taskID)
	if err != nil {
		// 读不到任务（被删除）时按停止处理，不再继续打 provider
		return true, models.IcpTaskStatePaused
	}
	return task.State != models.IcpTaskStateRunning, task.State
}

// queryWithRetry 调 provider，429/5xx 指数退避重试（maxRetries 次），其他错误直接失败
func (s *Service) queryWithRetry(ctx context.Context, taskID string, provider ICPProvider, taskType, query string) ([]models.ICPRecord, error) {
	attempts := s.maxRetries + 1
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			delay := s.backoff << (attempt - 1)
			if delay > maxRetryBackoff {
				delay = maxRetryBackoff
			}
			s.appendLog(ctx, taskID, IcpLogLevelWarn, fmt.Sprintf(
				"provider 返回可重试错误（429/5xx），%s 后第 %d/%d 次重试：%v",
				delay, attempt, s.maxRetries, lastErr))
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}

		var records []models.ICPRecord
		var err error
		if taskType == models.IcpTaskTypeCompany {
			records, err = provider.QueryCompany(ctx, query)
		} else {
			records, err = provider.QueryDomain(ctx, query)
		}
		if err == nil {
			return records, nil
		}
		lastErr = err
		if !isRetryableError(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

// providerFor 按任务记录（或当前配置）解析 provider；override 优先（集成测试注入 fake）
func (s *Service) providerFor(ctx context.Context, name string) ICPProvider {
	if s.override != nil {
		return s.override
	}
	if p := s.providers.Get(ctx, name); p != nil {
		return p
	}
	return s.providers.Resolve(ctx)
}

// appendLog 写任务日志（尽力而为：日志写失败不影响任务本身）
func (s *Service) appendLog(ctx context.Context, taskID, level, line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	if s.logs == nil {
		return
	}
	if err := s.logs.Append(ctx, taskID, level, line); err != nil {
		s.logWarn("icp: append task log failed", zap.String("taskId", taskID), zap.Error(err))
	}
}
