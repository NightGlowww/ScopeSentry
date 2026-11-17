package mcpguard

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	mcprepo "github.com/Autumn-27/ScopeSentry/internal/repositories/mcp"
	"go.uber.org/zap"
)

// configCacheTTL 全局配置缓存时长：MCP 请求路径上不每次读 Mongo；
// 管理端更新会主动 Invalidate，最多 5s 后其它实例生效。
const configCacheTTL = 5 * time.Second

// Service MCP 治理服务：全局配置读写 + 限速判定
type Service interface {
	Config(ctx context.Context) (*models.McpConfig, error)
	UpdateConfig(ctx context.Context, req *models.McpConfigUpdateRequest, operator string) (*models.McpConfig, error)
	InvalidateConfig()
	CheckRateLimit(keyID string, limit int) bool
	EffectiveRateLimit(record *models.ApiKey, cfg *models.McpConfig) int
	LogRetention(ctx context.Context) int64
}

type service struct {
	repo    mcprepo.ConfigRepository
	limiter *rateLimiter

	mu       sync.RWMutex
	cached   *models.McpConfig
	cachedAt time.Time
}

// NewService 创建服务
func NewService() Service {
	return &service{repo: mcprepo.NewConfigRepository(), limiter: newRateLimiter()}
}

// Config 读全局配置（带短 TTL 缓存）
func (s *service) Config(ctx context.Context) (*models.McpConfig, error) {
	s.mu.RLock()
	if s.cached != nil && time.Since(s.cachedAt) < configCacheTTL {
		cfg := *s.cached
		s.mu.RUnlock()
		return &cfg, nil
	}
	s.mu.RUnlock()

	cfg, err := s.repo.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	cfg.Normalize()

	s.mu.Lock()
	s.cached = cfg
	s.cachedAt = time.Now()
	s.mu.Unlock()

	copied := *cfg
	return &copied, nil
}

// UpdateConfig 更新全局开关；只改传进来的字段
func (s *service) UpdateConfig(ctx context.Context, req *models.McpConfigUpdateRequest, operator string) (*models.McpConfig, error) {
	if req == nil {
		return nil, fmt.Errorf("empty config request")
	}
	cfg, err := s.Config(ctx)
	if err != nil {
		return nil, err
	}
	if req.Enabled != nil {
		cfg.Enabled = *req.Enabled
	}
	if req.AllowedTools != nil {
		cfg.AllowedTools = models.NormalizeStringList(*req.AllowedTools)
	}
	if req.RateLimitPerMin != nil {
		if *req.RateLimitPerMin < 0 {
			return nil, fmt.Errorf("%w: rateLimitPerMin must be >= 0", ErrInvalidConfig)
		}
		cfg.RateLimitPerMin = *req.RateLimitPerMin
	}
	if req.LogRetention != nil {
		if *req.LogRetention < 0 {
			return nil, fmt.Errorf("%w: logRetention must be >= 0", ErrInvalidConfig)
		}
		cfg.LogRetention = *req.LogRetention
	}
	cfg.UpdatedBy = operator
	cfg.Normalize()
	if err := s.repo.SaveConfig(ctx, cfg); err != nil {
		return nil, err
	}
	s.InvalidateConfig()
	return cfg, nil
}

// InvalidateConfig 让缓存立即失效（本进程内）
func (s *service) InvalidateConfig() {
	s.mu.Lock()
	s.cached = nil
	s.cachedAt = time.Time{}
	s.mu.Unlock()
}

// EffectiveRateLimit key 级限速优先，未设置时跟随全局，两者皆为 0 表示不限速
func (s *service) EffectiveRateLimit(record *models.ApiKey, cfg *models.McpConfig) int {
	if record != nil && record.RateLimitPerMin > 0 {
		return record.RateLimitPerMin
	}
	if cfg != nil && cfg.RateLimitPerMin > 0 {
		return cfg.RateLimitPerMin
	}
	return 0
}

// CheckRateLimit 记一次调用并判断是否超限
func (s *service) CheckRateLimit(keyID string, limit int) bool {
	return s.limiter.allow(keyID, limit)
}

// LogRetention 日志保留条数；读配置失败时回落到默认值（janitor 不能因为配置抖动停摆）
func (s *service) LogRetention(ctx context.Context) int64 {
	cfg, err := s.Config(ctx)
	if err != nil {
		logger.Warn("mcp: read config for log retention failed, fallback to default", zap.Error(err))
		return models.McpDefaultLogRetention
	}
	if cfg.LogRetention <= 0 {
		return models.McpDefaultLogRetention
	}
	return cfg.LogRetention
}

var (
	defaultOnce    sync.Once
	defaultService Service
)

// Default 进程级单例
func Default() Service {
	defaultOnce.Do(func() { defaultService = NewService() })
	return defaultService
}
