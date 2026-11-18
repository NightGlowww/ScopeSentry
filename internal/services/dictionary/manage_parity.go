// services-------------------------------------
// @file      : manage_parity.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2026/11/18 02:50
// -------------------------------------------

package dictionary

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
)

// ListPaged 字典列表：kind 过滤 + 名称子串搜索 + 分页。
func (s *manageService) ListPaged(ctx context.Context, kind, search string, pageIndex, pageSize int) (*models.DictionaryListResult, error) {
	pageIndex, pageSize, err := NormalizePage(pageIndex, pageSize)
	if err != nil {
		return nil, err
	}

	filter := bson.M{}
	if strings.TrimSpace(kind) != "" {
		canonical, err := NormalizeKind(kind)
		if err != nil {
			return nil, err
		}
		filter["category"] = canonical
	}
	search = strings.TrimSpace(search)
	if search != "" {
		if len(search) > MaxNameLength {
			return nil, fmt.Errorf("%w: search longer than %d bytes", ErrInvalidName, MaxNameLength)
		}
		filter["name"] = bson.M{"$regex": regexp.QuoteMeta(search), "$options": "i"}
	}

	total, err := s.repo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}
	skip := int64((pageIndex - 1) * pageSize)
	rows, err := s.repo.ListPaged(ctx, filter, skip, int64(pageSize))
	if err != nil {
		return nil, err
	}
	kindCounts, err := s.repo.CountByCategory(ctx)
	if err != nil {
		return nil, err
	}

	list := make([]models.DictionaryItem, 0, len(rows))
	for _, m := range rows {
		list = append(list, dictItem(m))
	}
	return &models.DictionaryListResult{
		List:      list,
		Total:     total,
		PageIndex: pageIndex,
		PageSize:  pageSize,
		Kinds:     mergeKinds(kindCounts),
	}, nil
}

// Preview 预览前 limit 行（默认/上限 200）。
func (s *manageService) Preview(ctx context.Context, kind, name string, limit int) (*models.DictionaryPreviewResult, error) {
	meta, err := s.metaByName(ctx, kind, name)
	if err != nil {
		return nil, err
	}
	if !meta.EntriesSynced {
		unlock := s.locks.Lock(meta.ID.Hex())
		err = s.ensureEntriesLocked(ctx, meta)
		unlock()
		if err != nil {
			return nil, err
		}
	}

	limit = ClampPreviewLimit(limit)
	rows, err := s.entryRepo.Preview(ctx, meta.ID.Hex(), limit)
	if err != nil {
		return nil, err
	}
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, row.Value)
	}

	total := meta.LineCount
	if total == 0 {
		n, err := s.entryRepo.Count(ctx, meta.ID.Hex())
		if err != nil {
			return nil, err
		}
		total = n
	}
	return &models.DictionaryPreviewResult{
		Kind:      meta.Category,
		Name:      meta.Name,
		Total:     total,
		Lines:     lines,
		Limit:     limit,
		Truncated: int64(len(lines)) < total,
	}, nil
}

// SearchLines 在单个字典里按子串搜行（默认 50 条，上限 200）。
func (s *manageService) SearchLines(ctx context.Context, kind, name, keyword string, limit int) (*models.DictionarySearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, ErrEmptyKeyword
	}
	if len(keyword) > 256 {
		return nil, fmt.Errorf("%w: keyword longer than 256 bytes", ErrEmptyKeyword)
	}
	meta, err := s.metaByName(ctx, kind, name)
	if err != nil {
		return nil, err
	}
	if !meta.EntriesSynced {
		unlock := s.locks.Lock(meta.ID.Hex())
		err = s.ensureEntriesLocked(ctx, meta)
		unlock()
		if err != nil {
			return nil, err
		}
	}

	limit = ClampSearchLimit(limit)
	rows, err := s.entryRepo.Search(ctx, meta.ID.Hex(), keyword, limit)
	if err != nil {
		return nil, err
	}
	matches := make([]models.DictionaryMatch, 0, len(rows))
	for _, row := range rows {
		matches = append(matches, models.DictionaryMatch{Value: row.Value, Seq: row.Seq})
	}
	return &models.DictionarySearchResult{
		Kind:      meta.Category,
		Name:      meta.Name,
		Keyword:   keyword,
		Matches:   matches,
		Returned:  len(matches),
		Limit:     limit,
		Truncated: len(matches) == limit,
	}, nil
}

