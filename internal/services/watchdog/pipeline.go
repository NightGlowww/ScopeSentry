package watchdog

// 流水线断流告警 + 僵尸节点自愈。
//
// 起因（2026-11-26）：漏洞集合两天没有新记录，而任务一直在跑。
// 当时没有任何机制提示「扫描器在跑但没产出」，是靠人问「怎么就这点资产」才发现的。
// 这个循环每 30 分钟检查三条最关键的信号，把结论写进 system_alerts，
// 由 /api/system/quality-report 一并返回，界面和运维脚本都能看到。
//
// 三类告警：
//   1. tasks_stalled —— 队列里有任务、但 2 小时内一个都没完成（节点卡住/看门狗循环重启）
//   2. no_findings   —— 6 小时内完成了 >=3 个任务，却一条新漏洞都没有（链路某段断了）
//   3. node_zombie   —— 节点心跳停更但 currentTask 还挂着：整个队列被一个死节点堵住
//
// 第 3 条不只是告警，还会**自动释放**：清掉 currentTask、把任务标记为失败。
// 2026-11-26 实测：扫描器跑完一个目标后卡死（心跳停在 20:15，进程还在占 40% CPU），
// currentTask 一直挂着，队列里 32 个任务一动不动，而 /api/system/config-check 仍报
// nodes=1 ok —— 因为那里只数 key 存不存在，不看 updateTime。僵尸节点不释放，队列就永远不流动。

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/database/redis"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func ts(t time.Time) string { return t.Format("2006-01-02 15:04:05") }

// zombieAfter 心跳停更多久算僵尸。
// 扫描器每 30s 左右更新一次 updateTime；给到 6 分钟是为了容忍一次长模块切换
// （实测单模块可能跑 18 分钟，但心跳是独立协程写的，不该断）。
const zombieAfter = 6 * time.Minute

