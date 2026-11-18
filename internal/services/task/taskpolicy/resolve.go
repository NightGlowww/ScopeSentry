package taskpolicy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/services/policy"
)

// 建任务 → 策略读库这一层。
//
// 只依赖 internal/services/policy 的只读入口（Detail），不碰 scan_policy 集合，
// 这样任务侧不需要知道策略文档形状；测试用 WithLoader 注入假实现，不必起 Mongo。

var (
	// ErrMissingPolicyID 任务上带了空的 policyId（不该发生，防御）
	ErrMissingPolicyID = errors.New("scan policy id is empty")
	// ErrPolicyNotFound 策略不存在（被删掉 / id 非法）
	ErrPolicyNotFound = errors.New("scan policy not found")
	// ErrPolicyDisabled 策略已停用：停用的策略不再参与建任务
	ErrPolicyDisabled = errors.New("scan policy disabled")
)

// Loader 读一条策略的最小接口（生产实现走 policy.Service.Detail）
type Loader interface {
	Load(ctx context.Context, id string) (*models.ScanPolicy, error)
}

// Option 构造选项
type Option func(*Service)

// WithLoader 覆盖策略读取实现（测试 / 未来缓存）
func WithLoader(loader Loader) Option {
	return func(s *Service) {
		if loader != nil {
			s.loader = loader
		}
	}
}

// Service 策略消费端
type Service struct {
	loader Loader
}

// NewService 构造消费端；默认从 policy 服务读库（仓库层在调用期解析 DB，构造不连库）
func NewService(opts ...Option) *Service {
	s := &Service{loader: serviceLoader{svc: policy.DefaultService()}}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Config 返回策略参数；策略停用时返回 ErrPolicyDisabled（调用方按「不应用策略」处理）。
func (s *Service) Config(ctx context.Context, id string) (models.ScanPolicyConfig, error) {
	p, err := s.Load(ctx, id)
	if err != nil {
		return models.ScanPolicyConfig{}, err
	}
	if !p.Enabled {
		return models.ScanPolicyConfig{}, fmt.Errorf("%w: %s", ErrPolicyDisabled, p.Name)
	}
	return p.Config, nil
}

// Load 读策略本体（空 id / 读不到都返回错误，绝不给一个空 config 冒充「策略存在」）。
func (s *Service) Load(ctx context.Context, id string) (*models.ScanPolicy, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrMissingPolicyID
	}
	if s == nil || s.loader == nil {
		return nil, errors.New("taskpolicy: loader not configured")
	}
	p, err := s.loader.Load(ctx, id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, ErrPolicyNotFound
	}
	return p, nil
}

// serviceLoader 把 policy.Service.Detail 适配成 Loader
type serviceLoader struct {
	svc *policy.Service
}

func (l serviceLoader) Load(ctx context.Context, id string) (*models.ScanPolicy, error) {
	if l.svc == nil {
		return nil, errors.New("taskpolicy: policy service not configured")
	}
	detail, err := l.svc.Detail(ctx, id)
	if err != nil {
		if errors.Is(err, policy.ErrNotFound) || errors.Is(err, policy.ErrInvalidID) {
			return nil, fmt.Errorf("%w: %s", ErrPolicyNotFound, id)
		}
		return nil, err
	}
	if detail == nil {
		return nil, fmt.Errorf("%w: %s", ErrPolicyNotFound, id)
	}
	p := detail.ScanPolicy
	return &p, nil
}
