package httpstatus

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	repo "github.com/Autumn-27/ScopeSentry/internal/repositories/assets/httpstatus"
	"go.mongodb.org/mongo-driver/bson"
)

// ErrInvalidStatus status 既不是 2xx 分档也不是合法状态码
var ErrInvalidStatus = errors.New("invalid http status filter")

// Service HTTP 状态视图
type Service interface {
	List(ctx context.Context, req models.HTTPStatusListRequest) (*models.HTTPStatusListResult, error)
}

type service struct{ repo repo.Repository }

// NewService 创建服务
func NewService() Service { return &service{repo: repo.NewRepository()} }

// List 组装过滤条件并分页查询
func (s *service) List(ctx context.Context, req models.HTTPStatusListRequest) (*models.HTTPStatusListResult, error) {
	filter, err := BuildFilter(req)
	if err != nil {
		return nil, err
	}
	pageIndex, pageSize := req.PageIndex, req.PageSize
	if pageIndex < 1 {
		pageIndex = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}
	rows, total, err := s.repo.List(ctx, filter, pageIndex, pageSize)
	if err != nil {
		return nil, err
	}
	return &models.HTTPStatusListResult{List: rows, Total: total}, nil
}

// BuildFilter 纯函数：status 分档/精确码 + keyword + project
func BuildFilter(req models.HTTPStatusListRequest) (bson.M, error) {
	filter := bson.M{}
	status := strings.ToLower(strings.TrimSpace(req.Status))
	switch {
	case status == "":
	case len(status) == 3 && strings.HasSuffix(status, "xx"):
		base, err := strconv.Atoi(status[:1])
		if err != nil || base < 1 || base > 5 {
			return nil, fmt.Errorf("%w: %q", ErrInvalidStatus, req.Status)
		}
		filter["statuscode"] = bson.M{"$gte": base * 100, "$lt": (base + 1) * 100}
	default:
		code, err := strconv.Atoi(status)
		if err != nil || code < 100 || code > 599 {
			return nil, fmt.Errorf("%w: %q", ErrInvalidStatus, req.Status)
		}
		filter["statuscode"] = code
	}
	if p := strings.TrimSpace(req.Project); p != "" {
		filter["project"] = p
	}
	if kw := strings.TrimSpace(req.Keyword); kw != "" {
		re := bson.M{"$regex": regexp.QuoteMeta(kw), "$options": "i"}
		filter["$or"] = []bson.M{{"host": re}, {"url": re}, {"title": re}}
	}
	return filter, nil
}
