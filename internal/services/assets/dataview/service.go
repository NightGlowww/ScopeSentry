package dataview

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	repo "github.com/Autumn-27/ScopeSentry/internal/repositories/assets/dataview"
	"go.mongodb.org/mongo-driver/bson"
)

// 集合名：与 ARL-Next 一致，节点侧未来直接写入即可生效。
const (
	FileLeakCollection = "asset_fileleak"
	NpocCollection     = "asset_npoc_service"
)

// ErrInvalidIDs 删除请求里的 id 非法/为空
var ErrInvalidIDs = errors.New("invalid ids")

// Service 文件泄露 / NPoC 服务两个只读视图 + 批量删除。
type Service interface {
	ListFileLeak(ctx context.Context, req models.FileLeakListRequest) ([]bson.M, int64, error)
	DeleteFileLeak(ctx context.Context, ids []string) (int64, error)
	ListNpoc(ctx context.Context, req models.NpocServiceListRequest) ([]bson.M, int64, error)
	DeleteNpoc(ctx context.Context, ids []string) (int64, error)
}

type service struct{ repo repo.Repository }

// NewService 创建服务
func NewService() Service { return &service{repo: repo.NewRepository()} }

// ListFileLeak 文件泄露列表：keyword 匹配 url/site/title，statusCode 精确。
func (s *service) ListFileLeak(ctx context.Context, req models.FileLeakListRequest) ([]bson.M, int64, error) {
	filter := bson.M{}
	if kw := strings.TrimSpace(req.Keyword); kw != "" {
		re := bson.M{"$regex": regexp.QuoteMeta(kw), "$options": "i"}
		filter["$or"] = []bson.M{{"url": re}, {"site": re}, {"title": re}}
	}
	if req.StatusCode > 0 {
		filter["status_code"] = req.StatusCode
	}
	return s.list(ctx, FileLeakCollection, filter, req.PageIndex, req.PageSize)
}

// DeleteFileLeak 批量删除文件泄露结果。
func (s *service) DeleteFileLeak(ctx context.Context, ids []string) (int64, error) {
	return s.delete(ctx, FileLeakCollection, ids)
}

// ListNpoc NPoC 服务列表：keyword 匹配 scheme/host/target，scheme/host 精确。
func (s *service) ListNpoc(ctx context.Context, req models.NpocServiceListRequest) ([]bson.M, int64, error) {
	filter := bson.M{}
	if kw := strings.TrimSpace(req.Keyword); kw != "" {
		re := bson.M{"$regex": regexp.QuoteMeta(kw), "$options": "i"}
		filter["$or"] = []bson.M{{"scheme": re}, {"host": re}, {"target": re}}
	}
	if v := strings.TrimSpace(req.Scheme); v != "" {
		filter["scheme"] = v
	}
	if v := strings.TrimSpace(req.Host); v != "" {
		filter["host"] = v
	}
	return s.list(ctx, NpocCollection, filter, req.PageIndex, req.PageSize)
}

// DeleteNpoc 批量删除 NPoC 服务结果。
func (s *service) DeleteNpoc(ctx context.Context, ids []string) (int64, error) {
	return s.delete(ctx, NpocCollection, ids)
}

func (s *service) list(ctx context.Context, collection string, filter bson.M, pageIndex, pageSize int) ([]bson.M, int64, error) {
	rows, total, err := s.repo.List(ctx, collection, filter, bson.D{{Key: "insert_time", Value: -1}}, pageIndex, pageSize)
	if err != nil {
		return nil, 0, err
	}
	// 统一把 _id 暴露为 id（前端契约），其余字段原样透传。
	for _, row := range rows {
		if v, ok := row["_id"]; ok {
			row["id"] = v
			delete(row, "_id")
		}
	}
	return rows, total, nil
}

func (s *service) delete(ctx context.Context, collection string, ids []string) (int64, error) {
	if len(ids) == 0 {
		return 0, ErrInvalidIDs
	}
	return s.repo.DeleteByIDs(ctx, collection, ids)
}
