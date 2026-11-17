package ip

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	iprepo "github.com/Autumn-27/ScopeSentry/internal/repositories/assets/ip"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 分页与反查上限：默认 20 条/页，单页最多 200；单个 IP 的域名反查最多 1000 条。
const (
	defaultIPPageSize = 20
	maxIPPageSize     = 200
	maxDomainsPerIP   = 1000
)

// ErrEmptyIP 反查域名的请求缺少 ip
var ErrEmptyIP = errors.New("ip domains: ip is required")

// ReadModelService IP 读模型服务接口（对标 ARL-Next assetIP）
type ReadModelService interface {
	ListIPs(ctx context.Context, req models.IPListRequest) (*models.IPListResult, error)
	ListDomainsByIP(ctx context.Context, ip string) (*models.IPDomainsResult, error)
}

type readModelService struct {
	repo iprepo.ReadModelRepository
}

// NewReadModelService 创建 IP 读模型服务实例
func NewReadModelService() ReadModelService {
	return &readModelService{
		repo: iprepo.NewReadModelRepository(),
	}
}

// ListIPs IP 列表：按 ip 聚合 asset 集合，带端口/域名摘要。
func (s *readModelService) ListIPs(ctx context.Context, req models.IPListRequest) (*models.IPListResult, error) {
	pageIndex, pageSize := normalizeIPPage(req.PageIndex, req.PageSize)
	match := buildIPMatch(req)

	rows, total, err := s.repo.ListIPs(ctx, match, pageIndex, pageSize)
	if err != nil {
		return nil, fmt.Errorf("list ip assets: %w", err)
	}

	return &models.IPListResult{List: rows, Total: total}, nil
}

// ListDomainsByIP 反查某个 IP 上解析到的域名（asset.host + subdomain.ip）。
func (s *readModelService) ListDomainsByIP(ctx context.Context, ip string) (*models.IPDomainsResult, error) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return nil, ErrEmptyIP
	}

	items, total, err := s.repo.ListDomainsByIP(ctx, ip, maxDomainsPerIP)
	if err != nil {
		return nil, fmt.Errorf("list domains for ip %q: %w", ip, err)
	}

	return &models.IPDomainsResult{List: items, Total: total}, nil
}

// buildIPMatch 拼 IP 聚合的前置过滤条件。
//
// ip 走前缀匹配（按网段筛），keyword 走子串匹配；两者都做 QuoteMeta，
// 避免 "10.0." 这种输入被当成正则通配。
func buildIPMatch(req models.IPListRequest) bson.M {
	and := bson.A{
		bson.M{"ip": bson.M{"$exists": true, "$ne": ""}},
	}

	if prefix := strings.TrimSpace(req.IP); prefix != "" {
		and = append(and, bson.M{"ip": primitive.Regex{
			Pattern: "^" + regexp.QuoteMeta(prefix),
			Options: "i",
		}})
	}

	if keyword := strings.TrimSpace(req.Keyword); keyword != "" {
		and = append(and, bson.M{"ip": primitive.Regex{
			Pattern: regexp.QuoteMeta(keyword),
			Options: "i",
		}})
	}

	return bson.M{"$and": and}
}

// normalizeIPPage 归一化分页参数
func normalizeIPPage(pageIndex, pageSize int) (int, int) {
	if pageIndex < 1 {
		pageIndex = 1
	}
	if pageSize <= 0 {
		pageSize = defaultIPPageSize
	}
	if pageSize > maxIPPageSize {
		pageSize = maxIPPageSize
	}
	return pageIndex, pageSize
}
