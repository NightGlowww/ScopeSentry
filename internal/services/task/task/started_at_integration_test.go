//go:build integration

package task

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/constants"
	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/database/redis"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
)

// 待开始 → 运行中 的那一步必须落 startedAt：
// 节点队列的「还要等多久」用它算单任务耗时；不写的话 ETA 永远是「未知」。
func TestProcessTaskProgressStampsStartedAt(t *testing.T) {
	if mongodb.DB == nil || redis.Client == nil {
		t.Skip("mongo/redis not initialized")
	}
	ctx := context.Background()
	coll := mongodb.DB.Collection("task")
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	name := "it-started-at-" + suffix

	res, err := coll.InsertOne(ctx, bson.M{
		"name": name, "status": constants.TaskStatusPending, "taskNum": 3, "progress": 0,
		"creatTime": time.Now().Format("2006-01-02 15:04:05"),
	})
	if err != nil {
		t.Fatalf("seed task: %v", err)
	}
	id := res.InsertedID
	t.Cleanup(func() {
		_, _ = coll.DeleteMany(context.Background(), bson.M{"name": name})
		_, _ = redis.Client.Del(context.Background(),
			fmt.Sprintf("TaskInfo:tmp:%s", hexOf(id)), fmt.Sprintf("TaskInfo:time:%s", hexOf(id))).Result()
	})

	hex := hexOf(id)
	tmpKey := fmt.Sprintf("TaskInfo:tmp:%s", hex)
	if err := redis.Client.SAdd(ctx, tmpKey, "target-a").Err(); err != nil {
		t.Fatalf("seed redis tmp: %v", err)
	}

	svc := NewService()
	var task models.Task
	if err := coll.FindOne(ctx, bson.M{"_id": id}).Decode(&task); err != nil {
		t.Fatalf("load task: %v", err)
	}
	if err := svc.ProcessTaskProgress(ctx, task); err != nil {
		t.Fatalf("ProcessTaskProgress: %v", err)
	}

	var after struct {
		Status    int    `bson:"status"`
		StartedAt string `bson:"startedAt"`
	}
	if err := coll.FindOne(ctx, bson.M{"_id": id}).Decode(&after); err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if after.Status != constants.TaskStatusRunning {
		t.Fatalf("status = %d, want running(%d)", after.Status, constants.TaskStatusRunning)
	}
	if after.StartedAt == "" {
		t.Fatalf("进入运行态后 startedAt 仍为空（ETA 会一直算不出来）")
	}
}

func hexOf(id interface{}) string {
	if oid, ok := id.(interface{ Hex() string }); ok {
		return oid.Hex()
	}
	return fmt.Sprintf("%v", id)
}
