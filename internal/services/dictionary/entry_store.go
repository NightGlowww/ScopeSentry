// services-------------------------------------
// @file      : entry_store.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2026/11/18 02:40
// -------------------------------------------

package dictionary

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/mongo/gridfs"
)

const (
	// entryBatchSize 单批写入行数（与 repositories 层一致，2000 行/批）
	entryBatchSize = 2000
	// contentFlushSize 重建内容时的写缓冲大小
	contentFlushSize = 256 * 1024
)

// keyedMutex 按字典 ID 串行化写操作。
// 追加/删行都要重建 GridFS 内容，同一字典并发重建会互相覆盖，
// 所以这里用「一把锁一个字典」把写路径串起来（不同字典互不阻塞）。
type keyedMutex struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func newKeyedMutex() *keyedMutex {
	return &keyedMutex{locks: make(map[string]*sync.Mutex)}
}

// Lock 返回解锁函数，调用方 defer 即可。
func (k *keyedMutex) Lock(key string) func() {
	k.mu.Lock()
	l, ok := k.locks[key]
	if !ok {
		l = &sync.Mutex{}
		k.locks[key] = l
	}
	k.mu.Unlock()
	l.Lock()
	return l.Unlock
}

// metaByName 按 kind + name 取元数据，并做 kind/name 校验。
func (s *manageService) metaByName(ctx context.Context, kind, name string) (*models.DictionaryMeta, error) {
	canonical, err := NormalizeKind(kind)
	if err != nil {
		return nil, err
	}
	safeName, err := SanitizeDictionaryName(name)
	if err != nil {
		return nil, err
	}
	meta, err := s.repo.FindMetaByNameCategory(ctx, safeName, canonical)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, fmt.Errorf("%w: %s/%s", ErrDictionaryNotFound, canonical, safeName)
	}
	return meta, nil
}

