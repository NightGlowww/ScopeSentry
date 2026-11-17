package icp

import (
	"context"
	"fmt"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	icprepo "github.com/Autumn-27/ScopeSentry/internal/repositories/icp"
)

// manual provider：从本地 icp_result 回读已经导入的备案记录。
//
// 这是「没有外部凭据也能用」的兜底路径：手工导入（或历史任务写入）的记录
// 可以再被别的任务按域名/企业反查出来，任务链、去重、同步、导出全都能跑通。
// 它永远可用，不需要任何凭据。

// manualRecordLimit 单次回读上限（防止一个域名关联出上万条把内存打满）
const manualRecordLimit = 500

type manualProvider struct {
	results icprepo.ResultRepository
}

func newManualProvider(results icprepo.ResultRepository) *manualProvider {
	return &manualProvider{results: results}
}

// Name provider 名称
func (p *manualProvider) Name() string { return models.IcpProviderManual }

// Status manual 永远可用
func (p *manualProvider) Status() (bool, string) {
	return true, "使用本地 icp_result 中已导入的备案记录（无需外部凭据）"
}

// QueryDomain 按域名回读本地记录
func (p *manualProvider) QueryDomain(ctx context.Context, domain string) ([]models.ICPRecord, error) {
	if p.results == nil {
		return nil, fmt.Errorf("%w: 本地结果仓库未初始化", ErrProviderUnavailable)
	}
	return p.results.FindByDomain(ctx, domain, manualRecordLimit)
}

// QueryCompany 按企业名回读本地记录
func (p *manualProvider) QueryCompany(ctx context.Context, company string) ([]models.ICPRecord, error) {
	if p.results == nil {
		return nil, fmt.Errorf("%w: 本地结果仓库未初始化", ErrProviderUnavailable)
	}
	return p.results.FindByCompany(ctx, company, manualRecordLimit)
}