// Append 追加行，已在字典里的行不会重复写入（唯一索引 (dict_id, value)）。
func (s *manageService) Append(ctx context.Context, kind, name string, lines []string) (*models.DictionaryAppendResult, error) {
	meta, err := s.metaByName(ctx, kind, name)
	if err != nil {
		return nil, err
	}
	values, err := NormalizeLines(lines, MaxAppendLines)
	if err != nil {
		return nil, err
	}

	unlock := s.locks.Lock(meta.ID.Hex())
	defer unlock()
	if err := s.ensureEntriesLocked(ctx, meta); err != nil {
		return nil, err
	}
	added, duplicated, err := s.insertValues(ctx, meta, values)
	if err != nil {
		return nil, err
	}
	count, size, err := s.rebuildBlobLocked(ctx, meta)
	if err != nil {
		return nil, err
	}
	if err := s.persistStatsLocked(ctx, meta, count, size); err != nil {
		return nil, err
	}

	return &models.DictionaryAppendResult{
		Kind:       meta.Category,
		Name:       meta.Name,
		Added:      added,
		Duplicated: duplicated,
		Total:      count,
	}, nil
}

// DeleteEntries 删除指定行。
func (s *manageService) DeleteEntries(ctx context.Context, kind, name string, entries []string) (*models.DictionaryDeleteEntriesResult, error) {
	meta, err := s.metaByName(ctx, kind, name)
	if err != nil {
		return nil, err
	}
	values, err := NormalizeLines(entries, MaxDeleteEntries)
	if err != nil {
		return nil, err
	}

	unlock := s.locks.Lock(meta.ID.Hex())
	defer unlock()
	if err := s.ensureEntriesLocked(ctx, meta); err != nil {
		return nil, err
	}
	deleted, err := s.entryRepo.DeleteValues(ctx, meta.ID.Hex(), values)
	if err != nil {
		return nil, err
	}
	count, size, err := s.rebuildBlobLocked(ctx, meta)
	if err != nil {
		return nil, err
	}
	if err := s.persistStatsLocked(ctx, meta, count, size); err != nil {
		return nil, err
	}

	return &models.DictionaryDeleteEntriesResult{
		Kind:    meta.Category,
		Name:    meta.Name,
		Deleted: deleted,
		Total:   count,
	}, nil
}

// DeleteFileByName 删除整个字典：元数据 + 行 + GridFS 内容。
func (s *manageService) DeleteFileByName(ctx context.Context, kind, name string) (*models.DictionaryDeleteFileResult, error) {
	meta, err := s.metaByName(ctx, kind, name)
	if err != nil {
		return nil, err
	}
	dictID := meta.ID.Hex()

	unlock := s.locks.Lock(dictID)
	if err := s.entryRepo.DeleteAll(ctx, dictID); err != nil {
		unlock()
		return nil, err
	}
	if err := s.repo.DeleteFileSafe(ctx, dictID); err != nil {
		unlock()
		return nil, err
	}
	if err := s.repo.DeleteMetaByNameCategory(ctx, meta.Name, meta.Category); err != nil {
		unlock()
		return nil, err
	}
	unlock()

	s.refreshNode(ctx, "delete", dictID)
	return &models.DictionaryDeleteFileResult{
		Kind:    meta.Category,
		Name:    meta.Name,
		Deleted: true,
	}, nil
}

// OpenDownload 流式下载（不把整份字典读进内存）。
func (s *manageService) OpenDownload(ctx context.Context, kind, name string) (io.ReadCloser, int64, string, error) {
	meta, err := s.metaByName(ctx, kind, name)
	if err != nil {
		return nil, 0, "", err
	}
	dictID := meta.ID.Hex()
	filename := DownloadFilename(meta.Category, meta.Name)

	length, exists, err := s.repo.FileLength(ctx, dictID)
	if err != nil {
		return nil, 0, "", err
	}
	if !exists {
		// 内容文件不在：用行级集合重建一次（历史库/上次重建中断）
		unlock := s.locks.Lock(dictID)
		rebuildErr := s.ensureEntriesLocked(ctx, meta)
		if rebuildErr == nil {
			_, _, rebuildErr = s.rebuildBlobLocked(ctx, meta)
		}
		unlock()
		if rebuildErr != nil {
			return nil, 0, "", rebuildErr
		}
		length, exists, err = s.repo.FileLength(ctx, dictID)
		if err != nil {
			return nil, 0, "", err
		}
		if !exists {
			// 空字典：返回空内容而不是 404
			return io.NopCloser(bytes.NewReader(nil)), 0, filename, nil
		}
	}

	ds, err := s.repo.OpenFileStream(ctx, dictID)
	if err != nil {
		return nil, 0, "", err
	}
	return ds, length, filename, nil
}

