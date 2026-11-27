package icp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	icprepo "github.com/Autumn-27/ScopeSentry/internal/repositories/icp"
	"go.uber.org/zap"
)

// ICP 查询任务业务层。
//
// 任务执行选型：**goroutine + 状态轮询**。
// restart / resume 只负责把任务抢占成 state=1 然后异步起 worker，HTTP 响应立刻返回；
// 调用方轮询 task/list 或 statistics 看 state 与 stats 推进。理由：
//   - provider 是外部网络调用，一个企业/域名可能要几秒到几十秒，同步执行会把 HTTP 连接挂住；
//   - 任务天然可暂停（stop 改状态，worker 在下一个查询前退出），异步模型才能表达「运行中 / 暂停 / 完成」；
//   - 并发互斥由 DB 的 FindOneAndUpdate(state != 1) 保证，进程内不需要额外锁表。
//
// 失败语义：provider 返回 429/5xx 时指数退避重试（默认 3 次），
// 重试耗尽后任务进入 state=2（暂停）并写 lastError，resume 从 cursor 继续。

// 任务日志级别（与仓库层常量同源，避免两处字面量漂移）
const (
	IcpLogLevelInfo  = icprepo.IcpLogLevelInfo
	IcpLogLevelWarn  = icprepo.IcpLogLevelWarn
	IcpLogLevelError = icprepo.IcpLogLevelError
)

// 仓库层错误别名（handler 只依赖 service 包即可完成 400/404 映射）
var (
	// ErrTaskNotFound 任务不存在
	ErrTaskNotFound = icprepo.ErrTaskNotFound
	// ErrInvalidID 非法 ObjectID
	ErrInvalidID = icprepo.ErrInvalidID
	// ErrTaskBusy 任务正在运行（互斥失败）
	ErrTaskBusy = icprepo.ErrTaskBusy
)

// 业务错误（handler 统一映射到 400/404）
var (
	// ErrInvalidInput 参数非法
	ErrInvalidInput = errors.New("invalid icp request")
	// ErrEmptyQueries 任务没有任何查询目标
	ErrEmptyQueries = errors.New("icp task queries is empty")
	// ErrInvalidType 任务类型非法
	ErrInvalidType = errors.New("icp task type must be company or domain")
	// ErrInvalidProvider provider 名称非法
	ErrInvalidProvider = errors.New("unknown icp provider")
	// ErrTaskNotPaused 只有暂停态可以 resume
	ErrTaskNotPaused = errors.New("icp task is not paused")
)

const (
	// DefaultMaxRetries provider 429/5xx 的最大重试次数
	DefaultMaxRetries = 3
	// DefaultRetryBackoff 指数退避基数（200ms → 400ms → 800ms）
	DefaultRetryBackoff = 200 * time.Millisecond
	// DefaultRunTimeout 单次任务运行的总超时
	DefaultRunTimeout = 30 * time.Minute
	// maxRetryBackoff 单次退避上限，避免指数退避把任务拖成小时级
	maxRetryBackoff = 5 * time.Second
	// maxTaskNameRunes 任务名长度上限（同步到 asset_scope 时复用为地契名）
	maxTaskNameRunes = 128
)

// Statistics 统计口径
type Statistics struct {
	TaskID  string `json:"taskId,omitempty"`
	Total   int64  `json:"total"`
	Done    int64  `json:"done"`
	Found   int64  `json:"found"`
	Failed  int64  `json:"failed"`
	Results int64  `json:"results"`
	Tasks   int64  `json:"tasks"`
	State   *int   `json:"state,omitempty"`
}

// ProviderStatusResponse tyc_check 响应
type ProviderStatusResponse struct {
	Provider   string           `json:"provider"`
	Available  bool             `json:"available"`
	Reason     string           `json:"reason"`
	Configured bool             `json:"configured"`
	Candidates []ProviderStatus `json:"candidates"`
}

// SyncSummary sync 端点响应
type SyncSummary struct {
	TaskID    string `json:"taskId"`
	ScopeID   string `json:"scopeId"`
	ScopeName string `json:"scopeName"`
	GroupID   string `json:"groupId"`
	Domains   int64  `json:"domains"`
	Added     int64  `json:"added"`
	Total     int64  `json:"total"`
}

// BackfillSummary backfill_details 响应
type BackfillSummary struct {
	TaskID   string            `json:"taskId"`
	Scanned  int64             `json:"scanned"`
	Updated  int64             `json:"updated"`
	Failed   int64             `json:"failed"`
	Failures []BackfillFailure `json:"failures"`
}

