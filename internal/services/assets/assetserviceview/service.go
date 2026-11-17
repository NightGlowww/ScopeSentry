package assetserviceview

import (
	"context"
	"regexp"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	repo "github.com/Autumn-27/ScopeSentry/internal/repositories/assets/assetserviceview"
	"go.mongodb.org/mongo-driver/bson"
)

// Service 服务维度读模型
type Service interface {
	List(ctx context.Context, req models.AssetServiceListRequest) (*models.AssetServiceListResult, error)
}

type service struct{ repo repo.Repository }

// NewService 创建服务
func NewService() Service { return &service{repo: repo.NewRepository()} }

// List 组装过滤条件并聚合
func (s *service) List(ctx context.Context, req models.AssetServiceListRequest) (*models.AssetServiceListResult, error) {
	pageIndex, pageSize := req.PageIndex, req.PageSize
	if pageIndex < 1 {
		pageIndex = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}
	rows, total, err := s.repo.List(ctx, BuildMatch(req), pageIndex, pageSize)
	if err != nil {
		return nil, err
	}
	return &models.AssetServiceListResult{List: rows, Total: total}, nil
}

// BuildMatch 纯函数：排除空 service，叠加 project/keyword
func BuildMatch(req models.AssetServiceListRequest) bson.M {
	match := bson.M{"service": bson.M{"$nin": []interface{}{"", nil}}}
	if p := strings.TrimSpace(req.Project); p != "" {
		match["project"] = p
	}
	if kw := strings.TrimSpace(req.Keyword); kw != "" {
		match["service"] = bson.M{"$regex": regexp.QuoteMeta(kw), "$options": "i", "$nin": []interface{}{"", nil}}
	}
	return match
}
