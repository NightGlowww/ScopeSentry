package icp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	icprepo "github.com/Autumn-27/ScopeSentry/internal/repositories/icp"
	"go.mongodb.org/mongo-driver/bson"
)

// ICP provider 抽象。
//
// 契约只有四个方法（Name/Status/QueryDomain/QueryCompany），任务运行、重试、去重、
// 统计全部在 service 层完成 —— provider 只负责「给我某个域名/企业的备案记录」。
// 三个内置实现：
//   - manual：从本地 icp_result 回读已导入记录，永远可用（没有外部凭据也能跑通链路）；
//   - none：显式关闭 / 未配置，调用时返回明确原因；
//   - http：外部 ICP/天眼查类服务，baseURL/token 来自系统配置 icp.provider_url / icp.token。
//
// 测试用 httptest 假服务即可覆盖 http provider，不需要真实凭据。

// ErrProviderUnavailable provider 不可用（缺配置 / 显式 none）
var ErrProviderUnavailable = errors.New("icp provider unavailable")

// ProviderError 带状态码的 provider 错误。
// 429 与 5xx 视为可重试（service 层指数退避，最多 3 次），其余直接失败。
type ProviderError struct {
	StatusCode int
	Message    string
}

func (e *ProviderError) Error() string {
	if strings.TrimSpace(e.Message) == "" {
		return fmt.Sprintf("icp provider http %d", e.StatusCode)
	}
	return fmt.Sprintf("icp provider http %d: %s", e.StatusCode, e.Message)
}

// Retryable 429 / 5xx 可重试
func (e *ProviderError) Retryable() bool {
	return e.StatusCode == http.StatusTooManyRequests || e.StatusCode >= 500
}

// retryableError 供 service 层判断是否退避重试
type retryableError interface {
	Retryable() bool
}

// isRetryableError 只有显式声明 Retryable()=true 的错误才重试
func isRetryableError(err error) bool {
	var re retryableError
	if errors.As(err, &re) {
		return re.Retryable()
	}
	return false
}

// ICPProvider 数据来源契约
type ICPProvider interface {
	// Name provider 名称（落库到任务的 provider 字段）
	Name() string
	// Status 是否可用 + 不可用原因；不可用不代表任务不能创建，但运行时会失败并暂停
	Status() (available bool, reason string)
	QueryDomain(ctx context.Context, domain string) ([]models.ICPRecord, error)
	QueryCompany(ctx context.Context, company string) ([]models.ICPRecord, error)
}

// ProviderStatus provider 状态（tyc_check 返回）
type ProviderStatus struct {
	Name      string `json:"provider"`
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
}

// ProviderConfig 外部 provider 配置（系统配置集合 config）
type ProviderConfig struct {
	// Provider 显式指定 provider（manual / none / http），为空时按是否配置 URL 自动选择
	Provider string
	// BaseURL icp.provider_url
	BaseURL string
	// Token icp.token（可为空：自建服务通常不需要鉴权）
	Token string
}

// 系统配置键名（与任务要求一致）
const (
	IcpConfigProvider = "icp.provider"
	IcpConfigURL      = "icp.provider_url"
	IcpConfigToken    = "icp.token"
)

// DefaultProviderHTTPTimeout 单次 provider 请求超时
const DefaultProviderHTTPTimeout = 20 * time.Second

// ProviderRegistry provider 注册表：按任务/配置解析具体实现
type ProviderRegistry struct {
	results    icprepo.ResultRepository
	httpClient *http.Client
	config     func(context.Context) ProviderConfig
}

// NewProviderRegistry 创建注册表（results 供 manual provider 回读）
func NewProviderRegistry(results icprepo.ResultRepository) *ProviderRegistry {
	return &ProviderRegistry{
		results:    results,
		httpClient: &http.Client{Timeout: DefaultProviderHTTPTimeout},
		config:     LoadProviderConfig,
	}
}

// NewProviderRegistryWithConfig 用自定义配置源构造注册表。
// 生产走默认的 LoadProviderConfig（系统配置集合），测试/嵌入场景可以注入固定配置，
// 避免为了跑 provider 契约去改全局 config。
func NewProviderRegistryWithConfig(results icprepo.ResultRepository, config func(context.Context) ProviderConfig) *ProviderRegistry {
	r := NewProviderRegistry(results)
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
	return ProviderConfig{}
}

// Resolve 解析当前应使用的 provider：
//  1. 系统配置显式指定 icp.provider → 用它；
//  2. 配置了 icp.provider_url → http；
//  3. 都没有 → manual（本地已导入记录）。
func (r *ProviderRegistry) Resolve(ctx context.Context) ICPProvider {
	cfg := r.Config(ctx)
	if name := strings.ToLower(strings.TrimSpace(cfg.Provider)); name != "" {
		if p := r.Get(ctx, name); p != nil {
			return p
		}
	}
	if strings.TrimSpace(cfg.BaseURL) != "" {
		return newHTTPProvider(cfg, r.httpClient)
	}
	return newManualProvider(r.results)
}

// Get 按名称取 provider；未知名称返回 nil（调用方回落 Resolve）
func (r *ProviderRegistry) Get(ctx context.Context, name string) ICPProvider {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case models.IcpProviderManual:
		return newManualProvider(r.results)
	case models.IcpProviderNone:
		return newNoneProvider("已显式选择 none provider：任务只记录状态，不查询任何外部/本地来源")
	case models.IcpProviderHTTP:
		return newHTTPProvider(r.Config(ctx), r.httpClient)
	default:
		return nil
	}
}

// Statuses 所有内置 provider 的状态（tyc_check 一并返回，便于前端提示）
func (r *ProviderRegistry) Statuses(ctx context.Context) []ProviderStatus {
	out := make([]ProviderStatus, 0, 3)
	for _, p := range []ICPProvider{
		newManualProvider(r.results),
		newNoneProvider("未配置：显式选择 none 时任务不会查询任何来源"),
		newHTTPProvider(r.Config(ctx), r.httpClient),
	} {
		available, reason := p.Status()
		out = append(out, ProviderStatus{Name: p.Name(), Available: available, Reason: reason})
	}
	return out
}

// LoadProviderConfig 从系统配置集合 config 读取 icp.provider / icp.provider_url / icp.token。
// 读不到（DB 未初始化 / 未配置）返回零值，调用方据此回落到 manual。
func LoadProviderConfig(ctx context.Context) ProviderConfig {
	if mongodb.DB == nil {
		return ProviderConfig{}
	}
	cursor, err := mongodb.DB.Collection("config").Find(ctx, bson.M{
		"name": bson.M{"$in": []string{IcpConfigProvider, IcpConfigURL, IcpConfigToken}},
	})
	if err != nil {
		return ProviderConfig{}
	}
	defer cursor.Close(ctx)

	var docs []struct {
		Name  string      `bson:"name"`
		Value interface{} `bson:"value"`
	}
	if err := cursor.All(ctx, &docs); err != nil {
		return ProviderConfig{}
	}
	var cfg ProviderConfig
	for _, doc := range docs {
		value := configValueString(doc.Value)
		switch doc.Name {
		case IcpConfigProvider:
			cfg.Provider = value
		case IcpConfigURL:
			cfg.BaseURL = value
		case IcpConfigToken:
			cfg.Token = value
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
