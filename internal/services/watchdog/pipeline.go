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
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
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

// StaleAfter 心跳停更多久算「节点不新鲜」。
//
// 别把阈值调小：扫描器收到配置更新时是 os.Exit(0) 让容器重启来生效的
// （internal/configupdater），而重启后要重新加载 14438 个 POC、字典、WebFinger 等，
// 2026-11-26 实测从进程退出到心跳恢复用了约 7 分钟。阈值小于这个数，
// 每次「服务端一部署 → 节点重启」都会被误判成僵尸，把正在跑的任务标成失败。
// 15 分钟留了一倍余量，同时又比「等到人发现」快得多。
const StaleAfter = 15 * time.Minute

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
	// 「完成」不能把过期排队的任务算进去：它们不是跑完的，是被队列清理标记的。
	// 否则 watchdow 自己的 no_findings 会被自己的清理动作触发（2026-11-26 实测 27 条）。
	notExpired := bson.M{"$not": primitive.Regex{Pattern: "^排队超过"}}
	finished2h, _ := task.CountDocuments(ctx, bson.M{
		"endTime":    bson.M{"$gte": ts(now.Add(-2 * time.Hour))},
		"status":     bson.M{"$in": []int{3, 4}},
		"failReason": notExpired,
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
		"endTime":    bson.M{"$gte": ts(now.Add(-6 * time.Hour))},
		"status":     bson.M{"$in": []int{3, 4}},
		"failReason": notExpired,
	})
	new6h, _ := vuln.CountDocuments(ctx, bson.M{"time": bson.M{"$gte": ts(now.Add(-6 * time.Hour))}})
	setAlert(ctx, alerts, "no_findings", finished6h >= 3 && new6h == 0,
		"6 小时内完成了 "+itoa(int(finished6h))+" 个任务，但没有任何新漏洞——检查 WebCrawler/DirScan/VulnerabilityScan 是否在链路里")

	// 3) 僵尸节点：心跳停更 + currentTask 挂着 → 释放队列
	zombies := reconcileZombieNodes(ctx, task, queued)
	setAlert(ctx, alerts, "node_zombie", len(zombies) > 0,
		"有 "+itoa(len(zombies))+" 个节点心跳停更却还占着任务，已释放："+strings.Join(zombies, "；"))

	// 4) 排队过久的任务：等着的任务里有一半是几天前的，新任务永远轮不上。
	//
	// 2026-11-26 实测：队列 33 个任务，最老的是 11-19（等了 7 天），按单个任务
	// 20~40 分钟算，光这批就要 11~22 小时；再叠上持续新建的任务，队列只进不出。
	// 处理办法是把「超过窗口还没开始」的排队意图过期掉（任务记录保留，可 retest），
	// 并把它的队列项一起摘掉——只改 status 不动 Redis，节点照样会把它捞起来跑。
	expired := expireStaleQueued(ctx, task)
	setAlert(ctx, alerts, "stale_queue", len(expired) > 0,
		"有 "+itoa(len(expired))+" 个任务排队超过 "+queueMaxAge().String()+" 仍未开始，已过期并移出队列")

	// 5) 幽灵运行中：任务是 status=1，但没有任何节点在跑它、也不在任何队列里。
	//
	// 2026-11-26 实测：扫描器跑完一个目标后没能把「任务结束」发出去，进程被配置更新
	// 重启；重启后节点从队列取下一个，那个任务就永远挂在 status=1 —— 既没人跑，
	// 也不会被 tasks_stalled 抓到（队列里确实有别的任务在动）。
	// 判断依据用进度写入时间：Redis 里 TaskInfo:progress:<taskId>:* 的字段值就是时间戳，
	// 超过 phantomAfter 没有新写入，再加上「既非 currentTask 也不在队列」，才能断定没人会跑它。
	phantoms := reconcilePhantomRunning(ctx, task)
	setAlert(ctx, alerts, "phantom_running", len(phantoms) > 0,
		"有 "+itoa(len(phantoms))+" 个任务标记为运行中，但没有节点在跑、也不在队列里，已判定失败："+strings.Join(phantoms, "；"))

	return map[string]interface{}{
		"queued":   queued,
		"busy":     busy,
		"zombies":  zombies,
		"phantoms": phantoms,
		"expired":  expired,
		// 当前生效的排队过期窗口：Mongo 配置改了以后，这里能立刻看到实际生效值，
		// 不必等到有任务过期才反推。
		"queueMaxAgeH": int(queueMaxAge().Hours()),
	}, nil
}