// BackfillFailure 单条补全失败记录（逐条记录，不中断整批）
type BackfillFailure struct {
	ResultID string `json:"resultId"`
	Domain   string `json:"domain"`
	Error    string `json:"error"`
}

// Option 服务构造选项
type Option func(*Service)

// WithProviderOverride 注入固定 provider（集成测试用 fake provider）
func WithProviderOverride(p ICPProvider) Option {
	return func(s *Service) { s.override = p }
}

// WithProviderRegistry 覆盖 provider 注册表
func WithProviderRegistry(r *ProviderRegistry) Option {
	return func(s *Service) { s.providers = r }
}

// WithRetry 覆盖重试策略（测试把退避调到毫秒级）
func WithRetry(maxRetries int, backoff time.Duration) Option {
	return func(s *Service) {
		if maxRetries < 0 {
			maxRetries = 0
		}
		s.maxRetries = maxRetries
		if backoff > 0 {
			s.backoff = backoff
		}
	}
}

// WithRunTimeout 覆盖单次运行超时
func WithRunTimeout(d time.Duration) Option {
	return func(s *Service) {
		if d > 0 {
			s.runTimeout = d
		}
	}
}

// WithScopeWriter 覆盖资产组写入实现
func WithScopeWriter(w ScopeWriter) Option {
	return func(s *Service) { s.scopes = w }
}

// WithClock 覆盖时间源（测试断言 nextRunAt 等）
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// Service ICP 任务服务
type Service struct {
	tasks   icprepo.TaskRepository
	results icprepo.ResultRepository
	logs    icprepo.LogRepository
	scopes  ScopeWriter

	providers *ProviderRegistry
	override  ICPProvider

	maxRetries int
	backoff    time.Duration
	runTimeout time.Duration
	now        func() time.Time
}

// NewService 构造服务
func NewService(opts ...Option) *Service {
	results := icprepo.NewResultRepository()
	s := &Service{
		tasks:      icprepo.NewTaskRepository(),
		results:    results,
		logs:       icprepo.NewLogRepository(),
		scopes:     NewAssetScopeWriter(),
		providers:  NewProviderRegistry(results),
		maxRetries: DefaultMaxRetries,
		backoff:    DefaultRetryBackoff,
		runTimeout: DefaultRunTimeout,
		now:        time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.providers == nil {
		s.providers = NewProviderRegistry(results)
	}
	if s.scopes == nil {
		s.scopes = NewAssetScopeWriter()
	}
	return s
}

var (
	defaultService *Service
	defaultOnce    sync.Once
)

// DefaultService 进程内单例（provider 配置每次调用现读，改配置无需重启）
func DefaultService() *Service {
	defaultOnce.Do(func() {
		defaultService = NewService()
	})
	return defaultService
}

// CreateTask 新建任务（不自动运行；create 之后用 restart 触发）
func (s *Service) CreateTask(ctx context.Context, req models.IcpTaskCreateRequest) (*models.IcpTask, error) {
	taskType := strings.ToLower(strings.TrimSpace(req.Type))
	if taskType == "" {
		taskType = models.IcpTaskTypeDomain
	}
	if taskType != models.IcpTaskTypeCompany && taskType != models.IcpTaskTypeDomain {
		return nil, fmt.Errorf("%w: %s", ErrInvalidType, req.Type)
	}

	raw := req.Queries
	if len(raw) == 0 && strings.TrimSpace(req.Target) != "" {
		raw = []string{req.Target}
	}
	queries := models.NormalizeIcpQueries(taskType, raw)
	if len(queries) == 0 {
		return nil, fmt.Errorf("%w: queries 或 target 至少填一个", ErrEmptyQueries)
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = queries[0]
	}
	if len([]rune(name)) > maxTaskNameRunes {
		return nil, fmt.Errorf("%w: name 不能超过 %d 个字符", ErrInvalidInput, maxTaskNameRunes)
	}

	providerName := strings.ToLower(strings.TrimSpace(req.Provider))
	if providerName != "" {
		if p := s.providers.Get(ctx, providerName); p == nil {
			return nil, fmt.Errorf("%w: %s", ErrInvalidProvider, req.Provider)
		}
	} else {
		// 没显式指定时按当前环境解析（override 优先：集成测试注入 fake provider）
		providerName = s.providerFor(ctx, "").Name()
	}

	task := &models.IcpTask{
		Name:     name,
		Type:     taskType,
		Queries:  queries,
		State:    models.IcpTaskStateWaiting,
		Provider: providerName,
	}
	if err := s.tasks.Create(ctx, task); err != nil {
		return nil, err
	}
	s.appendLog(ctx, task.ID.Hex(), IcpLogLevelInfo, fmt.Sprintf(
		"任务已创建：type=%s provider=%s 共 %d 个查询", task.Type, task.Provider, len(task.Queries)))

	// 新建即开跑：此前只有 Restart/Resume 会 launch，创出来的任务永远停在 waiting(0)
	if _, err := s.tasks.Claim(ctx, task.ID.Hex(), false); err != nil {
		s.appendLog(ctx, task.ID.Hex(), IcpLogLevelError, "任务启动失败："+err.Error())
		return task, nil
	}
	s.launch(task.ID.Hex())
	return s.tasks.Get(ctx, task.ID.Hex())
}

// GetTask 取任务
func (s *Service) GetTask(ctx context.Context, id string) (*models.IcpTask, error) {
	return s.tasks.Get(ctx, id)
}

// ListTasks 任务列表
func (s *Service) ListTasks(ctx context.Context, q models.IcpTaskListQuery) ([]models.IcpTask, int64, error) {
	return s.tasks.List(ctx, q)
}

// Restart 重新运行任务（清零统计与 cursor，后台 goroutine 执行）
func (s *Service) Restart(ctx context.Context, id string) (*models.IcpTask, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("%w: taskId 不能为空", ErrInvalidInput)
	}
	before, err := s.tasks.Claim(ctx, id, true)
	if err != nil {
		return nil, err
	}
	s.appendLog(ctx, id, IcpLogLevelInfo, fmt.Sprintf(
		"任务重启：provider=%s type=%s 共 %d 个查询", before.Provider, before.Type, len(before.Queries)))
	s.launch(id)
	return s.tasks.Get(ctx, id)
}

