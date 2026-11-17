package policy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	policyrepo "github.com/Autumn-27/ScopeSentry/internal/repositories/policy"
)

// 扫描策略业务层。
//
// 三条约束：
//   - create / update 走「归一化 + 范围校验 + 引用存在性校验」，失败返回 ValidationError（handler 转 400）；
//   - apply 只做纯合并（读策略 + 可选读模板基线），不写库、不改任务；
//   - test 复用同一套纯函数 + 引用校验，返回结构化 valid/errors/warnings，供前端实时提示。
//
// 引用检查不做：任务表没有 policyId 字段，本仓库暂时无法统计「哪些任务用了这条策略」，
// 删除策略不校验引用；detail 里用 usage 如实说明这一点。

var (
	// ErrInvalidInput 请求参数不合法（空 id / 空 ids / 非法 taskTemplate）
	ErrInvalidInput = errors.New("invalid scan policy request")
	// ErrInvalidConfig 策略参数校验失败（错误详情在 ValidationError.Issues）
	ErrInvalidConfig = errors.New("invalid scan policy config")
	// ErrTemplateNotFound apply 时传入的模板 ID 不存在
	ErrTemplateNotFound = errors.New("scan template not found")

	// ErrNotFound 策略不存在
	ErrNotFound = policyrepo.ErrNotFound
	// ErrInvalidID 非法 ObjectID
	ErrInvalidID = policyrepo.ErrInvalidID
	// ErrNameExists 策略名已存在
	ErrNameExists = policyrepo.ErrNameExists
)

// usageNote detail.usage.note 的固定说明文案
const usageNote = "任务表未保存 policyId，无法统计引用；删除策略不做引用检查（历史任务不受影响）"

// ValidationError 参数校验失败，携带结构化 issue 列表
type ValidationError struct {
	Issues []models.PolicyIssue
}

// Error 拼成一行可读信息（handler 放进 400 的 data）
func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Issues))
	for _, issue := range e.Issues {
		parts = append(parts, issue.Field+": "+issue.Message)
	}
	return "扫描策略参数校验失败: " + strings.Join(parts, "; ")
}

// Unwrap 让 errors.Is(err, ErrInvalidConfig) 成立
func (e *ValidationError) Unwrap() error { return ErrInvalidConfig }

// Option 服务构造选项（测试注入仓库 / 引用检查器 / 时钟）
type Option func(*Service)

// WithRepository 覆盖仓库实现
func WithRepository(r policyrepo.Repository) Option {
	return func(s *Service) {
		if r != nil {
			s.repo = r
		}
	}
}

// WithRefChecker 覆盖引用检查实现
func WithRefChecker(c RefChecker) Option {
	return func(s *Service) {
		if c != nil {
			s.refs = c
		}
	}
}

// Service 扫描策略服务
type Service struct {
	repo policyrepo.Repository
	refs RefChecker
}

