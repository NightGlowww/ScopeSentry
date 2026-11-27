package dictionary

import (
	"context"
	"errors"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	dictrepo "github.com/Autumn-27/ScopeSentry/internal/repositories/dictionary"
	nodessvc "github.com/Autumn-27/ScopeSentry/internal/services/node"
)

type PortService interface {
	Get(ctx context.Context, search string, pageIndex, pageSize int) ([]models.PortDoc, int64, error)
	Add(ctx context.Context, name, value string) error
	Update(ctx context.Context, id, name, value string) error
	// Delete 返回真实删除条数
	Delete(ctx context.Context, ids []string) (int64, error)
}

type portService struct {
	repo        dictrepo.PortRepository
	nodeService nodessvc.Service
}

func NewPortService() PortService {
	return &portService{
		repo:        dictrepo.NewPortRepository(),
		nodeService: nodessvc.NewService(),
	}
}

func (s *portService) Get(ctx context.Context, search string, pageIndex, pageSize int) ([]models.PortDoc, int64, error) {
	total, err := s.repo.Count(ctx, search)
	if err != nil {
		return nil, 0, err
	}
	list, err := s.repo.Find(ctx, search, pageIndex, pageSize)
	if err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// 端口字典的入参错误：用 sentinel 让 handler 能映射成具体文案，而不是笼统 400。
var (
	ErrPortNameRequired  = errors.New("port name is required")
	ErrPortValueRequired = errors.New("port value is required")
)

func (s *portService) Add(ctx context.Context, name, value string) error {
	// name 是端口名（http/https/…），空名字会在字典里留下一条无名行；
	// 端口字典会通过 {dict.port.*} 进入扫描参数，垃圾行会一路带到节点。
	name = strings.TrimSpace(name)
	value = strings.TrimSpace(value)
	if name == "" {
		return ErrPortNameRequired
	}
	if value == "" {
		return ErrPortValueRequired
	}
	if err := s.repo.Insert(ctx, name, value); err != nil {
		return err
	}
	_ = s.nodeService.RefreshConfig(ctx, models.Message{Name: "all", Type: "port"})
	return nil
}

func (s *portService) Update(ctx context.Context, id, name, value string) error {
	// 改也不能改空：把已有行改成空白，等于把这条端口从字典里抹掉却留着一条空行。
	name = strings.TrimSpace(name)
	value = strings.TrimSpace(value)
	if name == "" {
		return ErrPortNameRequired
	}
	if value == "" {
		return ErrPortValueRequired
	}
	if err := s.repo.Update(ctx, id, name, value); err != nil {
		return err
	}
	_ = s.nodeService.RefreshConfig(ctx, models.Message{Name: "all", Type: "port"})
	return nil
}

func (s *portService) Delete(ctx context.Context, ids []string) (int64, error) {
	deleted, err := s.repo.Delete(ctx, ids)
	if err != nil {
		return 0, err
	}
	_ = s.nodeService.RefreshConfig(ctx, models.Message{Name: "all", Type: "port"})
	return deleted, nil
}