// Resume 从暂停处继续（保留 stats/cursor）
func (s *Service) Resume(ctx context.Context, id string) (*models.IcpTask, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("%w: taskId 不能为空", ErrInvalidInput)
	}
	task, err := s.tasks.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if task.State != models.IcpTaskStatePaused {
		return nil, fmt.Errorf("%w: 当前 state=%d", ErrTaskNotPaused, task.State)
	}
	if _, err := s.tasks.Claim(ctx, id, false); err != nil {
		return nil, err
	}
	s.appendLog(ctx, id, IcpLogLevelInfo, fmt.Sprintf(
		"任务恢复：从第 %d 个查询继续（已处理 %d/%d）", task.Cursor, task.Stats.Done, task.Stats.Total))
	s.launch(id)
	return s.tasks.Get(ctx, id)
}

// Stop 暂停任务；已完成的任务不覆写状态
//
// 日志只给真实存在且处于可暂停状态的任务写：早期实现对传入的每个 id 无条件 appendLog，
// 于是对不存在的 id 调 stop 会在 icp_task_log 里留下一条幻影「收到停止请求」，
// 再 delete 同一个 id 就会返回 deletedLogs=1 —— 任务从来没存在过，日志却说删掉了一条。
func (s *Service) Stop(ctx context.Context, ids []string) (int64, error) {
	if len(ids) == 0 {
		return 0, fmt.Errorf("%w: ids 不能为空", ErrInvalidInput)
	}
	existing, err := s.tasks.GetMany(ctx, ids)
	if err != nil {
		return 0, err
	}
	if len(existing) == 0 {
		return 0, fmt.Errorf("%w: %s", ErrTaskNotFound, strings.Join(ids, ","))
	}
	stoppable := make([]string, 0, len(existing))
	for i := range existing {
		switch existing[i].State {
		case models.IcpTaskStateWaiting, models.IcpTaskStateRunning, models.IcpTaskStatePaused:
			stoppable = append(stoppable, existing[i].ID.Hex())
		}
	}
	if len(stoppable) == 0 {
		return 0, nil
	}
	updated, err := s.tasks.SetState(ctx, stoppable, models.IcpTaskStatePaused, []int{
		models.IcpTaskStateWaiting, models.IcpTaskStateRunning, models.IcpTaskStatePaused,
	})
	if err != nil {
		return 0, err
	}
	for _, id := range stoppable {
		s.appendLog(ctx, id, IcpLogLevelWarn, "收到停止请求：任务进入暂停（可 resume 继续）")
	}
	return updated, nil
}

