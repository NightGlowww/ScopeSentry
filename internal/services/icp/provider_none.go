package icp

import (
	"context"
	"fmt"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// none provider：显式关闭 / 未配置。
//
// 保留它是为了让「没有可用数据源」这件事是显式可观测的：
// tyc_check 会返回 available=false + 具体原因，任务运行时会直接失败并进入暂停，
// 而不是静默返回 0 条让用户以为「查过了但没有备案」。

type noneProvider struct {
	reason string
}

func newNoneProvider(reason string) *noneProvider {
	if reason == "" {
		reason = "未配置 ICP provider"
	}
	return &noneProvider{reason: reason}
}

// Name provider 名称
func (p *noneProvider) Name() string { return models.IcpProviderNone }

// Status 永远不可用，原因随配置
func (p *noneProvider) Status() (bool, string) { return false, p.reason }

// QueryDomain 直接返回不可用原因（不重试）
func (p *noneProvider) QueryDomain(_ context.Context, _ string) ([]models.ICPRecord, error) {
	return nil, fmt.Errorf("%w: %s", ErrProviderUnavailable, p.reason)
}

// QueryCompany 直接返回不可用原因（不重试）
func (p *noneProvider) QueryCompany(_ context.Context, _ string) ([]models.ICPRecord, error) {
	return nil, fmt.Errorf("%w: %s", ErrProviderUnavailable, p.reason)
}
