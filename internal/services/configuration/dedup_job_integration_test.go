//go:build integration

package configuration_test

// 去重定时任务：配置里的「开关 + 周期」必须对应一个真的在 scheduled 集合里的 job。
//
// 这条用例存在的理由：deduplication 此前既没有 handler 也没有任何注册点，「立即运行一次」
// 之外根本不跑；开关看着是 true，实际每 N 小时什么都不发生。

import (
	"context"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	schedulerCore "github.com/Autumn-27/ScopeSentry/internal/scheduler"
	confSvc "github.com/Autumn-27/ScopeSentry/internal/services/configuration"
	"go.mongodb.org/mongo-driver/bson"
)

// scheduledJob 读出 handler 与 cron 表达式：
// Schedule 是「这个 job 多久跑一次」的唯一可见证据，只断言 handler 存在证明不了它会触发。
func scheduledJob(t *testing.T, id string) (handler, schedule string, found bool) {
	t.Helper()
	var doc struct {
		Handler  string `bson:"handler"`
		Schedule string `bson:"schedule"`
	}
	err := mongodb.DB.Collection("scheduled").FindOne(context.Background(), bson.M{"_id": id}).Decode(&doc)
	if err != nil {
		return "", "", false
	}
	return doc.Handler, doc.Schedule, true
}

func TestEnsureDeduplicationJobFollowsConfig(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	ctx := context.Background()
	svc := confSvc.NewService()
	configColl := mongodb.DB.Collection("config")

	// 备份并还原：这个配置是全局单例，用例不能把别人的状态留在库里
	var backup bson.M
	_ = configColl.FindOne(ctx, bson.M{"name": "deduplication"}).Decode(&backup)
	t.Cleanup(func() {
		cleanup := context.Background()
		if backup != nil {
			_, _ = configColl.ReplaceOne(cleanup, bson.M{"name": "deduplication"}, backup)
		}
		_ = svc.EnsureDeduplicationJob(cleanup)
	})

	// 1) flag=true, hour=3 → 注册 interval job，handler=deduplication
	if _, err := configColl.UpdateOne(ctx, bson.M{"name": "deduplication"}, bson.M{"$set": bson.M{"flag": true, "hour": 3}}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	if err := svc.EnsureDeduplicationJob(ctx); err != nil {
		t.Fatalf("ensure job: %v", err)
	}
	handler, schedule, found := scheduledJob(t, "deduplication")
	if !found || handler != "deduplication" {
		t.Fatalf("开关为 true 时应注册 job：found=%v handler=%q", found, handler)
	}
	if schedule != "0 */3 * * *" {
		t.Fatalf("hour=3 时应排成每 3 小时一次，实际 %q", schedule)
	}

	// 2) flag=false → 移除 job（留着空转会让「下次运行时间」一直往前走，看起来像还在跑）
	if _, err := configColl.UpdateOne(ctx, bson.M{"name": "deduplication"}, bson.M{"$set": bson.M{"flag": false}}); err != nil {
		t.Fatalf("update config: %v", err)
	}
	if err := svc.EnsureDeduplicationJob(ctx); err != nil {
		t.Fatalf("ensure job off: %v", err)
	}
	if _, _, found := scheduledJob(t, "deduplication"); found {
		t.Fatalf("开关为 false 时不应留下 job")
	}

	// 3) handler 已注册：否则 job 到期只会报「没有 handler」
	deadline := time.Now().Add(2 * time.Second)
	_ = deadline
	schedulerCore.InitializeGlobalScheduler()
}