// NewService 构造服务
func NewService(opts ...Option) *Service {
	repo := policyrepo.NewRepository()
	s := &Service{
		repo: repo,
		refs: repo,
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

// Create 新建策略（enabled 缺省 true）
func (s *Service) Create(ctx context.Context, req models.ScanPolicyCreateRequest) (*models.ScanPolicy, error) {
	name, err := normalizeName(req.Name)
	if err != nil {
		return nil, err
	}
	desc, err := normalizeDescription(req.Description)
	if err != nil {
		return nil, err
	}
	cfg, err := s.validate(ctx, req.Config)
	if err != nil {
		return nil, err
	}

	// 先查一次给友好错误；并发下由 name 唯一索引兜底（repo 会把 E11000 映射成 ErrNameExists）
	if _, err := s.repo.GetByName(ctx, name); err == nil {
		return nil, ErrNameExists
	} else if !errors.Is(err, policyrepo.ErrNotFound) {
		return nil, err
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	p := &models.ScanPolicy{
		Name:        name,
		Description: desc,
		Config:      cfg,
		Enabled:     enabled,
	}
	if err := s.repo.Create(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// Update 部分更新（name/description/config/enabled 传了才改；config 整体替换）
func (s *Service) Update(ctx context.Context, req models.ScanPolicyUpdateRequest) (*models.ScanPolicy, error) {
	id := req.IDValue()
	if id == "" {
		return nil, fmt.Errorf("%w: id 不能为空", ErrInvalidInput)
	}
	current, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	upd := policyrepo.Update{}
	if req.Name != nil {
		name, err := normalizeName(*req.Name)
		if err != nil {
			return nil, err
		}
		if name != current.Name {
			if _, err := s.repo.GetByName(ctx, name); err == nil {
				return nil, ErrNameExists
			} else if !errors.Is(err, policyrepo.ErrNotFound) {
				return nil, err
			}
			upd.Name = &name
		}
	}
	if req.Description != nil {
		desc, err := normalizeDescription(*req.Description)
		if err != nil {
			return nil, err
		}
		upd.Description = &desc
	}
	if req.Config != nil {
		cfg, err := s.validate(ctx, *req.Config)
		if err != nil {
			return nil, err
		}
		upd.Config = &cfg
	}
	if req.Enabled != nil {
		upd.Enabled = req.Enabled
	}
	return s.repo.Update(ctx, id, upd)
}

// Delete 批量删除（不校验引用，见 usageNote）
func (s *Service) Delete(ctx context.Context, ids []string) (int64, error) {
	clean := make([]string, 0, len(ids))
	for _, id := range ids {
		if v := strings.TrimSpace(id); v != "" {
			clean = append(clean, v)
		}
	}
	if len(clean) == 0 {
		return 0, fmt.Errorf("%w: ids 不能为空", ErrInvalidInput)
	}
	return s.repo.Delete(ctx, clean)
}

// Detail 策略详情 + 引用说明
func (s *Service) Detail(ctx context.Context, id string) (*models.ScanPolicyDetail, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("%w: id 不能为空", ErrInvalidInput)
	}
	p, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &models.ScanPolicyDetail{
		ScanPolicy: *p,
		Usage: models.PolicyUsage{
			Tracked:        false,
			ReferenceCount: 0,
			Note:           usageNote,
		},
	}, nil
}

// List 策略列表（pageSize 上限 200）
func (s *Service) List(ctx context.Context, q models.ScanPolicyListQuery) ([]models.ScanPolicy, int64, error) {
	if q.PageIndex <= 0 {
		q.PageIndex = 1
	}
	if q.PageSize <= 0 {
		q.PageSize = models.ScanPolicyDefaultPageSize
	}
	if q.PageSize > models.ScanPolicyMaxPageSize {
		return nil, 0, fmt.Errorf("%w: pageSize 不能超过 %d", ErrInvalidInput, models.ScanPolicyMaxPageSize)
	}
	q.Keyword = strings.TrimSpace(q.Keyword)
	return s.repo.List(ctx, q)
}

// Apply 预览合并结果：策略字段覆盖 taskTemplate 默认值，nil 不覆盖。
//
// taskTemplate 两种形态：
//   - 对象：直接当模板默认值（前端把当前表单带上来）；
//   - 字符串：扫描模板 ID，读模板分阶段插件清单生成 baseline（templateId + plugins）。
//
// 全程不写库；冲突只产 warning（不阻断），保证前端能“先看效果再落库”。
func (s *Service) Apply(ctx context.Context, policyID string, taskTemplate json.RawMessage) (*models.ScanPolicyApplyResult, error) {
	policyID = strings.TrimSpace(policyID)
	if policyID == "" {
		return nil, fmt.Errorf("%w: policyId 不能为空", ErrInvalidInput)
	}
	p, err := s.repo.Get(ctx, policyID)
	if err != nil {
		return nil, err
	}
	base, source, err := s.baseFromTaskTemplate(ctx, taskTemplate)
	if err != nil {
		return nil, err
	}
	merged := MergeConfig(p.Config, base)
	return &models.ScanPolicyApplyResult{
		PolicyID:           p.ID.Hex(),
		PolicyName:         p.Name,
		TaskTemplateSource: source,
		Config:             merged.Config,
		Warnings:           merged.Warnings,
		AppliedFields:      merged.AppliedFields,
	}, nil
}

// Test 校验一组扫描参数（test 端点）：纯规则 + 引用存在性，返回结构化结果。
func (s *Service) Test(ctx context.Context, cfg models.ScanPolicyConfig) (models.PolicyValidation, error) {
	result := ValidateConfigShape(cfg)
	if len(result.Errors) == 0 {
		refIssues, err := ValidateConfigRefs(ctx, result.Config, s.refs)
		if err != nil {
			return result, err
		}
		result.Errors = append(result.Errors, refIssues...)
	}
	result.Valid = len(result.Errors) == 0
	return result, nil
}

// validate create / update 共用：形状 + 引用，返回归一化后的 config
func (s *Service) validate(ctx context.Context, cfg models.ScanPolicyConfig) (models.ScanPolicyConfig, error) {
	shape := ValidateConfigShape(cfg)
	issues := shape.Errors
	if len(issues) == 0 {
		refIssues, err := ValidateConfigRefs(ctx, shape.Config, s.refs)
		if err != nil {
			return shape.Config, err
		}
		issues = append(issues, refIssues...)
	}
	if len(issues) > 0 {
		return shape.Config, &ValidationError{Issues: issues}
	}
	return shape.Config, nil
}

// baseFromTaskTemplate 解析 taskTemplate → baseline config + 来源标记
func (s *Service) baseFromTaskTemplate(ctx context.Context, raw json.RawMessage) (models.ScanPolicyConfig, string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return models.ScanPolicyConfig{}, "empty", nil
	}
	switch trimmed[0] {
	case '"':
		var templateID string
		if err := json.Unmarshal(trimmed, &templateID); err != nil {
			return models.ScanPolicyConfig{}, "", fmt.Errorf("%w: taskTemplate 解析失败: %v", ErrInvalidInput, err)
		}
		templateID = strings.TrimSpace(templateID)
		if templateID == "" {
			return models.ScanPolicyConfig{}, "empty", nil
		}
		if !objectIDPattern.MatchString(templateID) {
			return models.ScanPolicyConfig{}, "", &ValidationError{Issues: []models.PolicyIssue{{
				Field:   "taskTemplate",
				Code:    "template_invalid",
				Message: "模板 ID 必须是 24 位十六进制 ObjectID",
			}}}
		}
		phases, err := s.repo.LoadTemplatePhases(ctx, templateID)
		if err != nil {
			if errors.Is(err, policyrepo.ErrNotFound) || errors.Is(err, policyrepo.ErrInvalidID) {
				return models.ScanPolicyConfig{}, "", fmt.Errorf("%w: %s", ErrTemplateNotFound, templateID)
			}
			return models.ScanPolicyConfig{}, "", err
		}
		return BaselineFromTemplate(templateID, phases), "template", nil
	case '{':
		var cfg models.ScanPolicyConfig
		if err := json.Unmarshal(trimmed, &cfg); err != nil {
			return models.ScanPolicyConfig{}, "", fmt.Errorf("%w: taskTemplate 解析失败: %v", ErrInvalidInput, err)
		}
		return cfg, "object", nil
	default:
		return models.ScanPolicyConfig{}, "", fmt.Errorf("%w: taskTemplate 必须是对象或模板 ID 字符串", ErrInvalidInput)
	}
}

func normalizeName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", &ValidationError{Issues: []models.PolicyIssue{{
			Field:   "name",
			Code:    "name_required",
			Message: "策略名称不能为空",
		}}}
	}
	if utf8.RuneCountInString(name) > models.ScanPolicyMaxNameRunes {
		return "", &ValidationError{Issues: []models.PolicyIssue{{
			Field:   "name",
			Code:    "name_too_long",
			Message: fmt.Sprintf("策略名称不能超过 %d 个字符", models.ScanPolicyMaxNameRunes),
		}}}
	}
	return name, nil
}

func normalizeDescription(raw string) (string, error) {
	desc := strings.TrimSpace(raw)
	if utf8.RuneCountInString(desc) > models.ScanPolicyMaxDescriptionRunes {
		return "", &ValidationError{Issues: []models.PolicyIssue{{
			Field:   "description",
			Code:    "description_too_long",
			Message: fmt.Sprintf("描述不能超过 %d 个字符", models.ScanPolicyMaxDescriptionRunes),
		}}}
	}
	return desc, nil
}
