package fofa

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	fofarepo "github.com/Autumn-27/ScopeSentry/internal/repositories/fofa"
	"go.uber.org/zap"
)

// FOFA 批量建任务业务层（对标 ARL-Next backend/app/routes/taskFofa.py）。
//
// 流程：provider 分页拉 host → 归一/去重 → 与「同 query 最近一次导入」比对跳过
// → 复用 commonTask.Service.Insert 建任务（per-host 或 single）→ 写 fofa_import。
//
// 失败语义：单个 host/任务失败不影响整批，逐条写进 errors[]；
// 只有参数非法 / provider 不可用 / 查询无结果才整体失败（handler 映射 400）。

// 业务错误（handler 统一映射到 400）
var (
	// ErrInvalidInput 参数非法
	ErrInvalidInput = errors.New("invalid fofa request")
	// ErrQueryRequired 缺少 query
	ErrQueryRequired = errors.New("fofa query is required")
	// ErrPreviewSizeExceeded search 预览超过单页上限（100）
	ErrPreviewSizeExceeded = errors.New("fofa preview size exceeded")
	// ErrImportSizeExceeded 导入超过 host 上限（1000）
	ErrImportSizeExceeded = errors.New("fofa import size exceeded")
	// ErrInvalidCreateMode createMode 不是 per-host / single
	ErrInvalidCreateMode = errors.New("invalid fofa create mode")
	// ErrTooManyTasks per-host 待建任务数超过 50
	ErrTooManyTasks = errors.New("too many tasks to create")
	// ErrNoHosts 查询没有返回任何 host
	ErrNoHosts = errors.New("fofa query returned no host")
)

// ProviderStatusResponse status 端点响应
type ProviderStatusResponse struct {
	Provider   string              `json:"provider"`
	Available  bool                `json:"available"`
	Reason     string              `json:"reason"`
	Configured bool                `json:"configured"`
	KeySource  string              `json:"keySource,omitempty"`
	BaseURL    string              `json:"baseUrl,omitempty"`
	Balance    *models.FofaBalance `json:"balance,omitempty"`
	// Candidates 所有内置 provider 的状态（none / http），便于前端提示怎么配置
	Candidates []ProviderStatus `json:"candidates"`
}

// Option 服务构造选项
type Option func(*Service)

// WithProviderOverride 注入固定 provider（集成测试用 fake provider）
func WithProviderOverride(p FofaProvider) Option {
	return func(s *Service) { s.override = p }
}

// WithProviderRegistry 覆盖 provider 注册表
func WithProviderRegistry(r *ProviderRegistry) Option {
	return func(s *Service) { s.providers = r }
}

// WithTaskCreator 覆盖任务创建实现
func WithTaskCreator(c TaskCreator) Option {
	return func(s *Service) { s.creator = c }
}

// WithImportRepository 覆盖导入记录仓库
func WithImportRepository(r fofarepo.ImportRepository) Option {
	return func(s *Service) { s.imports = r }
}

// WithClock 覆盖时间源（导入记录 createdAt 断言用）
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// Service FOFA 导入服务
type Service struct {
	imports   fofarepo.ImportRepository
	providers *ProviderRegistry
	override  FofaProvider
	creator   TaskCreator
	now       func() time.Time
}

