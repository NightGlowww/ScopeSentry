package assetoverview

import (
	"context"
	"fmt"
	"sync"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	commonrepo "github.com/Autumn-27/ScopeSentry/internal/repositories/assets/common"
	certservice "github.com/Autumn-27/ScopeSentry/internal/services/assets/cert"
	cipservice "github.com/Autumn-27/ScopeSentry/internal/services/assets/cip"
)

// Service 资产族数量总览服务。
type Service interface {
	// Count 一次返回全部资产族数量
	Count(ctx context.Context) (*models.AssetOverviewCount, error)
}

type service struct {
	common commonrepo.Repository
	cert   certservice.Service
	cip    cipservice.Service
}

// NewService 创建资产总览服务实例
func NewService() Service {
	return &service{
		common: commonrepo.NewRepository(),
		cert:   certservice.NewService(),
		cip:    cipservice.NewService(),
	}
}

// Count 资产族数量。
//
// asset/subdomain/url/app/mp/vulnerability/sensitive 是物理集合，用
// EstimatedDocumentCount（只读集合元数据，不扫文档）；cert/cip 是读模型，
// 没有独立集合，必须走聚合 count。
func (s *service) Count(ctx context.Context) (*models.AssetOverviewCount, error) {
	out := &models.AssetOverviewCount{}
	errs := make([]error, 9)
	var wg sync.WaitGroup

	physical := []struct {
		collection string
		assign     *int64
		slot       int
	}{
		{"asset", &out.Asset, 0},
		{"subdomain", &out.Subdomain, 1},
		{"UrlScan", &out.URL, 2},
		{"app", &out.App, 3},
		{"mp", &out.MP, 4},
		{"vulnerability", &out.Vulnerability, 5},
		{"SensitiveResult", &out.Sensitive, 6},
	}
	for _, item := range physical {
		item := item
		wg.Add(1)
		go func() {
			defer wg.Done()
			*item.assign, errs[item.slot] = s.common.EstimatedDocumentCount(ctx, item.collection)
		}()
	}

	wg.Add(2)
	go func() {
		defer wg.Done()
		out.Cert, errs[7] = s.cert.Count(ctx)
	}()
	go func() {
		defer wg.Done()
		out.CIP, errs[8] = s.cip.CountSubjects(ctx)
	}()

	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("count asset overview: %w", err)
		}
	}
	return out, nil
}
