package common

import (
	"context"
	"sync"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"go.mongodb.org/mongo-driver/bson"
)

// 调度拒绝计数：四道闸各自拒了多少次（进程内，重启归零）。
// 同时落一条到 dispatch_rejections，用于回答「昨天下午是不是一直在撞闸」——
// 计数只能回答「现在多少」（2026-11-20）。
var (
	rejectMu     sync.Mutex
	rejectCounts = map[string]int64{}
)

const (
	RejectQueue      = "queue_full"
	RejectMemory     = "container_mem"
	RejectPrefix     = "prefix_quota"
	RejectRateLimit  = "rate_limited"
	RejectNameExists = "name_exists"

	RejectCollection = "dispatch_rejections"
)

func bumpReject(kind string) {
	rejectMu.Lock()
	rejectCounts[kind]++
	rejectMu.Unlock()
}

// 同一个任务在极短时间内被同一条闸门拒两次（实测 23ms 内两条），
// 说明调用链上有重复尝试；计数和记录都只保留第一次，避免数字虚高。
var (
	lastRejectMu   sync.Mutex
	lastRejectSeen = map[string]time.Time{}
)

func shouldRecord(key string, window time.Duration) bool {
	lastRejectMu.Lock()
	defer lastRejectMu.Unlock()
	now := time.Now()
	if t, ok := lastRejectSeen[key]; ok && now.Sub(t) < window {
		return false
	}
	lastRejectSeen[key] = now
	// 顺手清掉过期项，避免长期运行后 map 无限增长
	if len(lastRejectSeen) > 512 {
		for k, t := range lastRejectSeen {
			if now.Sub(t) > window {
				delete(lastRejectSeen, k)
			}
		}
	}
	return true
}

// noteReject 一次拒绝：计数 + 落库（异步，失败只记日志，不影响派发主流程）。
func noteReject(kind, node, taskName, detail string) {
	// 2 秒内同一 (闸门, 节点, 任务) 只记一次
	if !shouldRecord(kind+"|"+node+"|"+taskName, 2*time.Second) {
		return
	}
	bumpReject(kind)
	go func() {
		defer func() { _ = recover() }()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if mongodb.DB == nil {
			return
		}
		_, err := mongodb.DB.Collection(RejectCollection).InsertOne(ctx, bson.M{
			"at":       time.Now(),
			"gate":     kind,
			"node":     node,
			"taskName": taskName,
			"detail":   detail,
		})
		if err != nil {
			logger.Warn("record dispatch rejection failed: " + err.Error())
		}
	}()
}

// RejectStats 返回各闸门的拒绝次数（进程内累计）。
func RejectStats() map[string]int64 {
	rejectMu.Lock()
	defer rejectMu.Unlock()
	out := make(map[string]int64, len(rejectCounts))
	for k, v := range rejectCounts {
		out[k] = v
	}
	return out
}

// RecentRejections 返回最近 N 条拒绝记录（持久化，跨重启可查）。
func RecentRejections(limit int64) ([]bson.M, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cur, err := mongodb.DB.Collection(RejectCollection).Find(ctx, bson.M{},
		// 按时间倒序取最近 N 条
		//nolint:govet
		mongoFindOptions(limit),
	)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []bson.M
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}