// NewService 构造服务
func NewService(opts ...Option) *Service {
	s := &Service{
		imports:   fofarepo.NewImportRepository(),
		providers: NewProviderRegistry(),
		creator:   NewTaskCreator(),
		now:       time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.providers == nil {
		s.providers = NewProviderRegistry()
	}
	if s.creator == nil {
		s.creator = NewTaskCreator()
	}
	if s.imports == nil {
		s.imports = fofarepo.NewImportRepository()
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

// ProviderStatus 当前 provider 状态（available / reason / 余额）
func (s *Service) ProviderStatus(ctx context.Context) ProviderStatusResponse {
	provider := s.providerFor(ctx, "")
	status := provider.Status(ctx)
	return ProviderStatusResponse{
		Provider:   status.Provider,
		Available:  status.Available,
		Reason:     status.Reason,
		Configured: status.Configured,
		KeySource:  status.KeySource,
		BaseURL:    status.BaseURL,
		Balance:    status.Balance,
		Candidates: s.providers.Statuses(ctx),
	}
}

// Search 预览查询（单页，size ≤ 100）
func (s *Service) Search(ctx context.Context, req models.FofaSearchRequest) (*SearchPage, error) {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, fmt.Errorf("%w: query 不能为空", ErrQueryRequired)
	}
	page := req.Page
	if page <= 0 {
		page = 1
	}
	size := req.Size
	if size <= 0 {
		size = models.FofaDefaultPreviewSize
	}
	if size > models.FofaMaxPreviewSize {
		return nil, fmt.Errorf("%w: size 不能超过 %d", ErrPreviewSizeExceeded, models.FofaMaxPreviewSize)
	}

	provider := s.providerFor(ctx, "")
	result, err := provider.Search(ctx, query, page, size)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return &SearchPage{Query: query, Page: page}, nil
	}
	// provider 自行归一过一次，这里再兜一层（fake provider / 未来实现可能不归一）
	hosts, _ := models.NormalizeFofaHosts(result.Hosts)
	result.Hosts = hosts
	result.Provider = provider.Name()
	result.Query = query
	result.Page = page
	result.Size = len(hosts)
	return result, nil
}

// FetchHosts 拉取最多 size 个 host（按 100/页翻页），归一 + 去重保序。
func (s *Service) FetchHosts(ctx context.Context, query string, size int) ([]string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("%w: query 不能为空", ErrQueryRequired)
	}
	if size <= 0 {
		size = models.FofaDefaultImportSize
	}
	if size > models.FofaMaxImportSize {
		return nil, fmt.Errorf("%w: size 不能超过 %d", ErrImportSizeExceeded, models.FofaMaxImportSize)
	}

	provider := s.providerFor(ctx, "")
	out := make([]string, 0, size)
	seen := make(map[string]struct{}, size)
	// 页码上限：按最小页大小估算，多给 2 页容错，防止 provider 异常时死循环
	maxPages := size/models.FofaHostPageSize + 2

	for page := 1; page <= maxPages && len(out) < size; page++ {
		// 每页固定 100 条：FOFA 的偏移是 (page-1)*size，最后一页改成 50 会和上一页重叠，
		// 拿到多余 host 后截断即可（多取的最多 99 条）。
		want := models.FofaHostPageSize
		result, err := provider.Search(ctx, query, page, want)
		if err != nil {
			return nil, err
		}
		if result == nil || len(result.Hosts) == 0 {
			break
		}
		for _, raw := range result.Hosts {
			host := models.NormalizeFofaHost(raw)
			if host == "" {
				continue
			}
			if _, ok := seen[host]; ok {
				continue
			}
			seen[host] = struct{}{}
			out = append(out, host)
			if len(out) >= size {
				break
			}
		}
		if len(result.Hosts) < want {
			// 最后一页：provider 已经没更多了
			break
		}
	}
	return out, nil
}

