//go:build integration

package routes

import (
	"context"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/database/redis"
	nodesvc "github.com/Autumn-27/ScopeSentry/internal/services/node"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 「节点在线、心跳正常、队列 6 个任务，但界面看不出为什么不走」——2026-11-29 现场。
// 节点列表此前只有「在线 / 队列长度」，必须登服务器翻 Redis 才知道卡在哪个模块上。
// 这里造一个卡住节点，确认 /node 列表的字段口径能把它直接标出来。
func TestNodeDataReportsStalledCurrentTask(t *testing.T) {
	t.Setenv("WATCHDOG_STALLED_AFTER_MIN", "1")
	ctx := context.Background()
	if redis.Client == nil || mongodb.DB == nil {
		t.Skip("mongo/redis 未连接，跳过节点运行态用例")
	}

	const nodeName = "it-node-runtime"
	const taskName = "it-runtime-task"
	tasks := mongodb.DB.Collection("task")
	ins, err := tasks.InsertOne(ctx, bson.M{
		"name": taskName, "status": 1, "progress": 85,
		"node": []string{nodeName}, "taskNum": 2,
		"creatTime": "2026-11-29 00:26:25",
	})
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}
	oid := ins.InsertedID.(primitive.ObjectID)
	id := oid.Hex()
	hostKey := "node:" + nodeName
	curKey := hostKey + ":currentTask"
	progKey := "TaskInfo:progress:" + id + ":target.example"
	t.Cleanup(func() {
		_, _ = tasks.DeleteOne(ctx, bson.M{"_id": oid})
		_, _ = mongodb.DB.Collection("node_mem_samples").DeleteMany(ctx, bson.M{"node": nodeName})
		if redis.Client != nil {
			_, _ = redis.Client.Del(ctx, hostKey, curKey, progKey).Result()
		}
	})

	old := time.Now().Add(-90 * time.Minute).Format("2006-01-02 15:04:05")
	if err := redis.Client.HSet(ctx, hostKey, map[string]interface{}{
		"state": "1", "updateTime": time.Now().Format("2006-01-02 15:04:05"),
		"running": "1", "finished": "0", "maxTaskNum": "1",
	}).Err(); err != nil {
		t.Fatalf("hset node: %v", err)
	}
	if err := redis.Client.Set(ctx, curKey, id, 0).Err(); err != nil {
		t.Fatalf("set currentTask: %v", err)
	}
	if err := redis.Client.HSet(ctx, progKey, map[string]interface{}{
		"node": nodeName, "VulnerabilityScan_start": old,
	}).Err(); err != nil {
		t.Fatalf("hset progress: %v", err)
	}

	list, err := nodesvc.NewService().GetNodeData(ctx, false)
	if err != nil {
		t.Fatalf("GetNodeData: %v", err)
	}
	var got *struct {
		CurrentTaskID   string
		CurrentTaskName string
		CurrentModule   string
		CurrentTarget   string
		IdleMinutes     int
		Stalled         bool
	}
	for _, n := range list {
		if n.Name != nodeName {
			continue
		}
		got = &struct {
			CurrentTaskID   string
			CurrentTaskName string
			CurrentModule   string
			CurrentTarget   string
			IdleMinutes     int
			Stalled         bool
		}{n.CurrentTaskID, n.CurrentTaskName, n.CurrentModule, n.CurrentTarget, n.IdleMinutes, n.Stalled}
	}
	if got == nil {
		t.Fatalf("节点 %s 没出现在列表里", nodeName)
	}
	if got.CurrentTaskID != id || got.CurrentTaskName != taskName {
		t.Fatalf("当前任务没解析出来: id=%q name=%q", got.CurrentTaskID, got.CurrentTaskName)
	}
	if got.CurrentModule != "VulnerabilityScan" || got.CurrentTarget != "target.example" {
		t.Fatalf("模块/目标没解析出来: module=%q target=%q", got.CurrentModule, got.CurrentTarget)
	}
	if got.IdleMinutes < 60 {
		t.Fatalf("无产出时长不对: %d 分钟", got.IdleMinutes)
	}
	if !got.Stalled {
		t.Fatalf("超过阈值却没标卡住: idle=%d", got.IdleMinutes)
	}
}