// Delete 删除任务及其结果、日志
func (s *Service) Delete(ctx context.Context, ids []string) (int64, int64, int64, error) {
	if len(ids) == 0 {
		return 0, 0, 0, fmt.Errorf("%w: ids 不能为空", ErrInvalidInput)
	}
	tasks, err := s.tasks.Delete(ctx, ids)
	if err != nil {
		return 0, 0, 0, err
	}
	results, err := s.results.DeleteByTaskIDs(ctx, ids)
	if err != nil {
		return tasks, results, 0, err
	}
	logs, err := s.logs.DeleteByTaskIDs(ctx, ids)
	if err != nil {
		return tasks, results, logs, err
	}
	// 一条都没删掉说明这些 id 根本不存在：返回 404，而不是 200 + 全零让前端弹「已删除」
	if tasks == 0 && results == 0 && logs == 0 {
		return 0, 0, 0, fmt.Errorf("%w: %s", ErrTaskNotFound, strings.Join(ids, ","))
	}
	return tasks, results, logs, nil
}

// Logs 任务最近 N 行日志（默认/上限 200）
func (s *Service) Logs(ctx context.Context, taskID string, limit int) ([]models.IcpTaskLog, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("%w: taskId 不能为空", ErrInvalidInput)
	}
	if _, err := s.tasks.Get(ctx, taskID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > models.IcpMaxLogLines {
		limit = models.IcpMaxLogLines
	}
	return s.logs.Recent(ctx, taskID, limit)
}

// Statistics 统计：给了 taskId 就按任务，否则全局汇总
func (s *Service) Statistics(ctx context.Context, taskID string) (Statistics, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID != "" {
		task, err := s.tasks.Get(ctx, taskID)
		if err != nil {
			return Statistics{}, err
		}
		results, err := s.results.CountByTask(ctx, taskID)
		if err != nil {
			return Statistics{}, err
		}
		state := task.State
		return Statistics{
			TaskID:  taskID,
			Total:   task.Stats.Total,
			Done:    task.Stats.Done,
			Found:   task.Stats.Found,
			Failed:  task.Stats.Failed,
			Results: results,
			Tasks:   1,
			State:   &state,
		}, nil
	}

	stats, err := s.tasks.SumStats(ctx)
	if err != nil {
		return Statistics{}, err
	}
	tasks, err := s.tasks.Count(ctx)
	if err != nil {
		return Statistics{}, err
	}
	results, err := s.results.Count(ctx)
	if err != nil {
		return Statistics{}, err
	}
	return Statistics{
		Total:   stats.Total,
		Done:    stats.Done,
		Found:   stats.Found,
		Failed:  stats.Failed,
		Results: results,
		Tasks:   tasks,
	}, nil
}

// Overview 各状态任务数
func (s *Service) Overview(ctx context.Context) (map[string]int64, error) {
	return s.tasks.OverviewCounts(ctx)
}

// ProviderStatus 当前 provider 状态（tyc_check）
func (s *Service) ProviderStatus(ctx context.Context) ProviderStatusResponse {
	provider := s.providerFor(ctx, "")
	available, reason := provider.Status()
	cfg := s.providers.Config(ctx)
	return ProviderStatusResponse{
		Provider:   provider.Name(),
		Available:  available,
		Reason:     reason,
		Configured: strings.TrimSpace(cfg.BaseURL) != "",
		Candidates: s.providers.Statuses(ctx),
	}
}

// ListResults 任务结果分页
func (s *Service) ListResults(ctx context.Context, q models.IcpAssetQuery) ([]models.IcpResult, int64, error) {
	taskID := strings.TrimSpace(q.TaskID)
	if taskID == "" {
		return nil, 0, fmt.Errorf("%w: taskId 不能为空", ErrInvalidInput)
	}
	if _, err := s.tasks.Get(ctx, taskID); err != nil {
		return nil, 0, err
	}
	return s.results.List(ctx, q)
}

// ResultsForTask 结果条数（导出/统计用）
func (s *Service) ResultsForTask(ctx context.Context, taskID string) (int64, error) {
	return s.results.CountByTask(ctx, taskID)
}

// logWarn 记录内部错误（进程日志），不向用户抛
func (s *Service) logWarn(msg string, fields ...zap.Field) {
	logger.Warn(msg, fields...)
}
