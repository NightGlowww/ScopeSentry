package tagcolor

import (
	"context"

	repotc "github.com/Autumn-27/ScopeSentry/internal/repositories/assets/tagcolor"
	"github.com/Autumn-27/ScopeSentry/internal/utils/tagpalette"
)

// Service 标签颜色覆盖的业务层
type Service interface {
	List(ctx context.Context) (map[string]string, error)
	// Set 设置或清除某个标签的颜色：color 为空表示删除覆盖
	Set(ctx context.Context, tag, color string) error
}

type service struct {
	repo repotc.Repository
}

// NewService 创建服务实例
func NewService() Service {
	return &service{repo: repotc.NewRepository()}
}

// List 返回当前所有颜色覆盖。
func (s *service) List(ctx context.Context) (map[string]string, error) {
	return s.repo.List(ctx)
}

// Set 校验后写入；color 为空时删除覆盖，让标签回到默认配色。
func (s *service) Set(ctx context.Context, tag, color string) error {
	normalizedTag, err := tagpalette.NormalizeTag(tag)
	if err != nil {
		return err
	}
	normalizedColor, err := tagpalette.NormalizeColor(color)
	if err != nil {
		return err
	}
	if normalizedColor == "" {
		return s.repo.Delete(ctx, normalizedTag)
	}
	return s.repo.Set(ctx, normalizedTag, normalizedColor)
}
