// services-------------------------------------
// @file      : manage.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2025/4/28 20:29
// -------------------------------------------

package dictionary

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	dictrepo "github.com/Autumn-27/ScopeSentry/internal/repositories/dictionary"
	nodessvc "github.com/Autumn-27/ScopeSentry/internal/services/node"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ManageService interface {
	// ---- 历史接口（签名/行为保持不变）----
	List(ctx context.Context) ([]models.DictionaryMeta, error)
	Create(ctx context.Context, name, category string, content []byte) error
	Download(ctx context.Context, id string) (filename string, data []byte, err error)
	Delete(ctx context.Context, ids []string) error
	Save(ctx context.Context, id string, content []byte) error

	// ---- 对标 ARL-Next 的字典能力（dictionary.py / brute_dict.py / cdn_dict.py）----
	// ListPaged 按 kind 分页列出字典；search 为名称子串匹配
	ListPaged(ctx context.Context, kind, search string, pageIndex, pageSize int) (*models.DictionaryListResult, error)
	// Preview 预览前 N 行（上限 200）
	Preview(ctx context.Context, kind, name string, limit int) (*models.DictionaryPreviewResult, error)
	// SearchLines 在单个字典内搜行
	SearchLines(ctx context.Context, kind, name, keyword string, limit int) (*models.DictionarySearchResult, error)
	// Append 追加行（唯一索引去重）
	Append(ctx context.Context, kind, name string, lines []string) (*models.DictionaryAppendResult, error)
	// DeleteEntries 删除指定行
	DeleteEntries(ctx context.Context, kind, name string, entries []string) (*models.DictionaryDeleteEntriesResult, error)
	// DeleteFileByName 删除整个字典（元数据 + 行 + 内容）
	DeleteFileByName(ctx context.Context, kind, name string) (*models.DictionaryDeleteFileResult, error)
	// OpenDownload 流式下载
	OpenDownload(ctx context.Context, kind, name string) (reader io.ReadCloser, length int64, filename string, err error)
	// CreateByKind 新建字典（重名报错）
	CreateByKind(ctx context.Context, kind, name, content string) (*models.DictionaryCreateResult, error)
	// SaveByKind 整份保存（存在则覆盖），供 dict_cdn / dict_brute 使用
	SaveByKind(ctx context.Context, kind, name, content string, lines []string) (*models.DictionaryCreateResult, error)
	// BackfillEntryIndex 为历史字典补建行级索引（否则列表 lineCount 恒为 0）
	BackfillEntryIndex(ctx context.Context) (int, error)
}

type manageService struct {
	repo        dictrepo.ManageRepository
	entryRepo   dictrepo.EntryRepository
	nodeService nodessvc.Service
	locks       *keyedMutex
	lines       *lineCache
}

var (
	sharedManageOnce sync.Once
	sharedManage     *manageService
)

// sharedManageService 进程级单例：按字典的写锁必须跨 handler 共享，
// 否则同一字典的并发追加/上传会各自重建内容文件、互相覆盖。
func sharedManageService() *manageService {
	sharedManageOnce.Do(func() {
		sharedManage = &manageService{
			repo:        dictrepo.NewManageRepository(),
			entryRepo:   dictrepo.NewEntryRepository(),
			lines:       newLineCache(),
			nodeService: nodessvc.NewService(),
			locks:       newKeyedMutex(),
		}
	})
	return sharedManage
}

func NewManageService() ManageService {
	return sharedManageService()
}

func (s *manageService) List(ctx context.Context) ([]models.DictionaryMeta, error) {
	return s.repo.List(ctx)
}

// Create 历史接口：前端 category 是自由输入，能映射就映射到规范 kind，
// 映射不了就原样保留（老数据/自定义分类不能因为本次改造写不进去）。
func (s *manageService) Create(ctx context.Context, name, category string, content []byte) error {
	kind := category
	if canonical, err := NormalizeKind(category); err == nil {
		kind = canonical
	}
	safeName, err := SanitizeDictionaryName(name)
	if err != nil {
		return err
	}
	if kind == "" {
		return fmt.Errorf("%w: category is required", ErrInvalidKind)
	}
	values, err := normalizeContent(string(content))
	if err != nil {
		return err
	}

	exists, err := s.repo.ExistsByNameCategory(ctx, safeName, kind)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("duplication file name")
	}

	id, err := s.repo.InsertMeta(ctx, safeName, kind, "0.00")
	if err != nil {
		return err
	}
	meta, err := metaFromID(id, safeName, kind)
	if err != nil {
		return err
	}

	unlock := s.locks.Lock(id)
	defer unlock()
	if _, _, err := s.writeContentLocked(ctx, meta, values); err != nil {
		return err
	}
	return nil
}

func (s *manageService) Download(ctx context.Context, id string) (string, []byte, error) {
	data, err := s.repo.DownloadFile(ctx, id)
	if err == nil {
		return id, data, nil
	}

	// 内容文件缺失（历史库/重建中断）时，以行级集合为准补一次再读
	meta, metaErr := s.metaByID(ctx, id)
	if metaErr != nil {
		return "", nil, err
	}

	unlock := s.locks.Lock(id)
	if ensureErr := s.ensureEntriesLocked(ctx, meta); ensureErr != nil {
		unlock()
		return "", nil, ensureErr
	}
	// 行级集合已对齐但仍没有内容文件：按行重建一次
	if _, exists, lenErr := s.repo.FileLength(ctx, id); lenErr == nil && !exists {
		if _, _, rebuildErr := s.rebuildBlobLocked(ctx, meta); rebuildErr != nil {
			unlock()
			return "", nil, rebuildErr
		}
	}
	unlock()

	data, err = s.repo.DownloadFile(ctx, id)
	if err == nil {
		return id, data, nil
	}
	// 空字典（0 行）没有内容文件，按历史语义返回空内容
	if count, countErr := s.entryRepo.Count(ctx, id); countErr == nil && count == 0 {
		return id, []byte{}, nil
	}
	return "", nil, err
}

func (s *manageService) Delete(ctx context.Context, ids []string) error {
	if err := s.repo.DeleteMeta(ctx, ids); err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.entryRepo.DeleteAll(ctx, id); err != nil {
			return err
		}
		if err := s.repo.DeleteFileSafe(ctx, id); err != nil {
			return err
		}
		s.refreshNode(ctx, "delete", id)
	}
	return nil
}

func (s *manageService) Save(ctx context.Context, id string, content []byte) error {
	meta, err := s.metaByID(ctx, id)
	if err != nil {
		return err
	}
	values, err := normalizeContent(string(content))
	if err != nil {
		return err
	}

	unlock := s.locks.Lock(id)
	defer unlock()
	if _, _, err := s.writeContentLocked(ctx, meta, values); err != nil {
		return err
	}
	return nil
}

// metaByID 取元数据；不存在返回 ErrDictionaryNotFound。
func (s *manageService) metaByID(ctx context.Context, id string) (*models.DictionaryMeta, error) {
	meta, err := s.repo.FindMetaByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, fmt.Errorf("%w: id=%s", ErrDictionaryNotFound, id)
	}
	return meta, nil
}

// metaFromID 用已知 ID 构造内存元数据（新建流程里省一次查询）。
func metaFromID(id, name, category string) (*models.DictionaryMeta, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, fmt.Errorf("invalid dictionary id %q: %w", id, err)
	}
	return &models.DictionaryMeta{ID: oid, Name: name, Category: category, Size: "0.00"}, nil
}
