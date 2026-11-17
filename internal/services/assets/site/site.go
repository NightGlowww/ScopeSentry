package site

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	siterepo "github.com/Autumn-27/ScopeSentry/internal/repositories/assets/site"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 分页与链路上限：默认 20 条，单页最多 200（防止一次拉穿资产表）；
// 链路与 ARL 对齐，最多 500 条。
const (
	defaultPageSize = 20
	maxPageSize     = 200
	maxChainSize    = 500
)

// ErrEmptyHost 子域链路请求缺少 host
var ErrEmptyHost = errors.New("site chain: host is required")

// Service 站点读模型服务接口
type Service interface {
	ListSites(ctx context.Context, req models.SiteListRequest) (*models.SiteListResult, error)
	GetChain(ctx context.Context, host string) (*models.SiteChainResult, error)
}

type service struct {
	siteRepo siterepo.Repository
}

// NewService 创建站点读模型服务实例
func NewService() Service {
	return &service{
		siteRepo: siterepo.NewRepository(),
	}
}

// ListSites 站点列表：按 host+port 聚合 asset 集合。
func (s *service) ListSites(ctx context.Context, req models.SiteListRequest) (*models.SiteListResult, error) {
	pageIndex, pageSize := normalizePage(req.PageIndex, req.PageSize)
	match := buildSiteMatch(req)

	rows, total, err := s.siteRepo.ListSites(ctx, match, pageIndex, pageSize)
	if err != nil {
		return nil, fmt.Errorf("list sites: %w", err)
	}

	return &models.SiteListResult{List: rows, Total: total}, nil
}

// GetChain 子域链路：host 精确命中 + 同 rootDomain 的兄弟子域，按时间倒序，最多 maxChainSize 条。
func (s *service) GetChain(ctx context.Context, host string) (*models.SiteChainResult, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, ErrEmptyHost
	}

	chain, total, err := s.siteRepo.GetSubdomainChain(ctx, host, maxChainSize)
	if err != nil {
		return nil, fmt.Errorf("get subdomain chain for %q: %w", host, err)
	}

	return &models.SiteChainResult{List: chain, Total: total, Limit: maxChainSize}, nil
}

// buildSiteMatch 拼站点聚合的前置过滤条件。
//
// keyword 落在 host/ip/title/url 上（$or），project 精确匹配。
// 注意 keyword 是「聚合前」过滤：一行站点只要有一条资产命中就会留下，
// 展示字段仍然取该 host+port 下最新的一条，这是 ARL 侧同样的口径。
func buildSiteMatch(req models.SiteListRequest) bson.M {
	and := bson.A{
		// 空 host 的分组没有展示意义，直接排除（$exists 把「字段缺失」也挡掉）
		bson.M{"host": bson.M{"$exists": true, "$ne": ""}},
	}

	if project := strings.TrimSpace(req.Project); project != "" {
		and = append(and, bson.M{"project": project})
	}

	if keyword := strings.TrimSpace(req.Keyword); keyword != "" {
		// QuoteMeta：关键字里的 . * ( ) 不能当正则元字符用，否则 "10.0." 会误匹配 "10x0y"
		pattern := primitive.Regex{Pattern: regexp.QuoteMeta(keyword), Options: "i"}
		and = append(and, bson.M{"$or": bson.A{
			bson.M{"host": pattern},
			bson.M{"ip": pattern},
			bson.M{"title": pattern},
			bson.M{"url": pattern},
		}})
	}

	return bson.M{"$and": and}
}

// normalizePage 归一化分页参数：非法值兜默认，超大值截断
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
