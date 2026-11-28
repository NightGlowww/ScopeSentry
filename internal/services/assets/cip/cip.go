package cip

import (
	"context"
	"fmt"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	ciprepo "github.com/Autumn-27/ScopeSentry/internal/repositories/assets/cip"
)

// 分页上限：默认 20 条，单页最多 200。
const (
	defaultPageSize = 20
	maxPageSize     = 200
)

// Service ICP/主体读模型服务接口（对标 ARL-Next assetCip）
type Service interface {
	// List 主体列表：按 company 分组统计域名数
	List(ctx context.Context, req models.CIPListRequest) (*models.CIPListResult, error)
	// Detail 按域名反查主体，返回主体下的域名列表（分页）
	Detail(ctx context.Context, req models.CIPDetailRequest) (*models.CIPDetailResult, error)
	// CountSubjects 主体总数（去重后的 company 数）
	CountSubjects(ctx context.Context) (int64, error)
}

type service struct {
	repo ciprepo.Repository
}

// NewService 创建 ICP/主体读模型服务实例
func NewService() Service {
	return &service{repo: ciprepo.NewRepository()}
}

// List 主体列表。
func (s *service) List(ctx context.Context, req models.CIPListRequest) (*models.CIPListResult, error) {
	pageIndex, pageSize := normalizePage(req.PageIndex, req.PageSize)
	rows, total, err := s.repo.List(ctx, req, pageIndex, pageSize)
	if err != nil {
		return nil, fmt.Errorf("list cip subjects: %w", err)
	}
	return &models.CIPListResult{List: rows, Total: total}, nil
}

// Detail 按域名反查主体。
func (s *service) Detail(ctx context.Context, req models.CIPDetailRequest) (*models.CIPDetailResult, error) {
	pageIndex, pageSize := normalizePage(req.PageIndex, req.PageSize)

	root, err := s.repo.FindRootDomain(ctx, req.Domain)
	if err != nil {
		return nil, fmt.Errorf("find root domain %q: %w", req.Domain, err)
	}
	if root == nil {
		// 只在 icp_result 里存在的域名（ICP 任务产出、RootDomain 没这行）：
		// 以前这里直接 return nil → 详情 404，可列表里明明有这个主体。
		company, cerr := s.repo.CompanyOfDomain(ctx, req.Domain)
		if cerr != nil {
			return nil, cerr
		}
		if company == "" {
			return nil, nil
		}
		icps, ierr := s.repo.ICPsByCompany(ctx, company, req.Project)
		if ierr != nil {
			return nil, ierr
		}
		rows, total, lerr := s.repo.ListDomainsByCompany(ctx, company, req.Project, pageIndex, pageSize)
		if lerr != nil {
			return nil, lerr
		}
		result := &models.CIPDetailResult{
			Domain:    strings.TrimSpace(req.Domain),
			Company:   company,
			ICPs:      icps,
			List:      rows,
			Total:     total,
			PageIndex: pageIndex,
			PageSize:  pageSize,
		}
		if len(icps) > 0 {
			result.ICP = icps[0]
		}
		return result, nil
	}

	result := &models.CIPDetailResult{
		Domain:    root.Domain,
		Company:   strings.TrimSpace(root.Company),
		PageIndex: pageIndex,
		PageSize:  pageSize,
	}

	if result.Company == "" {
		// 没有主体名的域名只作为单条记录返回，不硬凑成一个主体
		result.Total = 1
		if root.ICP != "" {
			result.ICP = root.ICP
			result.ICPs = []string{root.ICP}
		} else {
			result.ICPs = []string{}
		}
		if pageIndex == 1 {
			result.List = []models.CIPDomain{toCIPDomain(*root)}
		} else {
			result.List = []models.CIPDomain{}
		}
		return result, nil
	}

	icps, err := s.repo.ICPsByCompany(ctx, result.Company, req.Project)
	if err != nil {
		return nil, err
	}
	if icps == nil {
		icps = []string{}
	}
	result.ICPs = icps
	if len(icps) > 0 {
		result.ICP = icps[0]
	}

	rows, total, err := s.repo.ListDomainsByCompany(ctx, result.Company, req.Project, pageIndex, pageSize)
	if err != nil {
		return nil, err
	}
	result.List = rows
	result.Total = total
	return result, nil
}

// CountSubjects 主体总数。
func (s *service) CountSubjects(ctx context.Context) (int64, error) {
	total, err := s.repo.CountSubjects(ctx)
	if err != nil {
		return 0, fmt.Errorf("count cip subjects: %w", err)
	}
	return total, nil
}

func toCIPDomain(root models.RootDomain) models.CIPDomain {
	return models.CIPDomain{
		Domain:  root.Domain,
		ICP:     root.ICP,
		Project: root.Project,
		Time:    root.Time,
		Tags:    root.Tags,
	}
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
