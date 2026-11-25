// Package reconcile 让任务状态和现实对齐。
//
// 背景（2026-11-25 实测）：库里 33 个任务标记「运行中」，节点日志显示只有 1 个真的在跑。
// 原因是「运行中」被当成一个可以从别处推断的字段——自动恢复、重排、节点重启都会写它，
// 但没有任何东西在事后把它改回去，于是排队中的、孤儿状态的都顶着「运行中」。
//
// 对账规则（只改 status=1 的记录）：
//  1. 在某个节点 NodeTask 队列里 → 还没开始，回「待开始」(0)；
//  2. 既不在队列、也不是任何节点的 currentTask → 孤儿，回「待开始」(0) 并写明原因；
//  3. 是某个节点的 currentTask → 保持运行中。
//
// currentTask 由扫描端在 Task begin 时写入 node:<节点名>:currentTask，任务结束时清空。
package reconcile

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	redisc "github.com/Autumn-27/ScopeSentry/internal/database/redis"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type queueItem struct {
	ID string `json:"id"`
}

// RunOnce 执行一次对账，返回（排队中回退数, 孤儿回退数）。
func RunOnce(ctx context.Context) (int, int, error) {
	if redisc.Client == nil || mongodb.DB == nil {
		return 0, 0, nil
	}
	keys, err := redisc.Client.Keys(ctx, "node:*").Result()
	if err != nil {
		return 0, 0, err
	}
	names := map[string]struct{}{}
	for _, k := range keys {
		rest := strings.TrimPrefix(k, "node:")
		if i := strings.Index(rest, ":"); i > 0 {
			rest = rest[:i]
		}
		if rest != "" {
			names[rest] = struct{}{}
		}
	}

	queued := map[primitive.ObjectID]struct{}{}
	current := map[primitive.ObjectID]struct{}{}
	for name := range names {
		items, lerr := redisc.Client.LRange(ctx, "NodeTask:"+name, 0, -1).Result()
		if lerr == nil {
			for _, raw := range items {
				var q queueItem
				if json.Unmarshal([]byte(raw), &q) == nil && q.ID != "" {
					if oid, oerr := primitive.ObjectIDFromHex(q.ID); oerr == nil {
						queued[oid] = struct{}{}
					}
				}
			}
		}
		if cur, cerr := redisc.Client.Get(ctx, "node:"+name+":currentTask").Result(); cerr == nil && cur != "" {
			if oid, oerr := primitive.ObjectIDFromHex(cur); oerr == nil {
				current[oid] = struct{}{}
			}
		}
	}

	col := mongodb.DB.Collection("task")
	demotedQueued, demotedOrphan := 0, 0
	if len(queued) > 0 {
		ids := make([]primitive.ObjectID, 0, len(queued))
		for id := range queued {
			if _, running := current[id]; !running {
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			res, uerr := col.UpdateMany(ctx, bson.M{"_id": bson.M{"$in": ids}, "status": 1}, bson.M{"$set": bson.M{
				"status": 0, "failReason": "reconciled: 已排队但未被任何节点领取",
			}})
			if uerr == nil {
				demotedQueued = int(res.ModifiedCount)
			}
		}
	}

	// 反向：节点声明了 currentTask 但状态不是运行中 → 提升为运行中。
	// 这样「运行中」只有一个来源：节点的 currentTask 声明。
	if len(current) > 0 {
		curIDs := make([]primitive.ObjectID, 0, len(current))
		for id := range current {
			curIDs = append(curIDs, id)
		}
		if _, perr := col.UpdateMany(ctx, bson.M{"_id": bson.M{"$in": curIDs}, "status": bson.M{"$ne": 1}}, bson.M{"$set": bson.M{
			"status": 1, "failReason": "",
		}}); perr != nil {
			logger.Error("taskreconcile: promote failed: " + perr.Error())
		}
	}

	keep := make([]primitive.ObjectID, 0, len(queued)+len(current))
	for id := range queued {
		keep = append(keep, id)
	}
	for id := range current {
		keep = append(keep, id)
	}
	res, uerr := col.UpdateMany(ctx, bson.M{"status": 1, "_id": bson.M{"$nin": keep}}, bson.M{"$set": bson.M{
		"status": 0, "failReason": "reconciled: 未在任何节点执行，状态回退为待开始",
	}})
	if uerr != nil {
		return demotedQueued, 0, uerr
	}
	demotedOrphan = int(res.ModifiedCount)

	// 关键：清掉被回退任务的进度临时键。
	// TaskProgress 看到 TaskInfo:tmp:<id> 存在就认为「节点在写进度」，把待开始的任务翻成运行中——
	// 这正是「33 个运行中、实际 1 个在跑」反复复发的真正来源（2026-11-25 定位）。
	for id := range queued {
		if _, running := current[id]; running {
			continue
		}
		_ = redisc.Client.Del(ctx, "TaskInfo:tmp:"+id.Hex(), "TaskInfo:time:"+id.Hex()).Err()
	}

	if demotedQueued+demotedOrphan > 0 {
		logger.Info(fmt.Sprintf("taskreconcile: queued->pending=%d orphan->pending=%d (queued=%d current=%d)",
			demotedQueued, demotedOrphan, len(queued), len(current)))
	}
	return demotedQueued, demotedOrphan, nil
}

// Start 每 2 分钟对一次账。TASK_RECONCILE=off 关闭。
func Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(2 * time.Minute)
		defer t.Stop()
		for {
			if _, _, err := RunOnce(ctx); err != nil {
				logger.Error("taskreconcile: " + err.Error())
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}
