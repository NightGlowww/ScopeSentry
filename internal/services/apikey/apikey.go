package apikey

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/repositories/apikey"
	"github.com/Autumn-27/ScopeSentry/internal/services/mcpguard"
	"github.com/Autumn-27/ScopeSentry/internal/utils/random"
	mongodriver "go.mongodb.org/mongo-driver/mongo"
)

const keyPrefix = "ssk_"

var (
	ErrInvalidKey  = errors.New("invalid api key")
	ErrNotFound    = errors.New("api key not found")
	ErrKeyDisabled = errors.New("api key disabled")
	ErrKeyExpired  = errors.New("api key expired")
	ErrInvalid     = errors.New("invalid api key request")
)

type Service interface {
	Create(ctx context.Context, name, createdBy string) (*models.CreateApiKeyResponse, error)
	// CreateWithOptions 新建带治理字段的 key（明文 key 只在响应里返回一次）
	CreateWithOptions(ctx context.Context, req *models.McpKeyCreateRequest, createdBy string) (*models.CreateApiKeyResponse, error)
	List(ctx context.Context) ([]models.ApiKey, error)
	ListPage(ctx context.Context, keyword string, pageIndex, pageSize int) ([]models.ApiKey, int64, error)
	// Update 编辑治理字段（scope / tools / 限速 / 过期 / 名称）
	Update(ctx context.Context, id string, req *models.McpKeyUpdateRequest) (*models.ApiKey, error)
	// SetEnabled 启用/禁用
	SetEnabled(ctx context.Context, id string, enabled bool) error
	Delete(ctx context.Context, id string) error
	// Validate 校验 key 并返回记录；disabled/expired 也返回记录，便于审计落 keyId
	Validate(ctx context.Context, rawKey string) (*models.ApiKey, error)
	EnsureIndexes(ctx context.Context) error
}

type service struct {
	repo apikey.Repository
}

func NewService() Service {
	return &service{repo: apikey.NewRepository()}
}

// Create 历史入口（/api/apikey/create）：等价 scope=*、无工具白名单、不过期
func (s *service) Create(ctx context.Context, name, createdBy string) (*models.CreateApiKeyResponse, error) {
	return s.CreateWithOptions(ctx, &models.McpKeyCreateRequest{
		Name:  name,
		Scope: models.StringList{mcpguard.ScopeAll},
	}, createdBy)
}

// CreateWithOptions 新建 MCP Key
func (s *service) CreateWithOptions(ctx context.Context, req *models.McpKeyCreateRequest, createdBy string) (*models.CreateApiKeyResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: empty request", ErrInvalid)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalid)
	}
	scope := models.NormalizeStringList(req.Scope)
	if len(scope) == 0 {
		// 未指定 scope 的新 key 默认全权限（与历史 key 行为一致），显式收权由管理端传值
		scope = models.StringList{mcpguard.ScopeAll}
	}
	if err := validateScope(scope); err != nil {
		return nil, err
	}
	if req.RateLimitPerMin < 0 {
		return nil, fmt.Errorf("%w: rateLimitPerMin must be >= 0", ErrInvalid)
	}
	expiresAt, err := ParseExpiresAt(req.ExpiresAt)
	if err != nil {
		return nil, err
	}

	rawKey := keyPrefix + random.GenerateRandomString(40)
	displayPrefix := rawKey[:12] + "..."

	record := &models.ApiKey{
		Name:            name,
		KeyHash:         hashKey(rawKey),
		KeyPrefix:       displayPrefix,
		Enabled:         true,
		Scope:           scope,
		Tools:           models.NormalizeStringList(req.Tools),
		RateLimitPerMin: req.RateLimitPerMin,
		ExpiresAt:       expiresAt,
		CreatedBy:       createdBy,
	}
	if err := s.repo.Create(ctx, record); err != nil {
		return nil, err
	}

	return &models.CreateApiKeyResponse{
		ID:              record.ID.Hex(),
		Name:            record.Name,
		Key:             rawKey,
		KeyPrefix:       displayPrefix,
		Scope:           record.Scope,
		Tools:           record.Tools,
		RateLimitPerMin: record.RateLimitPerMin,
		ExpiresAt:       record.ExpiresAt,
		CreatedAt:       record.CreatedAt.Format(time.RFC3339),
	}, nil
}

