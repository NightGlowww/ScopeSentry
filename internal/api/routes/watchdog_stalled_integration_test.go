//go:build integration

package routes

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/database/redis"
	"github.com/Autumn-27/ScopeSentry/internal/services/watchdog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 「节点活着、任务还在它的 currentTask 上，但长时间没有进度」——这类任务能堵队列几天，
// 而原有的三条巡检（tasks_stalled / phantom_running / node_zombie）都抓不到。
// 这里直接造现场跑一轮 Check()，确认它被判失败并出现在告警结果里。
func TestWatchdogFailsStalledRunningTask(t *testing.T) {
	t.Setenv("WATCHDOG_STALLED_AFTER_MIN", "1")
	ctx := context.Background()
	tasks := mongodb.DB.Collection("task")

	name := "it-stalled-" + t.Name()
	ins, err := tasks.InsertOne(ctx, bson.M{
		"name": name, "status": 1, "progress": 85,
		"node": []string{"it-node-stalled"}, "taskNum": 2,
		"creatTime": "2026-11-24 01:26:25",
	})
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}
	oid := ins.InsertedID.(primitive.ObjectID)
	id := oid.Hex()
	t.Cleanup(func() { _, _ = tasks.DeleteOne(ctx, bson.M{"_id": oid}) })

	if redis.Client == nil {
		t.Skip("redis 未连接，跳过卡住检测用例")
	}
	taskKey := "node:it-node-stalled:currentTask"
	progKey := "TaskInfo:progress:" + id + ":target.example"
	if err := redis.Client.Set(ctx, taskKey, id, 0).Err(); err != nil {
		t.Fatalf("set currentTask: %v", err)
	}
	old := time.Now().Add(-24 * time.Hour).Format("2006-01-02 15:04:05")
	if err := redis.Client.HSet(ctx, progKey, "VulnerabilityScan_start", old).Err(); err != nil {
		t.Fatalf("hset progress: %v", err)
	}
	t.Cleanup(func() {
		_, _ = redis.Client.Del(ctx, taskKey).Result()
		_, _ = redis.Client.Del(ctx, progKey).Result()
	})

	res, err := watchdog.Check(ctx)
	if err != nil {
		t.Fatalf("watchdog.Check: %v", err)
	}
	stalled, _ := res["stalled"].([]string)
	found := false
	for _, s := range stalled {
		if s == id {
			found = true
		}
	}
	if !found {
		t.Fatalf("stalled 结果里没有 %s: %v", id, res["stalled"])
	}

	var doc struct {
		Status     int    `bson:"status"`
		FailCode   string `bson:"failCode"`
		FailReason string `bson:"failReason"`
	}
	if err := tasks.FindOne(ctx, bson.M{"_id": oid}).Decode(&doc); err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if doc.Status != 4 || doc.FailCode != "stalledNoProgress" {
		t.Fatalf("任务未被判失败: status=%d failCode=%q", doc.Status, doc.FailCode)
	}
	if !strings.Contains(doc.FailReason, "it-node-stalled") || !strings.Contains(doc.FailReason, "重启") {
		t.Fatalf("failReason 没写清节点与处置建议: %q", doc.FailReason)
	}
}
