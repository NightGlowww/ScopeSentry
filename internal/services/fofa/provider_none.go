package fofa

import (
	"context"
	"fmt"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// none provider：显式关闭 / 未配置。
//
// 保留它是为了让「没有可用数据源」这件事显式可观测：
// status 返回 available=false + 具体原因，search/task 直接返回 ErrProviderUnavailable，
// 而不是静默返回 0 条让用户以为「查过了但没有资产」。

type noneProvider struct {
	reason string
}

func newNoneProvider(reason string) *noneProvider {
	if reason == "" {
		reason = "未配置 FOFA provider"
	}
	return &noneProvider{reason: reason}
}

// Name provider 名称
func (p *noneProvider) Name() string { return models.FofaProviderNone }

// Status 永远不可用，原因随配置
func (p *noneProvider) Status(_ context.Context) ProviderStatus {
	return ProviderStatus{
		Provider:   models.FofaProviderNone,
		Available:  false,
		Reason:     p.reason,
		Configured: false,
		KeySource:  "none",
	}
}

// Search 直接返回不可用原因（不发任何请求）
func (p *noneProvider) Search(_ context.Context, _ string, _, _ int) (*SearchPage, error) {
	return nil, fmt.Errorf("%w: %s", ErrProviderUnavailable, p.reason)
}
