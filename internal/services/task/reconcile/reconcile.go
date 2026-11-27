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
	"github.com/Autumn-27/ScopeSentry/internal/services/task/taskstats"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type queueItem struct {
	ID string `json:"id"`
}

// RunOnce 执行一次对账，返回（排队中回退数, 孤儿回退数, 僵死收尾数）。
func RunOnce(ctx context.Context) (int, int, int, error) {
	if redisc.Client == nil || mongodb.DB == nil {
		return 0, 0, 0, nil
	}
	keys, err := redisc.Client.Keys(ctx, "node:*").Result()
	if err != nil {
		return 0, 0, 0, err
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

	// 只有心跳新鲜的节点才算「能把队列跑掉的节点」。
	// 不区分的话，队列挂在早已离线的节点上，任务就永远停在「待开始」——
	// 既没人领取、也不满足「僵死收尾」的前提（2026-11-28 实测 7 条任务卡在这里）。
	live := liveNodes(ctx, names)

	queued := map[primitive.ObjectID]struct{}{}
	current := map[primitive.ObjectID]struct{}{}
	for name := range names {
		if !live[name] {
			continue
		}
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
		// 用户主动停止的任务不能被「节点还声称在跑」翻回运行中
		if _, perr := col.UpdateMany(ctx, bson.M{"_id": bson.M{"$in": curIDs}, "status": bson.M{"$ne": 1},
			"stoppedByUser": bson.M{"$ne": true}}, bson.M{"$set": bson.M{
			"status": 1, "failReason": "",
		}}); perr != nil {
			logger.Error("taskreconcile: promote failed: " + perr.Error())
		}
	}

	keep := make([]primitive.ObjectID, 0, len(queued)+len(current))
	keepSet := make(map[primitive.ObjectID]struct{}, len(queued)+len(current))
	for id := range queued {
		keep = append(keep, id)
		keepSet[id] = struct{}{}
	}
	for id := range current {
		keep = append(keep, id)
		keepSet[id] = struct{}{}
	}
	// 用户主动停止的任务不回退成「待开始」——回退等于让它重新排队再跑一遍
	res, uerr := col.UpdateMany(ctx, bson.M{"status": 1, "_id": bson.M{"$nin": keep},
		"stoppedByUser": bson.M{"$ne": true}}, bson.M{"$set": bson.M{
		"status": 0, "failReason": "reconciled: 未在任何节点执行，状态回退为待开始",
	}})
	if uerr != nil {
		return demotedQueued, 0, 0, uerr
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

	// 终态收敛：没人在跑、不在队列、且有进度但久无写入的任务，按失败收尾。
	// 放在回退之后：先让状态与现实对齐，再判断谁彻底没救了。
	finalized, ferr := finalizeStaleTasks(ctx, keepSet)
	if ferr != nil {
		logger.Error("taskreconcile: finalize stale: " + ferr.Error())
	}

	if demotedQueued+demotedOrphan+finalized > 0 {
		logger.Info(fmt.Sprintf("taskreconcile: queued->pending=%d orphan->pending=%d stale->failed=%d (queued=%d current=%d)",
			demotedQueued, demotedOrphan, finalized, len(queued), len(current)))
	}
	return demotedQueued, demotedOrphan, finalized, nil
}

// Start 每 2 分钟对一次账。TASK_RECONCILE=off 关闭。
func Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(2 * time.Minute)
		defer t.Stop()
		for {
			if _, _, _, err := RunOnce(ctx); err != nil {
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

// ---------------------------------------------------------------------------
// 终态收敛（对齐 ARL-Next 1.5.2「终态统计补齐」）
//
// 现象（2026-11-28 线上实测）：多个任务进度卡在 82%~92%、状态却是「待开始」。
// 原因不是进度算错，而是它们**永远到不了终态**：
//   - 节点跑完一轮后不再声明 currentTask，对账把 status=1 回退成 0；
//   - 但「完成」只认「所有目标的所有模块都有 _end」，只要有一个模块的结束信号丢了
//     （节点重启、流水线中断、目标被并入子域后再无进度），进度就永远达不到 100%。
//
// 结果：任务既不算完成也不算失败，一直挂在「待开始」里，统计和列表都失真。
//
// 这里给的是一条**确定性收尾规则**：没有任何节点在跑、也不在队列里、
// 且最后一次进度写入已经超过宽限期 → 按失败收尾，并把「最后进度时间 + 百分比」写进 failReason。
// 不伪造「完成」：数据可能不完整，标成失败并说明原因才是诚实的。
const (
	// staleFinalizeGrace 最后进度写入后多久仍无人执行就收尾。
	// 取 30 分钟：单个模块（目录扫描/漏洞扫描）实测最长跑到 25 分钟，再短会误杀。
	staleFinalizeGrace = 30 * time.Minute
	// progressTimeLayout 节点写进 hash 的时间格式（本地时区）。
	progressTimeLayout = "2006-01-02 15:04:05"
)

// nodeStaleAfter 节点心跳停更多久算离线。与 watchdog.StaleAfter 取同一个数：
// 扫描器配置更新是 os.Exit(0) 重启，实测从退出到心跳恢复约 7 分钟，
// 阈值小于这个数会把「正在重启的节点」误判成离线。
const nodeStaleAfter = 15 * time.Minute

// liveNodes 判断哪些节点还活着（心跳 updateTime 在阈值内）。
// 读不到心跳/时间解析失败的节点一律按「不活」处理：宁可让任务收尾，
// 也不要让它永远挂在「待开始」里等一个可能再也不会回来的节点。
func liveNodes(ctx context.Context, names map[string]struct{}) map[string]bool {
	out := make(map[string]bool, len(names))
	now := time.Now()
	for name := range names {
		values, err := redisc.Client.HGetAll(ctx, "node:"+name).Result()
		if err != nil {
			continue
		}
		ts := strings.TrimSpace(values["updateTime"])
		if ts == "" {
			continue
		}
		parsed, perr := time.ParseInLocation(progressTimeLayout, ts, time.Local)
		if perr != nil {
			continue
		}
		out[name] = now.Sub(parsed) <= nodeStaleAfter
	}
	return out
}

// ShouldFinalizeStale 纯判断：最后一次进度写入距今是否已超过宽限期。
// lastEnd 为零值（没有任何进度信号）时返回 false——没有证据就不动它。
func ShouldFinalizeStale(lastEnd, now time.Time, grace time.Duration) bool {
	if lastEnd.IsZero() {
		return false
	}
	return now.Sub(lastEnd) > grace
}

// StaleFinalizeReason 生成收尾说明：把最后进度时间和百分比都写进去，
// 用户点开任务能直接看出「是跑到 92% 就没信号了」，而不是一句「失败」。
func StaleFinalizeReason(progress int, lastEnd time.Time) string {
	return fmt.Sprintf("reconciled: 节点已不在执行该任务，最后一次进度写入于 %s（进度 %d%%），按失败收尾",
		lastEnd.Format(progressTimeLayout), progress)
}

// lastProgressEnd 扫一个任务的逐目标进度 hash，取最大的 *_end 时间。
func lastProgressEnd(ctx context.Context, taskID string) time.Time {
	keys, err := redisc.Client.Keys(ctx, "TaskInfo:progress:"+taskID+":*").Result()
	if err != nil {
		return time.Time{}
	}
	var latest time.Time
	for _, key := range keys {
		fields, herr := redisc.Client.HGetAll(ctx, key).Result()
		if herr != nil {
			continue
		}
		for name, ts := range fields {
			if !strings.HasSuffix(name, "_end") || ts == "" {
				continue
			}
			parsed, perr := time.ParseInLocation(progressTimeLayout, ts, time.Local)
			if perr != nil {
				continue
			}
			if parsed.After(latest) {
				latest = parsed
			}
		}
	}
	return latest
}

// finalizeStaleTasks 把僵死任务收尾成失败，返回收尾数量。
// 只处理 status=0、有进度、非用户停止、且不在队列/不在执行中的任务。
func finalizeStaleTasks(ctx context.Context, keep map[primitive.ObjectID]struct{}) (int, error) {
	col := mongodb.DB.Collection("task")
	cur, err := col.Find(ctx, bson.M{
		"status":        0,
		"progress":      bson.M{"$gt": 0},
		"stoppedByUser": bson.M{"$ne": true},
	}, options.Find().SetProjection(bson.M{"_id": 1, "progress": 1, "name": 1}))
	if err != nil {
		return 0, err
	}
	defer func() { _ = cur.Close(ctx) }()

	now := time.Now()
	finalized := 0
	for cur.Next(ctx) {
		var row struct {
			ID       primitive.ObjectID `bson:"_id"`
			Progress int                `bson:"progress"`
			Name     string             `bson:"name"`
		}
		if derr := cur.Decode(&row); derr != nil {
			continue
		}
		if _, busy := keep[row.ID]; busy {
			continue
		}
		lastEnd := lastProgressEnd(ctx, row.ID.Hex())
		if !ShouldFinalizeStale(lastEnd, now, staleFinalizeGrace) {
			continue
		}
		// 收尾也是终态：冻结产出快照，列表「产出」列才不会永远显示「—」
		finalSet := bson.M{
			"status":     4,
			"failReason": StaleFinalizeReason(row.Progress, lastEnd),
		}
		finalSet = taskstats.Merge(finalSet, taskstats.Freeze(ctx, row.Name))
		res, uerr := col.UpdateOne(ctx, bson.M{"_id": row.ID, "status": 0}, bson.M{"$set": finalSet})
		if uerr != nil {
			logger.Error("taskreconcile: finalize stale failed: " + uerr.Error())
			continue
		}
		if res.ModifiedCount > 0 {
			finalized++
		}
	}
	return finalized, cur.Err()
}