// insertFromReader 从流里逐行入库（回填/大文件解析共用），O(1) 内存。
func (s *manageService) insertFromReader(ctx context.Context, meta *models.DictionaryMeta, r io.Reader) (int64, int64, error) {
	dictID := meta.ID.Hex()
	reader := bufio.NewReaderSize(r, 64*1024)
	batch := make([]string, 0, entryBatchSize)
	var added, duplicated int64

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		seqStart, err := s.repo.NextSeq(ctx, dictID, int64(len(batch)))
		if err != nil {
			return err
		}
		a, d, err := s.entryRepo.InsertBatch(ctx, dictID, meta.Category, seqStart, batch)
		if err != nil {
			return err
		}
		added += a
		duplicated += d
		batch = batch[:0]
		return nil
	}

	for {
		line, readErr := reader.ReadString('\n')
		if line != "" {
			if v, ok := NormalizeLine(line); ok && len(v) <= MaxLineLength {
				batch = append(batch, v)
				if len(batch) >= entryBatchSize {
					if err := flush(); err != nil {
						return added, duplicated, err
					}
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return added, duplicated, fmt.Errorf("read dictionary content: %w", readErr)
		}
	}
	if err := flush(); err != nil {
		return added, duplicated, err
	}
	return added, duplicated, nil
}

// insertValues 把一批已归一化的行写入（按批分配 seq）。
func (s *manageService) insertValues(ctx context.Context, meta *models.DictionaryMeta, values []string) (int64, int64, error) {
	dictID := meta.ID.Hex()
	var added, duplicated int64
	for start := 0; start < len(values); start += entryBatchSize {
		end := start + entryBatchSize
		if end > len(values) {
			end = len(values)
		}
		chunk := values[start:end]
		seqStart, err := s.repo.NextSeq(ctx, dictID, int64(len(chunk)))
		if err != nil {
			return added, duplicated, err
		}
		a, d, err := s.entryRepo.InsertBatch(ctx, dictID, meta.Category, seqStart, chunk)
		if err != nil {
			return added, duplicated, err
		}
		added += a
		duplicated += d
	}
	return added, duplicated, nil
}

// ensureEntriesLocked 保证行级集合与旧 GridFS 内容对齐（老数据首次行操作时回填一次）。
// 调用方必须已持有该字典的写锁。
func (s *manageService) ensureEntriesLocked(ctx context.Context, meta *models.DictionaryMeta) error {
	if meta.EntriesSynced {
		return nil
	}
	dictID := meta.ID.Hex()

	ds, err := s.repo.OpenFileStream(ctx, dictID)
	if err != nil {
		if !errors.Is(err, gridfs.ErrFileNotFound) {
			return err
		}
		// 没有内容文件：行级集合是唯一来源
		count, countErr := s.entryRepo.Count(ctx, dictID)
		if countErr != nil {
			return countErr
		}
		if count == 0 {
			// 空字典：只对齐标记，不动 updated_at
			if err := s.repo.MarkEntriesSynced(ctx, dictID, 0); err != nil {
				return err
			}
			meta.LineCount, meta.EntriesSynced = 0, true
			return nil
		}
		// 有行没内容：补一份内容文件
		lineCount, size, rebuildErr := s.rebuildBlobLocked(ctx, meta)
		if rebuildErr != nil {
			return rebuildErr
		}
		return s.persistStatsLocked(ctx, meta, lineCount, size)
	}
	defer ds.Close()

	// 内容文件是权威来源：先清空再重灌。
	// 上次回填如果中断在半路，残留行会被覆盖，不会出现"半份字典"。
	if err := s.entryRepo.DeleteAll(ctx, dictID); err != nil {
		return err
	}
	if _, _, err := s.insertFromReader(ctx, meta, ds); err != nil {
		return err
	}

	// 迁移只发生一次：回填后立刻按去重结果重建内容文件，
	// 保证「下载到的内容」与「行级集合」一致（老文件里可能有重复行）。
	lineCount, size, err := s.rebuildBlobLocked(ctx, meta)
	if err != nil {
		return err
	}
	return s.persistStatsLocked(ctx, meta, lineCount, size)
}

// rebuildBlobLocked 以行级集合为准重建 GridFS 内容（下载/节点拉取仍按文件走）。
// 流式读写，内存占用与字典大小无关；返回新的行数与大小字符串。
func (s *manageService) rebuildBlobLocked(ctx context.Context, meta *models.DictionaryMeta) (int64, string, error) {
	dictID := meta.ID.Hex()

	// 先清掉旧版本：GridFS 按 filename 允许同名多版本，
	// 每次追加都写一份而不删旧的话，200MB 字典会越写越胖。
	if err := s.repo.DeleteFileSafe(ctx, dictID); err != nil {
		return 0, "", err
	}

	cursor, err := s.entryRepo.Stream(ctx, dictID)
	if err != nil {
		return 0, "", err
	}
	defer cursor.Close(ctx)

	var count, bytes int64
	writeErr := s.repo.WriteFileStream(ctx, dictID, func(w io.Writer) error {
		bw := bufio.NewWriterSize(w, contentFlushSize)
		for cursor.Next(ctx) {
			var row struct {
				Value string `bson:"value"`
			}
			if err := cursor.Decode(&row); err != nil {
				return fmt.Errorf("decode entry: %w", err)
			}
			if _, err := bw.WriteString(row.Value); err != nil {
				return fmt.Errorf("write dictionary content: %w", err)
			}
			if err := bw.WriteByte('\n'); err != nil {
				return fmt.Errorf("write dictionary content: %w", err)
			}
			count++
			bytes += int64(len(row.Value)) + 1
		}
		if err := cursor.Err(); err != nil {
			return fmt.Errorf("iterate entries: %w", err)
		}
		if err := bw.Flush(); err != nil {
			return fmt.Errorf("flush dictionary content: %w", err)
		}
		return nil
	})
	if writeErr != nil {
		return 0, "", writeErr
	}
	if count == 0 {
		// 空字典不保留 0 字节文件，下载时按空内容返回
		if err := s.repo.DeleteFileSafe(ctx, dictID); err != nil {
			return 0, "", err
		}
		return 0, "0.00", nil
	}
	return count, fmt.Sprintf("%.2f", float64(bytes)/(1024*1024)), nil
}

// persistStatsLocked 落库行数/大小并对齐标记，然后通知节点刷新。
func (s *manageService) persistStatsLocked(ctx context.Context, meta *models.DictionaryMeta, lineCount int64, size string) error {
	dictID := meta.ID.Hex()
	if err := s.repo.UpdateMetaStats(ctx, dictID, size, lineCount, true); err != nil {
		return err
	}
	meta.LineCount = lineCount
	meta.Size = size
	meta.EntriesSynced = true
	s.refreshNode(ctx, "add", dictID)
	return nil
}

// refreshNode 通知节点字典变更；失败不影响主流程（与旧实现一致）。
func (s *manageService) refreshNode(ctx context.Context, action, dictID string) {
	_ = s.nodeService.RefreshConfig(ctx, models.Message{
		Name:    "all",
		Type:    "dictionary",
		Content: fmt.Sprintf("%s:%s", action, dictID),
	})
}

// writeContentLocked 整份覆盖：清空行 → 写入新行 → 重建内容。
func (s *manageService) writeContentLocked(ctx context.Context, meta *models.DictionaryMeta, values []string) (int64, string, error) {
	dictID := meta.ID.Hex()
	if err := s.entryRepo.DeleteAll(ctx, dictID); err != nil {
		return 0, "", err
	}
	if len(values) > 0 {
		if _, _, err := s.insertValues(ctx, meta, values); err != nil {
			return 0, "", err
		}
	}
	count, size, err := s.rebuildBlobLocked(ctx, meta)
	if err != nil {
		return 0, "", err
	}
	if err := s.persistStatsLocked(ctx, meta, count, size); err != nil {
		return 0, "", err
	}
	return count, size, nil
}

// normalizeContent 整份内容 → 归一化去重行；空内容返回空切片（表示清空）。
func normalizeContent(content string) ([]string, error) {
	raw := SplitContent(content)
	if len(raw) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		v, ok := NormalizeLine(line)
		if !ok {
			continue
		}
		if len(v) > MaxLineLength {
			return nil, fmt.Errorf("%w: %d > %d bytes", ErrLineTooLong, len(v), MaxLineLength)
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out, nil
}

// kindOfCategory 历史 category（可能是前端自定义字符串）→ 规范 kind。
func kindOfCategory(category string) string {
	if k, err := NormalizeKind(category); err == nil {
		return k
	}
	if strings.TrimSpace(category) == "" {
		return models.DictKindOther
	}
	return models.DictKindOther
}