// Start 每 30 分钟检查一次；用 PIPELINE_WATCHDOG=off 关闭。
func Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(30 * time.Minute)
		defer t.Stop()
		for {
			_, _ = Check(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

// Check 跑一轮检查，返回是否可以立即观察到的结果（给运维接口用）。
func Check(ctx context.Context) (map[string]interface{}, error) {
	if mongodb.DB == nil {
		return nil, fmt.Errorf("db not ready")
	}
	now := time.Now()
	task := mongodb.DB.Collection("task")
	vuln := mongodb.DB.Collection("vulnerability")
	alerts := mongodb.DB.Collection("system_alerts")

	// 1) 任务停滞：队列非空但 2h 无完成
	queued := 0
	if redis.Client != nil {
		if keys, err := redis.Client.Keys(ctx, "NodeTask:*").Result(); err == nil {
			for _, k := range keys {
				if n, err := redis.Client.LLen(ctx, k).Result(); err == nil {
					queued += int(n)
				}
			}
		}
	}
	finished2h, _ := task.CountDocuments(ctx, bson.M{
		"endTime": bson.M{"$gte": ts(now.Add(-2 * time.Hour))},
		"status":  bson.M{"$in": []int{3, 4}},
	})
	busy := false
	if redis.Client != nil {
		if keys, err := redis.Client.Keys(ctx, "node:*:currentTask").Result(); err == nil {
			for _, k := range keys {
				if v, err := redis.Client.Get(ctx, k).Result(); err == nil && v != "" {
					busy = true
				}
			}
		}
	}
	setAlert(ctx, alerts, "tasks_stalled", queued > 0 && !busy && finished2h == 0,
		"队列里有 "+itoa(queued)+" 个任务，但没有任何节点在跑、且 2 小时内无任务完成——调度或链路卡住了")

	// 2) 无产出：6h 完成 >=3 个任务但 0 新漏洞
	finished6h, _ := task.CountDocuments(ctx, bson.M{
		"endTime": bson.M{"$gte": ts(now.Add(-6 * time.Hour))},
		"status":  bson.M{"$in": []int{3, 4}},
	})
	new6h, _ := vuln.CountDocuments(ctx, bson.M{"time": bson.M{"$gte": ts(now.Add(-6 * time.Hour))}})
	setAlert(ctx, alerts, "no_findings", finished6h >= 3 && new6h == 0,
		"6 小时内完成了 "+itoa(int(finished6h))+" 个任务，但没有任何新漏洞——检查 WebCrawler/DirScan/VulnerabilityScan 是否在链路里")

	// 3) 僵尸节点：心跳停更 + currentTask 挂着 → 释放队列
	zombies := reconcileZombieNodes(ctx, task, queued)
	setAlert(ctx, alerts, "node_zombie", len(zombies) > 0,
		"有 "+itoa(len(zombies))+" 个节点心跳停更却还占着任务，已释放："+strings.Join(zombies, "；"))

	return map[string]interface{}{
		"queued":  queued,
		"busy":    busy,
		"zombies": zombies,
	}, nil
}

// reconcileZombieNodes 找出心跳停更的节点，清掉它的 currentTask 并把对应任务标记失败，
// 让队列继续流动。返回处理过的节点描述（供告警文案使用）。
func reconcileZombieNodes(ctx context.Context, task *mongo.Collection, queued int) []string {
	out := []string{}
	if redis.Client == nil {
		return out
	}
	keys, err := redis.Client.Keys(ctx, "node:*").Result()
	if err != nil {
		return out
	}
	now := time.Now()
	for _, k := range keys {
		if strings.HasSuffix(k, ":currentTask") {
			continue
		}
		// 只认 node:<name> 这种 hash（node:<name>:currentTask 是 string）
		if t, terr := redis.Client.Type(ctx, k).Result(); terr != nil || t != "hash" {
			continue
		}
		name := strings.TrimPrefix(k, "node:")
		upd, herr := redis.Client.HGet(ctx, k, "updateTime").Result()
		if herr != nil || upd == "" {
			continue
		}
		last, perr := time.ParseInLocation("2006-01-02 15:04:05", upd, time.Local)
		if perr != nil || now.Sub(last) < zombieAfter {
			continue
		}
		// 心跳停了这么久，还挂着任务就是僵尸。队列空的时候不必动它：
		// 没有别的活要干，留着 currentTask 让人看见「它当时在跑什么」更有用。
		taskKey := "node:" + name + ":currentTask"
		cur, cerr := redis.Client.Get(ctx, taskKey).Result()
		if cerr != nil || cur == "" {
			continue
		}
		if queued == 0 {
			continue
		}
		_ = redis.Client.Del(ctx, taskKey)
		reason := fmt.Sprintf("节点 %s 心跳停在 %s（超过 %s 未更新），已自动释放队列；该任务判定为失败，可 retest 重跑",
			name, upd, zombieAfter)
		if oid, e := primitive.ObjectIDFromHex(cur); e == nil {
			_, _ = task.UpdateOne(ctx, bson.M{"_id": oid, "status": 1}, bson.M{
				"$set": bson.M{"status": 4, "failReason": reason, "endTime": ts(now)},
			})
		}
		logger.Warn("watchdog: " + reason)
		out = append(out, name+"("+cur+")")
	}
	return out
}

// setAlert 以 key 为唯一键 upsert：firing=true 时记录首次触发时间并累计次数，
// firing=false 时标记为已恢复（保留历史，便于回看「什么时候断的」）。
func setAlert(ctx context.Context, coll *mongo.Collection, key string, firing bool, msg string) {
	now := ts(time.Now())
	if firing {
		_, err := coll.UpdateOne(ctx, bson.M{"_id": key}, bson.M{
			"$set":         bson.M{"key": key, "message": msg, "active": true, "lastSeenAt": now},
			"$setOnInsert": bson.M{"firstSeenAt": now},
			"$inc":         bson.M{"count": 1},
		}, options.Update().SetUpsert(true))
		if err == nil {
			logger.Warn("pipeline alert: " + key + " — " + msg)
		}
		return
	}
	_, _ = coll.UpdateOne(ctx, bson.M{"_id": key, "active": true}, bson.M{
		"$set": bson.M{"active": false, "recoveredAt": now},
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte(48 + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}
