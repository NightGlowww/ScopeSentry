package statfinger

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	statfingerrepo "github.com/Autumn-27/ScopeSentry/internal/repositories/assets/statfinger"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 分页上限：默认 20 条，单页最多 200。
// handler 层的 binding 已经把 >200 拒成 400，这里的兜底只防内部调用绕过 binding。
const (
	defaultPageSize = 20
	maxPageSize     = 200
)

// Service 指纹统计读模型服务接口（对标 ARL-Next assetStatFinger）
type Service interface {
	// ListTechnologies 技术名统计列表
	ListTechnologies(ctx context.Context, req models.StatFingerListRequest) (*models.StatFingerListResult, error)
	// ListAssetsByTechnology 使用某技术的资产列表
	ListAssetsByTechnology(ctx context.Context, req models.StatFingerAssetsRequest) (*models.StatFingerAssetsResult, error)
}

type service struct {
	repo statfingerrepo.Repository
}

// NewService 创建指纹统计读模型服务实例
func NewService() Service {
	return &service{repo: statfingerrepo.NewRepository()}
}

// ListTechnologies 技术名统计：count 倒序分页。
func (s *service) ListTechnologies(ctx context.Context, req models.StatFingerListRequest) (*models.StatFingerListResult, error) {
	pageIndex, pageSize := normalizePage(req.PageIndex, req.PageSize)

	rows, total, err := s.repo.ListTechnologies(ctx,
		BuildStatFingerPreMatch(req), BuildStatFingerPostMatch(req), pageIndex, pageSize)
	if err != nil {
		return nil, fmt.Errorf("list technologies: %w", err)
	}

	return &models.StatFingerListResult{List: rows, Total: total}, nil
}

// ListAssetsByTechnology 反查使用某技术的资产。
func (s *service) ListAssetsByTechnology(ctx context.Context, req models.StatFingerAssetsRequest) (*models.StatFingerAssetsResult, error) {
	pageIndex, pageSize := normalizePage(req.PageIndex, req.PageSize)

	rows, total, err := s.repo.ListAssetsByTechnology(ctx, BuildTechnologyAssetsFilter(req), pageIndex, pageSize)
	if err != nil {
		return nil, fmt.Errorf("list assets by technology %q: %w", req.Technology, err)
	}

	return &models.StatFingerAssetsResult{List: rows, Total: total}, nil
}

// BuildStatFingerPreMatch 拼 $unwind 之前的过滤条件。
//
// 这里只用「索引友好」的条件：
//   - technologies 存在（缺失字段的文档没有统计意义，$unwind 也会丢掉）；
//   - project 精确匹配。
//
// 关键字不放在这里：它只应该匹配技术名本身，放到 unwind 之前会把
// 「同一文档里另一个技术名命中」的文档整条带进来，统计结果就脏了。
func BuildStatFingerPreMatch(req models.StatFingerListRequest) bson.M {
	conds := bson.A{bson.M{"technologies": bson.M{"$exists": true}}}
	if project := strings.TrimSpace(req.Project); project != "" {
		conds = append(conds, bson.M{"project": project})
	}
	return composeAnd(conds)
}

// BuildStatFingerPostMatch 拼 $unwind 之后的技术名过滤（关键字子串，不区分大小写）。
// 返回空文档表示不过滤。
func BuildStatFingerPostMatch(req models.StatFingerListRequest) bson.M {
	keyword := strings.TrimSpace(req.Keyword)
	if keyword == "" {
		return bson.M{}
	}
	return bson.M{"technologies": primitive.Regex{
		Pattern: regexp.QuoteMeta(keyword),
		Options: "i",
	}}
}

// BuildTechnologyAssetsFilter 拼「按技术反查资产」的过滤条件。
// technology 走数组元素精确匹配（multikey 索引），project 可选收窄。
func BuildTechnologyAssetsFilter(req models.StatFingerAssetsRequest) bson.M {
	conds := bson.A{bson.M{"technologies": strings.TrimSpace(req.Technology)}}
	if project := strings.TrimSpace(req.Project); project != "" {
		conds = append(conds, bson.M{"project": project})
	}
	return composeAnd(conds)
}

// composeAnd 组合条件：空列表返回空文档（match all），单个条件直接返回，
// 多个条件才包 $and，避免生成 Mongo 不接受的空 $and 数组。
func composeAnd(conds bson.A) bson.M {
	switch len(conds) {
	case 0:
		return bson.M{}
	case 1:
		if m, ok := conds[0].(bson.M); ok {
			return m
		}
		return bson.M{"$and": conds}
	default:
		return bson.M{"$and": conds}
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
