package watchdog

// 流水线断流告警。
//
// 起因（2026-11-26）：漏洞集合两天没有新记录，而任务一直在跑。
// 当时没有任何机制提示「扫描器在跑但没产出」，是靠人问「怎么就这点资产」才发现的。
// 这个循环每 30 分钟检查两条最关键的断流信号，把结论写进 system_alerts，
// 由 /api/system/quality-report 一并返回，界面和运维脚本都能看到。
//
// 两类告警：
//   1. tasks_stalled —— 队列里有任务、但 2 小时内一个都没完成（节点卡住/看门狗循环重启）
//   2. no_findings   —— 6 小时内完成了 >=3 个任务，却一条新漏洞都没有（链路某段断了）

import (
	"context"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/database/redis"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func ts(t time.Time) string { return t.Format("2006-01-02 15:04:05") }

// Start 每 30 分钟检查一次；用 PIPELINE_WATCHDOG=off 关闭。
func Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(30 * time.Minute)
		defer t.Stop()
		for {
			check(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

func check(ctx context.Context) {
	if mongodb.DB == nil {
		return
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
	// 「有任务在跑」不算停滞：大目标本来就可能跑几小时。
	// 真正的停滞是「队列有货 + 所有节点空闲」——那才是调度或链路卡住。
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
