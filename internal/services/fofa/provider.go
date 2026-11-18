package fofa

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
)

// FOFA provider 抽象。
//
// 契约只有两个方法：
//   - Status：是否可用 + 不可用原因 + 余额（provider 能拿到才返回）；
//   - Search：按 query 分页取一页 host（page 从 1 开始，size 单页上限 100）。
//
// 拉取多少页、host 归一去重、任务怎么建、导入记录怎么写全部在 service 层完成 ——
// provider 只负责「给我第 N 页」。
// 两个内置实现：
//   - http：FOFA 官方 API（fofa.key / fofa.base_url，base64 query）；
//   - none：未配置 / 显式关闭，调用时返回明确原因（让「没有可用数据源」显式可观测）。
//
// 测试用 httptest 假服务覆盖 http provider，不需要真实 key。

// ErrProviderUnavailable provider 不可用（缺 key / 显式 none）
var ErrProviderUnavailable = errors.New("fofa provider unavailable")

// ProviderError 带状态码的 provider 错误。
// 业务错误（FOFA 返回 {"error":true,"errmsg":...}）按 400 处理，HTTP 层的 401/429/5xx 保留原码。
type ProviderError struct {
	StatusCode int
	Message    string
}

func (e *ProviderError) Error() string {
	if strings.TrimSpace(e.Message) == "" {
		return fmt.Sprintf("fofa provider http %d", e.StatusCode)
	}
	return fmt.Sprintf("fofa provider http %d: %s", e.StatusCode, e.Message)
}

// SearchPage 一页搜索结果（host 已归一，service 直接可用）
type SearchPage struct {
	Provider       string   `json:"provider"`
	Query          string   `json:"query"`
	Page           int      `json:"page"`
	Size           int      `json:"size"`
	Total          int64    `json:"total"`
	Hosts          []string `json:"hosts"`
	ConsumedFPoint float64  `json:"consumedFPoint,omitempty"`
}

// ProviderStatus provider 状态（/api/fofa/status 返回）
type ProviderStatus struct {
	Provider   string              `json:"provider"`
	Available  bool                `json:"available"`
	Reason     string              `json:"reason"`
	Configured bool                `json:"configured"`
	KeySource  string              `json:"keySource,omitempty"`
	BaseURL    string              `json:"baseUrl,omitempty"`
	Balance    *models.FofaBalance `json:"balance,omitempty"`
}

// FofaProvider FOFA 数据来源契约
type FofaProvider interface {
	// Name provider 名称（落响应体与日志）
	Name() string
	// Status 是否可用 + 原因 + 余额
	Status(ctx context.Context) ProviderStatus
	// Search 取第 page 页（从 1 开始），单页最多 size 条
	Search(ctx context.Context, query string, page, size int) (*SearchPage, error)
}

// ProviderConfig provider 配置（系统配置集合 config + 环境变量）
type ProviderConfig struct {
	// Key fofa.key（环境变量 FOFA_KEY 兜底）
	Key string
	// KeySource key 的来源，便于前端/排障说明（config:fofa.key / env:FOFA_KEY / none）
	KeySource string
	// BaseURL fofa.base_url（环境变量 FOFA_BASE_URL 兜底，默认 https://fofa.info）
	BaseURL string
	// BaseURLSource base_url 的来源
	BaseURLSource string
}

// 系统配置键名 / 环境变量 / 默认值（与任务契约一致：FOFA_BASE_URL 默认 https://fofa.info）
const (
	// FofaConfigKey 系统配置里的 FOFA API Key 键名
	FofaConfigKey = "fofa.key"
	// FofaConfigBaseURL 系统配置里的 API 基址键名（测试/私有部署用）
	FofaConfigBaseURL = "fofa.base_url"
	// EnvKey 环境变量兜底 key
	EnvKey = "FOFA_KEY"
	// EnvBaseURL 环境变量覆盖 API 基址
	EnvBaseURL = "FOFA_BASE_URL"
	// DefaultBaseURL FOFA 官方地址
	DefaultBaseURL = "https://fofa.info"
	// DefaultProviderTimeout 单次 provider 请求超时
	DefaultProviderTimeout = 20 * time.Second
)

// ProviderRegistry provider 注册表：按当前配置解析具体实现
type ProviderRegistry struct {
	httpClient *http.Client
	config     func(context.Context) ProviderConfig
}