// queueMaxAge 排队多久算过期。
//
// 取值顺序：Mongo config{name:"task.queueMaxAgeH"} > 环境变量 TASK_QUEUE_MAX_AGE_H > 默认 72h。
// 为什么要 Mongo 这一层：容器的环境变量只能在重建容器时改，而重建这个服务端会把
// 面板和节点一起折腾一遍（2026-11-26 踩过）。放一条配置就能随时调窗口。
var queueCfg struct {
	sync.Mutex
	value    time.Duration
	loadedAt time.Time
}

func queueMaxAge() time.Duration {
	if v := strings.TrimSpace(os.Getenv("TASK_QUEUE_MAX_AGE_H")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Hour
		}
	}
	queueCfg.Lock()
	defer queueCfg.Unlock()
	if !queueCfg.loadedAt.IsZero() && time.Since(queueCfg.loadedAt) < time.Minute {
		return queueCfg.value
	}
	queueCfg.loadedAt = time.Now()
	queueCfg.value = 72 * time.Hour
	if mongodb.DB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var doc struct {
			Value string `bson:"value"`
		}
		if err := mongodb.DB.Collection("config").FindOne(ctx, bson.M{"name": "task.queueMaxAgeH"}).Decode(&doc); err == nil {
			if n, cerr := strconv.Atoi(strings.TrimSpace(doc.Value)); cerr == nil && n > 0 {
				queueCfg.value = time.Duration(n) * time.Hour
			}
		}
	}
	return queueCfg.value
}

// expireStaleQueued 把排队超窗口的任务过期掉，并从节点队列里摘掉对应条目。
func expireStaleQueued(ctx context.Context, task *mongo.Collection) []string {
	out := []string{}
	cutoff := ts(time.Now().Add(-queueMaxAge()))
	cur, err := task.Find(ctx, bson.M{
		"status":    0,
		"creatTime": bson.M{"$lt": cutoff},
	})
	if err != nil {
		return out
	}
	var rows []bson.M
	_ = cur.All(ctx, &rows)
	_ = cur.Close(ctx)
	if len(rows) == 0 {
		return out
	}
	// 队列里每个节点一张 list，把命中的项摘掉（LREM 需要完整元素值）
	queueKeys, _ := redis.Client.Keys(ctx, "NodeTask:*").Result()
	for _, r := range rows {
		oid, ok := r["_id"].(primitive.ObjectID)
		if !ok {
			continue
		}
		id := oid.Hex()
		for _, qk := range queueKeys {
			items, err := redis.Client.LRange(ctx, qk, 0, -1).Result()
			if err != nil {
				continue
			}
			for _, it := range items {
				if jsonTaskID(it) == id {
					_ = redis.Client.LRem(ctx, qk, 0, it)
				}
			}
		}
		reason := fmt.Sprintf("排队超过 %s 仍未开始（创建于 %v），已过期并移出队列；需要的话 retest 即可重新排队",
			queueMaxAge(), r["creatTime"])
		_, _ = task.UpdateOne(ctx, bson.M{"_id": oid, "status": 0}, bson.M{
			"$set": bson.M{"status": 4, "failReason": reason, "endTime": ts(time.Now())},
		})
		logger.Warn("watchdog: 过期排队任务 " + id + " — " + reason)
		out = append(out, id)
	}
	return out
}

// phantomAfter 判定幽灵任务所需的静默时长。
const phantomAfter = 20 * time.Minute