func (s *service) List(ctx context.Context) ([]models.ApiKey, error) {
	return s.repo.FindAll(ctx)
}

func (s *service) ListPage(ctx context.Context, keyword string, pageIndex, pageSize int) ([]models.ApiKey, int64, error) {
	return s.repo.ListPage(ctx, strings.TrimSpace(keyword), pageIndex, pageSize)
}

// Update 编辑治理字段
func (s *service) Update(ctx context.Context, id string, req *models.McpKeyUpdateRequest) (*models.ApiKey, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: empty request", ErrInvalid)
	}
	existing, err := s.repo.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, mongodriver.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = existing.Name
	}
	scope := models.NormalizeStringList(req.Scope)
	if len(scope) == 0 {
		scope = models.StringList{mcpguard.ScopeAll}
	}
	if err := validateScope(scope); err != nil {
		return nil, err
	}
	if req.RateLimitPerMin < 0 {
		return nil, fmt.Errorf("%w: rateLimitPerMin must be >= 0", ErrInvalid)
	}
	expiresAt, err := ParseExpiresAt(req.ExpiresAt)
	if err != nil {
		return nil, err
	}
	if err := s.repo.UpdatePolicy(ctx, id, name, scope, models.NormalizeStringList(req.Tools), req.RateLimitPerMin, expiresAt); err != nil {
		return nil, err
	}
	return s.repo.FindByID(ctx, id)
}

func (s *service) SetEnabled(ctx context.Context, id string, enabled bool) error {
	if _, err := s.repo.FindByID(ctx, id); err != nil {
		if errors.Is(err, mongodriver.ErrNoDocuments) {
			return ErrNotFound
		}
		return err
	}
	return s.repo.SetEnabled(ctx, id, enabled)
}

func (s *service) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}

// Validate 校验 key。
//
// 语义变化（MCP 治理）：禁用/过期的 key 也把记录返回，让中间件能落审计并给出
// 明确拒绝原因；只有「不存在/格式不对」才返回 ErrInvalidKey（记录为 nil）。
func (s *service) Validate(ctx context.Context, rawKey string) (*models.ApiKey, error) {
	rawKey = strings.TrimSpace(rawKey)
	if rawKey == "" || !strings.HasPrefix(rawKey, keyPrefix) {
		return nil, ErrInvalidKey
	}

	record, err := s.repo.FindByHash(ctx, hashKey(rawKey))
	if err != nil {
		if errors.Is(err, mongodriver.ErrNoDocuments) {
			return nil, ErrInvalidKey
		}
		return nil, err
	}

	if record.Enabled {
		go func() {
			_ = s.repo.UpdateLastUsed(context.Background(), record.ID)
		}()
	}

	if !record.Enabled {
		return record, ErrKeyDisabled
	}
	if record.IsExpired(time.Now()) {
		return record, ErrKeyExpired
	}
	return record, nil
}

func (s *service) EnsureIndexes(ctx context.Context) error {
	return s.repo.EnsureIndexes(ctx)
}

// ParseExpiresAt 解析过期时间：空 = 长期有效；支持 RFC3339 与 "2006-01-02 15:04:05"
func ParseExpiresAt(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return &t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", raw, time.Local); err == nil {
		return &t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", raw, time.Local); err == nil {
		return &t, nil
	}
	return nil, fmt.Errorf("%w: expiresAt must be RFC3339 or 2006-01-02 15:04:05", ErrInvalid)
}

// validateScope 只做格式校验：允许 *、资源名、资源:动作、资源:*
func validateScope(scope []string) error {
	for _, v := range scope {
		if v == mcpguard.ScopeAll {
			continue
		}
		parts := strings.Split(v, ":")
		if len(parts) > 2 {
			return fmt.Errorf("%w: bad scope %q", ErrInvalid, v)
		}
		for _, p := range parts {
			if p == "" || p == "*" {
				continue
			}
			for _, r := range p {
				if !(r == '_' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
					return fmt.Errorf("%w: bad scope %q", ErrInvalid, v)
				}
			}
		}
	}
	return nil
}

func hashKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(sum[:])
}

// IsAPIKeyFormat 判断 token 是否为 API Key 格式（非 JWT）
func IsAPIKeyFormat(token string) bool {
	return strings.HasPrefix(token, keyPrefix)
}