// CreateTasks 从 FOFA 查询批量建任务。
//
// per-host：待建（跳过已导入的）每个 host 一个任务，最多 50 个；
// single：一个任务包含全部待建 host（最多 1000 个目标）。
// 去重：同 query 最近一次 fofa_import 里出现过的 host 计入 skipped。
func (s *Service) CreateTasks(ctx context.Context, req models.FofaTaskRequest) (*models.FofaImportResult, error) {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, fmt.Errorf("%w: query 不能为空", ErrQueryRequired)
	}
	mode := strings.ToLower(strings.TrimSpace(req.CreateMode))
	if mode != models.FofaCreateModePerHost && mode != models.FofaCreateModeSingle {
		return nil, fmt.Errorf("%w: createMode 必须是 %s 或 %s", ErrInvalidCreateMode,
			models.FofaCreateModePerHost, models.FofaCreateModeSingle)
	}
	size := req.Size
	if size <= 0 {
		size = models.FofaDefaultImportSize
	}
	if size > models.FofaMaxImportSize {
		return nil, fmt.Errorf("%w: size 不能超过 %d", ErrImportSizeExceeded, models.FofaMaxImportSize)
	}
	baseName := sanitizeTaskName(req.TaskName)
	if baseName == "" {
		return nil, fmt.Errorf("%w: taskName 不能为空", ErrInvalidInput)
	}

	hosts, err := s.FetchHosts(ctx, query, size)
	if err != nil {
		return nil, err
	}
	if len(hosts) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoHosts, query)
	}

	// 去重基准：同 query 最近一次导入记录里的 host
	latest, err := s.imports.LatestByQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	imported := make(map[string]struct{})
	if latest != nil {
		for _, host := range latest.Hosts {
			imported[host] = struct{}{}
		}
	}

	pending := make([]string, 0, len(hosts))
	skipped := 0
	for _, host := range hosts {
		if _, ok := imported[host]; ok {
			skipped++
			continue
		}
		pending = append(pending, host)
	}
	if mode == models.FofaCreateModePerHost && len(pending) > models.FofaMaxTasksPerImport {
		return nil, fmt.Errorf("%w: per-host 单次最多建 %d 个任务，本次待建 %d 个（可改用 single 模式）",
			ErrTooManyTasks, models.FofaMaxTasksPerImport, len(pending))
	}

	result := &models.FofaImportResult{
		Query:      query,
		CreateMode: mode,
		Size:       len(hosts),
		Skipped:    skipped,
		Hosts:      hosts,
		TaskIDs:    make([]string, 0, len(pending)),
		TaskNames:  make([]string, 0, len(pending)),
		Errors:     make([]models.FofaImportError, 0),
	}
	// succeeded 只装「真的建出任务」的 host：失败的不进下一次导入的去重基准，
	// 否则同名冲突之类的临时失败会让这个 host 永远被跳过、再也重试不了。
	succeeded := make([]string, 0, len(pending))

	if mode == models.FofaCreateModeSingle {
		if len(pending) > 0 {
			name := baseName
			taskID, createErr := s.creator.Create(ctx, CreateTaskSpec{
				Name:       name,
				Targets:    pending,
				Project:    req.Project,
				TemplateID: req.TemplateID,
			})
			if createErr != nil {
				result.Failed++
				s.appendFailure(result, pendingLabel(pending), createErr)
			} else {
				result.Created++
				result.TaskIDs = append(result.TaskIDs, taskID)
				result.TaskNames = append(result.TaskNames, name)
				succeeded = append(succeeded, pending...)
			}
		}
	} else {
		for _, host := range pending {
			name := perHostTaskName(baseName, host)
			taskID, createErr := s.creator.Create(ctx, CreateTaskSpec{
				Name:       name,
				Targets:    []string{host},
				Project:    req.Project,
				TemplateID: req.TemplateID,
			})
			if createErr != nil {
				result.Failed++
				s.appendFailure(result, host, createErr)
				continue
			}
			result.Created++
			result.TaskIDs = append(result.TaskIDs, taskID)
			result.TaskNames = append(result.TaskNames, name)
			succeeded = append(succeeded, host)
		}
	}

	// 导入记录的 hosts 是「这个 query 累计已建过任务的 host」：
	// 本次成功的 + 上一次记录里的（跳过的那些），保证多轮导入仍按最近一次记录去重。
	recordHosts := mergeUniqueHosts(succeeded, importedHosts(latest))
	record := &models.FofaImport{
		Query:      query,
		Hosts:      recordHosts,
		TaskIDs:    result.TaskIDs,
		TaskNames:  result.TaskNames,
		CreateMode: mode,
		Size:       len(recordHosts),
		Project:    strings.TrimSpace(req.Project),
		TemplateID: strings.TrimSpace(req.TemplateID),
		Created:    result.Created,
		Skipped:    result.Skipped,
		Failed:     result.Failed,
		Errors:     result.Errors,
		CreatedAt:  s.now(),
	}
	if err := s.imports.Create(ctx, record); err != nil {
		// 任务已经建出来了，导入记录写失败不能把结果吞掉：
		// 记日志 + 返回结果（importId 为空），下一次导入会因为缺记录而重新建同名任务并逐条失败，不会静默重复。
		logger.Warn("fofa: persist import record failed",
			zap.String("query", query), zap.Error(err))
	} else {
		result.ImportID = record.ID.Hex()
	}
	return result, nil
}