// CreateByKind 新建字典（重名报 ErrDictionaryExists）。
func (s *manageService) CreateByKind(ctx context.Context, kind, name, content string) (*models.DictionaryCreateResult, error) {
	canonical, err := NormalizeKind(kind)
	if err != nil {
		return nil, err
	}
	safeName, err := SanitizeDictionaryName(name)
	if err != nil {
		return nil, err
	}
	values, err := normalizeContent(content)
	if err != nil {
		return nil, err
	}

	existing, err := s.repo.FindMetaByNameCategory(ctx, safeName, canonical)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("%w: %s/%s", ErrDictionaryExists, canonical, safeName)
	}

	id, err := s.repo.InsertMeta(ctx, safeName, canonical, "0.00")
	if err != nil {
		return nil, err
	}
	meta, err := metaFromID(id, safeName, canonical)
	if err != nil {
		return nil, err
	}

	unlock := s.locks.Lock(id)
	defer unlock()
	count, size, err := s.writeContentLocked(ctx, meta, values)
	if err != nil {
		return nil, err
	}
	return &models.DictionaryCreateResult{
		ID:        id,
		Kind:      canonical,
		Name:      safeName,
		LineCount: count,
		Size:      size,
		Replaced:  false,
	}, nil
}

// SaveByKind dict_cdn / dict_brute 的 save：存在就整份覆盖，不存在就新建。
func (s *manageService) SaveByKind(ctx context.Context, kind, name, content string, lines []string) (*models.DictionaryCreateResult, error) {
	canonical, err := NormalizeKind(kind)
	if err != nil {
		return nil, err
	}
	safeName, err := SanitizeDictionaryName(name)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(content) == "" && len(lines) > 0 {
		content = strings.Join(lines, "\n")
	}
	values, err := normalizeContent(content)
	if err != nil {
		return nil, err
	}

	existing, err := s.repo.FindMetaByNameCategory(ctx, safeName, canonical)
	if err != nil {
		return nil, err
	}

	replaced := existing != nil
	meta := existing
	if meta == nil {
		id, err := s.repo.InsertMeta(ctx, safeName, canonical, "0.00")
		if err != nil {
			return nil, err
		}
		meta, err = metaFromID(id, safeName, canonical)
		if err != nil {
			return nil, err
		}
	}

	unlock := s.locks.Lock(meta.ID.Hex())
	defer unlock()
	count, size, err := s.writeContentLocked(ctx, meta, values)
	if err != nil {
		return nil, err
	}
	return &models.DictionaryCreateResult{
		ID:        meta.ID.Hex(),
		Kind:      canonical,
		Name:      safeName,
		LineCount: count,
		Size:      size,
		Replaced:  replaced,
	}, nil
}

// dictItem 元数据 → 列表行。
func dictItem(m models.DictionaryMeta) models.DictionaryItem {
	return models.DictionaryItem{
		ID:        m.ID.Hex(),
		Name:      m.Name,
		Kind:      kindOfCategory(m.Category),
		Category:  m.Category,
		Size:      m.Size,
		LineCount: m.LineCount,
		UpdatedAt: m.UpdatedAt,
	}
}

// mergeKinds 保证 5 个规范 kind 都出现在 kind 面板里（没有字典也返回 0），
// 历史自定义 category 原样附在后面，避免老数据在列表里"看不见"。
func mergeKinds(counts []models.DictionaryKindCount) []models.DictionaryKindCount {
	index := make(map[string]int64, len(counts))
	for _, c := range counts {
		index[c.Kind] = c.Count
	}
	out := make([]models.DictionaryKindCount, 0, len(models.DictionaryKinds)+len(counts))
	known := make(map[string]struct{}, len(models.DictionaryKinds))
	for _, k := range models.DictionaryKinds {
		out = append(out, models.DictionaryKindCount{Kind: k, Count: index[k]})
		known[k] = struct{}{}
	}
	for _, c := range counts {
		if _, ok := known[c.Kind]; ok {
			continue
		}
		out = append(out, c)
	}
	return out
}

// BackfillEntryIndex 为历史（仅文件、未建行索引）的字典补建行级索引。
//
// 背景：lineCount 存在 meta 上，只有 entries_synced=true 之后才有值；
// 老版本只写文件不建索引，列表里会一直显示 0 行。Preview/Search/Append 会懒同步，
// 但没人点就一直不对，所以启动时补一次（失败不影响服务启动）。
func (s *manageService) BackfillEntryIndex(ctx context.Context) (int, error) {
	done := 0
	for {
		metas, err := s.repo.ListPaged(ctx, bson.M{"entries_synced": bson.M{"$ne": true}}, 0, 20)
		if err != nil {
			return done, err
		}
		if len(metas) == 0 {
			return done, nil
		}
		for i := range metas {
			m := &metas[i]
			unlock := s.locks.Lock(m.ID.Hex())
			err := s.ensureEntriesLocked(ctx, m)
			unlock()
			if err != nil {
				return done, err
			}
			done++
		}
	}
}
