package cert

import (
	"context"
	"fmt"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	certrepo "github.com/Autumn-27/ScopeSentry/internal/repositories/assets/cert"
)

// 分页上限：默认 20 条，单页最多 200。
// handler 层 binding 已经把 >200 拒成 400，这里的兜底只防内部调用绕过 binding。
const (
	defaultPageSize = 20
	maxPageSize     = 200
)

// Service 证书读模型服务接口（对标 ARL-Next assetCert）
type Service interface {
	// List 证书列表：合并同一证书、按 notAfter 升序分页
	List(ctx context.Context, req models.CertListRequest) (*models.CertListResult, error)
	// Detail 证书详情
	Detail(ctx context.Context, req models.CertDetailRequest) (*models.CertRow, error)
	// Count 证书总数（去重后）
	Count(ctx context.Context) (int64, error)
}

type service struct {
	repo certrepo.Repository
}

// NewService 创建证书读模型服务实例
func NewService() Service {
	return &service{repo: certrepo.NewRepository()}
}

// List 证书列表。
func (s *service) List(ctx context.Context, req models.CertListRequest) (*models.CertListResult, error) {
	pageIndex, pageSize := normalizePage(req.PageIndex, req.PageSize)
	rows, total, err := s.repo.List(ctx, req, pageIndex, pageSize, time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("list certificates: %w", err)
	}
	return &models.CertListResult{List: rows, Total: total}, nil
}

// Detail 证书详情。
func (s *service) Detail(ctx context.Context, req models.CertDetailRequest) (*models.CertRow, error) {
	row, err := s.repo.GetByID(ctx, req.ID, req.Project, time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("get certificate %q: %w", req.ID, err)
	}
	return row, nil
}

// Count 证书总数。
func (s *service) Count(ctx context.Context) (int64, error) {
	total, err := s.repo.Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("count certificates: %w", err)
	}
	return total, nil
}

// normalizePage 归一化分页参数
func normalizePage(pageIndex, pageSize int) (int, int) {
	if pageIndex < 1 {
		pageIndex = 1
	}
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return pageIndex, pageSize
}