// reconcilePhantomRunning 找出「status=1 但没有任何执行者」的任务并标记失败。
func reconcilePhantomRunning(ctx context.Context, task *mongo.Collection) []string {
	out := []string{}
	if redis.Client == nil {
		return out
	}
	// 1) 当前正在跑的 taskId 集合
	busyIDs := map[string]bool{}
	if keys, err := redis.Client.Keys(ctx, "node:*:currentTask").Result(); err == nil {
		for _, k := range keys {
			if v, err := redis.Client.Get(ctx, k).Result(); err == nil && v != "" {
				busyIDs[v] = true
			}
		}
	}
	// 2) 队列里的 taskId 集合（队列项是 JSON，字段 id 就是任务 id）
	queuedIDs := map[string]bool{}
	if keys, err := redis.Client.Keys(ctx, "NodeTask:*").Result(); err == nil {
		for _, k := range keys {
			items, err := redis.Client.LRange(ctx, k, 0, -1).Result()
			if err != nil {
				continue
			}
			for _, it := range items {
				if id := jsonTaskID(it); id != "" {
					queuedIDs[id] = true
				}
			}
		}
	}
	// 3) 逐个检查 status=1 的任务
	cur, err := task.Find(ctx, bson.M{"status": 1})
	if err != nil {
		return out
	}
	var rows []bson.M
	_ = cur.All(ctx, &rows)
	_ = cur.Close(ctx)
	now := time.Now()
	for _, r := range rows {
		id := ""
		if oid, ok := r["_id"].(primitive.ObjectID); ok {
			id = oid.Hex()
		}
		if id == "" || busyIDs[id] || queuedIDs[id] {
			continue
		}
		last := lastProgressWrite(ctx, id)
		if last.IsZero() || now.Sub(last) < phantomAfter {
			continue
		}
		reason := fmt.Sprintf("任务标记为运行中，但没有任何节点在跑、也不在队列里，且 %s 没有进度写入；判定为失败，可 retest 重跑", now.Sub(last).Round(time.Minute))
		_, _ = task.UpdateOne(ctx, bson.M{"_id": r["_id"], "status": 1}, bson.M{
			"$set": bson.M{"status": 4, "failReason": reason, "endTime": ts(now)},
		})
		logger.Warn("watchdog: " + id + " " + reason)
		out = append(out, id)
	}
	return out
}

// lastProgressWrite 取该任务所有目标进度 hash 里最新的时间戳（字段值就是 "2006-01-02 15:04:05"）。
func lastProgressWrite(ctx context.Context, taskID string) time.Time {
	keys, err := redis.Client.Keys(ctx, "TaskInfo:progress:"+taskID+":*").Result()
	if err != nil {
		return time.Time{}
	}
	var newest time.Time
	for _, k := range keys {
		vals, err := redis.Client.HVals(ctx, k).Result()
		if err != nil {
			continue
		}
		for _, v := range vals {
			if t, err := time.ParseInLocation("2006-01-02 15:04:05", v, time.Local); err == nil && t.After(newest) {
				newest = t
			}
		}
	}
	return newest
}

// jsonTaskID 从队列项 JSON 里取任务 id。
func jsonTaskID(raw string) string {
	var it struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(raw), &it); err != nil {
		return ""
	}
	return it.ID
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
		if perr != nil || now.Sub(last) < StaleAfter {
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
		// 双重条件：节点心跳停更 + 这个任务本身也没有新的进度写入。
		//
		// 只按节点 updateTime 判断会误杀：updateTime 是服务端在节点来取配置时写的，
		// 节点埋头跑一个长目标（DirScan + nuclei 常常 20~40 分钟）时这个值会滞后，
		// 但任务其实一直在推进（模块开始/结束都会写 TaskInfo:progress）。
		// 所以只要进度还在动，就不算僵尸。
		if lp := lastProgressWrite(ctx, cur); !lp.IsZero() && now.Sub(lp) < StaleAfter {
			continue
		}
		_ = redis.Client.Del(ctx, taskKey)
		reason := fmt.Sprintf("节点 %s 心跳停在 %s（超过 %s 未更新，远大于正常重启所需的 ~7 分钟），已自动释放队列；该任务判定为失败，可 retest 重跑",
			name, upd, StaleAfter)
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
