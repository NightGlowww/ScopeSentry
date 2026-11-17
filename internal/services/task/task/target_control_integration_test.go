//go:build integration

package task

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	redisdb "github.com/Autumn-27/ScopeSentry/internal/database/redis"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func newTaskGinCtx() *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/api/task/target/pause", nil)
	return c
}

// issue #277：暂停未领取的目标要摘队列并减 taskNum；已领取的目标只记状态；恢复要放回队列并加回来。
func TestPauseResumeTarget(t *testing.T) {
	if mongodb.DB == nil || redisdb.Client == nil {
		t.Skip("mongo/redis not initialized")
	}
	ctx := context.Background()
	svc := NewService()
	name := fmt.Sprintf("target-ctl-%d", time.Now().UnixNano())
	res, err := mongodb.DB.Collection("task").InsertOne(ctx, bson.M{
		"name": name, "status": 0, "progress": 0, "taskNum": 3,
	})
	if err != nil {
		t.Fatalf("seed task: %v", err)
	}
	taskID := res.InsertedID.(primitive.ObjectID)
	queueKey := fmt.Sprintf("TaskInfo:%s", taskID.Hex())
	t.Cleanup(func() {
		_, _ = mongodb.DB.Collection("task").DeleteMany(context.Background(), bson.M{"_id": taskID})
		_, _ = mongodb.DB.Collection("TaskTargetControl").DeleteMany(context.Background(), bson.M{"taskId": taskID.Hex()})
		_ = redisdb.Client.Del(context.Background(), queueKey).Err()
	})

	targets := []string{"a.example.com", "b.example.com", "c.example.com"}
	for _, target := range targets {
		if err := redisdb.Client.LPush(ctx, queueKey, target).Err(); err != nil {
			t.Fatalf("seed queue: %v", err)
		}
	}

	// 暂停队列里的目标 a：摘队列 + taskNum 3 -> 2
	if err := svc.PauseTarget(newTaskGinCtx(), taskID.Hex(), "a.example.com"); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if n := redisdb.Client.LLen(ctx, queueKey).Val(); n != 2 {
		t.Fatalf("queue len = %d, want 2", n)
	}
	assertTaskNum(t, taskID, 2)

	// 幂等：再暂停一次不重复扣减
	if err := svc.PauseTarget(newTaskGinCtx(), taskID.Hex(), "a.example.com"); err != nil {
		t.Fatalf("pause again: %v", err)
	}
	assertTaskNum(t, taskID, 2)

	// 暂停一个不在队列里的目标（已被节点领走）：只记录，不扣减
	if err := svc.PauseTarget(newTaskGinCtx(), taskID.Hex(), "z.example.com"); err != nil {
		t.Fatalf("pause missing: %v", err)
	}
	assertTaskNum(t, taskID, 2)

	// 恢复 a：回到队列并加回 taskNum
	if err := svc.ResumeTarget(newTaskGinCtx(), taskID.Hex(), "a.example.com"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if n := redisdb.Client.LLen(ctx, queueKey).Val(); n != 3 {
		t.Fatalf("queue len = %d, want 3", n)
	}
	assertTaskNum(t, taskID, 3)
	if head, err := redisdb.Client.LIndex(ctx, queueKey, 0).Result(); err != nil || head != "a.example.com" {
		t.Fatalf("head = %q (err %v), want a.example.com", head, err)
	}

	// 暂停列表里应只剩 z
	paused, err := svc.GetPausedTargets(newTaskGinCtx(), taskID.Hex())
	if err != nil {
		t.Fatalf("paused list: %v", err)
	}
	if paused["a.example.com"] || !paused["z.example.com"] {
		t.Fatalf("paused = %v", paused)
	}
}

func assertTaskNum(t *testing.T, id primitive.ObjectID, want int) {
	t.Helper()
	var doc struct {
		TaskNum int `bson:"taskNum"`
	}
	if err := mongodb.DB.Collection("task").FindOne(context.Background(), bson.M{"_id": id}).Decode(&doc); err != nil {
		t.Fatalf("load task: %v", err)
	}
	if doc.TaskNum != want {
		t.Fatalf("taskNum = %d, want %d", doc.TaskNum, want)
	}
}
