package dictionary

import (
	"context"
	"strings"
	"sync"

	dictrepo "github.com/Autumn-27/ScopeSentry/internal/repositories/dictionary"
)

// lineCache 字典行进程内缓存。
//
// 背景：dictionary_entry 的搜索是不区分大小写的子串匹配 + seq 排序，
// 这两个条件用不上同一个索引（(dict_id,value) 与 (dict_id,seq) 各一个），
// 2 万行的字典一次搜索 60-80ms。字典是「上传后基本不变」的数据，
// 缓存全部行后在 Go 里做子串过滤是亚毫秒级，且消灭了 Mongo 侧的键扫描。
//
// 失效：Append / DeleteEntries / DeleteFileByName / SaveByKind 都会调
// invalidate；进程重启自然冷启动。
type lineCache struct {
	mu    sync.RWMutex
	lines map[string]cacheEntry
}

type cacheEntry struct {
	values []string
	seqs   []int64
}

// maxCacheLines 单个字典缓存上限；超过就绕过缓存（仍走原 Mongo 查询，
// 防止一个异常大的字典把进程内存吃掉）。
const maxCacheLines = 500_000

func newLineCache() *lineCache {
	return &lineCache{lines: make(map[string]cacheEntry)}
}

// load 返回 (values, seqs, ok)：ok=false 表示字典过大未缓存，调用方走原路径。
func (c *lineCache) load(ctx context.Context, repo dictrepo.EntryRepository, dictID string) ([]string, []int64, bool, error) {
	c.mu.RLock()
	if e, ok := c.lines[dictID]; ok {
		c.mu.RUnlock()
		return e.values, e.seqs, true, nil
	}
	c.mu.RUnlock()

	values, seqs, err := loadAllLines(ctx, repo, dictID)
	if err != nil {
		return nil, nil, false, err
	}
	if len(values) > maxCacheLines {
		return nil, nil, false, nil
	}
	c.mu.Lock()
	c.lines[dictID] = cacheEntry{values: values, seqs: seqs}
	c.mu.Unlock()
	return values, seqs, true, nil
}

func (c *lineCache) invalidate(dictID string) {
	c.mu.Lock()
	delete(c.lines, dictID)
	c.mu.Unlock()
}

func loadAllLines(ctx context.Context, repo dictrepo.EntryRepository, dictID string) ([]string, []int64, error) {
	cursor, err := repo.Stream(ctx, dictID)
	if err != nil {
		return nil, nil, err
	}
	defer cursor.Close(ctx)
	values := make([]string, 0, 4096)
	seqs := make([]int64, 0, 4096)
	for cursor.Next(ctx) {
		var row struct {
			Value string `bson:"value"`
			Seq   int64  `bson:"seq"`
		}
		if err := cursor.Decode(&row); err != nil {
			return nil, nil, err
		}
		values = append(values, row.Value)
		seqs = append(seqs, row.Seq)
	}
	if err := cursor.Err(); err != nil {
		return nil, nil, err
	}
	return values, seqs, nil
}

// filterLines 大小写不敏感子串过滤（纯函数，便于测试）。
func filterLines(values []string, seqs []int64, keyword string, limit int) []dictMatch {
	kw := strings.ToLower(keyword)
	out := make([]dictMatch, 0, 64)
	for i, v := range values {
		if strings.Contains(strings.ToLower(v), kw) {
			out = append(out, dictMatch{Value: v, Seq: seqs[i]})
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

type dictMatch struct {
	Value string
	Seq   int64
}
