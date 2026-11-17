package wih

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	wihrepo "github.com/Autumn-27/ScopeSentry/internal/repositories/assets/wih"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 分页上限：默认 20 条，单页最多 200（handler 的 binding 会把 >200 拒成 400）
const (
	defaultPageSize = 20
	maxPageSize     = 200
)

// 详情查询的哨兵错误：handler 据此区分 400（id 格式错）和 404（记录不存在）
var (
	ErrInvalidID = errors.New("wih: invalid id")
	ErrNotFound  = errors.New("wih: not found")
)

// Service WIH 敏感信息读模型服务接口（对标 ARL-Next assetWih）
type Service interface {
	List(ctx context.Context, req models.WIHListRequest) (*models.WIHListResult, error)
	GetDetail(ctx context.Context, id string) (*models.WIHDetail, error)
}

type service struct {
	repo wihrepo.Repository
}

// NewService 创建 WIH 读模型服务实例
func NewService() Service {
	return &service{repo: wihrepo.NewRepository()}
}

// List 列表：type/project/keyword 过滤 + time 倒序分页。
func (s *service) List(ctx context.Context, req models.WIHListRequest) (*models.WIHListResult, error) {
	pageIndex, pageSize := normalizePage(req.PageIndex, req.PageSize)

	rows, total, err := s.repo.List(ctx, BuildListFilter(req), pageIndex, pageSize)
	if err != nil {
		return nil, fmt.Errorf("list wih results: %w", err)
	}
	return &models.WIHListResult{List: rows, Total: total}, nil
}

// GetDetail 单条记录。
func (s *service) GetDetail(ctx context.Context, id string) (*models.WIHDetail, error) {
	oid, err := primitive.ObjectIDFromHex(strings.TrimSpace(id))
	if err != nil {
		return nil, ErrInvalidID
	}

	detail, err := s.repo.FindByID(ctx, oid)
	if err != nil {
		return nil, fmt.Errorf("get wih result %s: %w", id, err)
	}
	if detail == nil {
		return nil, ErrNotFound
	}
	return detail, nil
}

// BuildListFilter 拼 WIH 列表过滤条件。
//
//   - type    -> sid（敏感规则名），逗号分隔可传多个，精确匹配（$in 走索引）
//   - project 精确
//   - keyword -> url / sid / rootDomain / match 子串（match 是数组，正则天然匹配任一元素）
func BuildListFilter(req models.WIHListRequest) bson.M {
	conds := bson.A{}

	if typ := strings.TrimSpace(req.Type); typ != "" {
		if values := splitCSV(typ); len(values) > 0 {
			conds = append(conds, bson.M{"sid": bson.M{"$in": values}})
		}
	}

	if project := strings.TrimSpace(req.Project); project != "" {
		conds = append(conds, bson.M{"project": project})
	}

	if keyword := strings.TrimSpace(req.Keyword); keyword != "" {
		pattern := primitive.Regex{Pattern: regexp.QuoteMeta(keyword), Options: "i"}
		conds = append(conds, bson.M{"$or": bson.A{
			bson.M{"url": pattern},
			bson.M{"sid": pattern},
			bson.M{"rootDomain": pattern},
			bson.M{"match": pattern},
		}})
	}

	return composeAnd(conds)
}

// splitCSV 逗号分隔取值去重（空片段丢掉）
func splitCSV(raw string) bson.A {
	seen := make(map[string]struct{})
	values := bson.A{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		values = append(values, part)
	}
	return values
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