// NewProviderRegistry 创建注册表
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		httpClient: &http.Client{Timeout: DefaultProviderTimeout},
		config:     LoadProviderConfig,
	}
}

// NewProviderRegistryWithConfig 用自定义配置源构造注册表（测试注入固定 key/base_url）
func NewProviderRegistryWithConfig(config func(context.Context) ProviderConfig) *ProviderRegistry {
	r := NewProviderRegistry()
	if config != nil {
		r.config = config
	}
	return r
}

// Config 读取当前配置（每次调用都重新读，改配置后无需重启）
func (r *ProviderRegistry) Config(ctx context.Context) ProviderConfig {
	if r.config != nil {
		return r.config(ctx)
	}
	return ProviderConfig{BaseURL: DefaultBaseURL}
}

// Resolve 解析当前应使用的 provider：配了 key → http；没配 → none（带明确原因）
func (r *ProviderRegistry) Resolve(ctx context.Context) FofaProvider {
	cfg := r.Config(ctx)
	if strings.TrimSpace(cfg.Key) != "" {
		return newHTTPProvider(cfg, r.httpClient)
	}
	return newNoneProvider(noneReason(cfg))
}

// Get 按名称取 provider；未知名称返回 nil（调用方回落 Resolve）
func (r *ProviderRegistry) Get(ctx context.Context, name string) FofaProvider {
	cfg := r.Config(ctx)
	switch strings.ToLower(strings.TrimSpace(name)) {
	case models.FofaProviderHTTP:
		return newHTTPProvider(cfg, r.httpClient)
	case models.FofaProviderNone:
		return newNoneProvider("已显式选择 none provider：任务只记录状态，不查询任何 FOFA 数据")
	default:
		return nil
	}
}

// Statuses 所有内置 provider 的状态（status 端点一并返回，便于前端提示）
func (r *ProviderRegistry) Statuses(ctx context.Context) []ProviderStatus {
	cfg := r.Config(ctx)
	httpProvider := newHTTPProvider(cfg, r.httpClient)
	statuses := []ProviderStatus{
		newNoneProvider(noneReason(cfg)).Status(ctx),
		httpProvider.Status(ctx),
	}
	return statuses
}

// noneReason 未配置 key 时的明确原因
func noneReason(cfg ProviderConfig) string {
	if strings.TrimSpace(cfg.Key) == "" {
		return "未配置 fofa.key（可直接在本页下方填写 FOFA Key，或在系统配置/环境变量 FOFA_KEY 中设置）"
	}
	return "未配置 FOFA provider"
}

// LoadProviderConfig 读取 fofa.key / fofa.base_url。
// 优先级：系统配置集合 config > 环境变量 > 默认值。
func LoadProviderConfig(ctx context.Context) ProviderConfig {
	cfg := ProviderConfig{
		Key:           strings.TrimSpace(os.Getenv(EnvKey)),
		BaseURL:       strings.TrimRight(strings.TrimSpace(os.Getenv(EnvBaseURL)), "/"),
		KeySource:     "none",
		BaseURLSource: "default",
	}
	if cfg.Key != "" {
		cfg.KeySource = "env:" + EnvKey
	}
	if cfg.BaseURL != "" {
		cfg.BaseURLSource = "env:" + EnvBaseURL
	} else {
		cfg.BaseURL = DefaultBaseURL
	}
	if mongodb.DB == nil {
		return cfg
	}

	cursor, err := mongodb.DB.Collection("config").Find(ctx, bson.M{
		"name": bson.M{"$in": []string{FofaConfigKey, FofaConfigBaseURL}},
	})
	if err != nil {
		return cfg
	}
	defer cursor.Close(ctx)

	var docs []struct {
		Name  string      `bson:"name"`
		Value interface{} `bson:"value"`
	}
	if err := cursor.All(ctx, &docs); err != nil {
		return cfg
	}
	for _, doc := range docs {
		value := configValueString(doc.Value)
		switch doc.Name {
		case FofaConfigKey:
			if value != "" {
				cfg.Key = value
				cfg.KeySource = "config:" + FofaConfigKey
			}
		case FofaConfigBaseURL:
			if value != "" {
				cfg.BaseURL = strings.TrimRight(value, "/")
				cfg.BaseURLSource = "config:" + FofaConfigBaseURL
			}
		}
	}
	return cfg
}

// configValueString 配置值统一成字符串（历史库里 value 可能是数字/布尔）
func configValueString(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}
