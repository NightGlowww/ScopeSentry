package system

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

// StorageCollection 单个集合的占用
type StorageCollection struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
	Docs  int64  `json:"docs"`
}

// StorageReport 存储报告：回答「库为什么这么大、备份为什么这么久」。
// 2026-11-28 实测：SensitiveBody 18.3GB / 1.9 万条（平均近 1MB 一条响应体），
// 其余集合加起来不到 1GB —— 备份 3.3GB/9min 的账基本都记在它头上。
type StorageReport struct {
	Total       int64               `json:"total"`
	Collections []StorageCollection `json:"collections"`
	CollectedAt string              `json:"collectedAt"`
}

var (
	storageMu     sync.Mutex
	storageCache  *StorageReport
	storageCached time.Time
)

// storageTTL 让这个偏重的统计（每个集合一次 collStats）不至于被页面反复触发。
const storageTTL = 60 * time.Second

// StorageStats 返回各集合占用（按字节降序，最多 limit 个）。
func StorageStats(ctx context.Context, limit int) (*StorageReport, error) {
	if mongodb.DB == nil {
		return nil, context.Canceled
	}
	storageMu.Lock()
	defer storageMu.Unlock()
	if storageCache != nil && time.Since(storageCached) < storageTTL {
		return storageCache, nil
	}

	names, err := mongodb.DB.ListCollectionNames(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	out := make([]StorageCollection, 0, len(names))
	var total int64
	for _, name := range names {
		var st struct {
			Size  int64 `bson:"size"`
			Count int64 `bson:"count"`
		}
		err := mongodb.DB.RunCommand(ctx, bson.D{{"collStats", name}}).Decode(&st)
		if err != nil {
			continue
		}
		out = append(out, StorageCollection{Name: name, Bytes: st.Size, Docs: st.Count})
		total += st.Size
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Bytes == out[j].Bytes {
			return out[i].Name < out[j].Name
		}
		return out[i].Bytes > out[j].Bytes
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	rep := &StorageReport{Total: total, Collections: out, CollectedAt: time.Now().Format(time.RFC3339)}
	storageCache = rep
	storageCached = time.Now()
	return rep, nil
}

// Storage 处理 GET /api/system/storage。
func Storage(c *gin.Context) {
	rep, err := StorageStats(c.Request.Context(), 8)
	if err != nil {
		c.JSON(500, gin.H{"code": 500, "message": "api.error", "data": err.Error()})
		return
	}
	c.JSON(200, gin.H{"code": 200, "message": "api.success", "data": rep})
}