// ListImports 导入历史（createdAt 倒序分页）
func (s *Service) ListImports(ctx context.Context, q models.FofaImportListQuery) ([]models.FofaImport, int64, error) {
	if q.PageSize > models.FofaMaxPageSize {
		return nil, 0, fmt.Errorf("%w: pageSize 不能超过 %d", ErrInvalidInput, models.FofaMaxPageSize)
	}
	return s.imports.List(ctx, q)
}

// providerFor 选 provider：override（测试）> 显式名称 > 当前配置解析
func (s *Service) providerFor(ctx context.Context, name string) FofaProvider {
	if s.override != nil {
		return s.override
	}
	if strings.TrimSpace(name) != "" {
		if p := s.providers.Get(ctx, name); p != nil {
			return p
		}
	}
	return s.providers.Resolve(ctx)
}

// appendFailure 追加一条失败记录（超过上限只计数不追加）
func (s *Service) appendFailure(result *models.FofaImportResult, host string, err error) {
	if result == nil || err == nil {
		return
	}
	if len(result.Errors) >= models.FofaMaxErrors {
		return
	}
	result.Errors = append(result.Errors, models.FofaImportError{Host: host, Error: err.Error()})
}

// pendingLabel single 模式失败时的 host 摘要（不把上千个 host 塞进一条错误）
func pendingLabel(hosts []string) string {
	if len(hosts) == 1 {
		return hosts[0]
	}
	return fmt.Sprintf("%d hosts", len(hosts))
}

// sanitizeTaskName 去首尾空白、去掉会破坏任务名的换行
func sanitizeTaskName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\n", "-")
	name = strings.ReplaceAll(name, "\r", "-")
	return capTaskName(name)
}

// importedHosts 上一次导入记录里的 host（nil 安全）
func importedHosts(record *models.FofaImport) []string {
	if record == nil {
		return nil
	}
	return record.Hosts
}

// mergeUniqueHosts 多组 host 合并去重保序（前一组优先）
func mergeUniqueHosts(groups ...[]string) []string {
	total := 0
	for _, group := range groups {
		total += len(group)
	}
	out := make([]string, 0, total)
	seen := make(map[string]struct{}, total)
	for _, group := range groups {
		for _, host := range group {
			host = strings.TrimSpace(host)
			if host == "" {
				continue
			}
			if _, ok := seen[host]; ok {
				continue
			}
			seen[host] = struct{}{}
			out = append(out, host)
		}
	}
	return out
}

// perHostTaskName 拼 per-host 任务名：<taskName>-<host>。
// 名字超长时先截 taskName 而不是截 host —— 截掉 host 会让多个任务同名，
// 后建的会以「任务名已存在」逐条失败。
func perHostTaskName(base, host string) string {
	suffix := "-" + host
	baseRunes := []rune(base)
	if maxBase := models.FofaMaxTaskNameRunes - len([]rune(suffix)); maxBase >= 1 && len(baseRunes) > maxBase {
		baseRunes = baseRunes[:maxBase]
	}
	return capTaskName(string(baseRunes) + suffix)
}

// capTaskName 截断到任务名长度上限（runes）
func capTaskName(name string) string {
	name = strings.TrimSpace(name)
	runes := []rune(name)
	if len(runes) <= models.FofaMaxTaskNameRunes {
		return name
	}
	return string(runes[:models.FofaMaxTaskNameRunes])
}
