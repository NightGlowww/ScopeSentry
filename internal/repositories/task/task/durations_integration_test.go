//go:build integration

package task

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"go.mongodb.org/mongo-driver/bson"
)

// 队列 ETA 的数据源：已完成任务的耗时。
//
// 踩过的坑：查询要求 startedAt 非空，而这个字段长期没有任何写入方 ——
// 结果永远 0 条样本，节点队列一直显示「未知」。现在 startedAt 会在任务进入运行态时写，
// 历史任务退回 creatTime（含排队时间，属于上限估计，估算器自己会截断异常值）。
func TestRecentTaskDurationsMinutes(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	ctx := context.Background()
	coll := mongodb.DB.Collection("task")
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	prefix := "it-dur-" + suffix

	fmtTime := func(t time.Time) string { return t.Format("2006-01-02 15:04:05") }
	base := time.Now().Add(-3 * time.Hour)

	docs := []interface{}{
		// 有 startedAt：精确耗时 10 分钟
		bson.M{
			"name": prefix + "-with-start", "status": 3,
			"creatTime": fmtTime(base.Add(-40 * time.Minute)),
			"startedAt": fmtTime(base),
			"endTime":   fmtTime(base.Add(10 * time.Minute)),
		},
		// 没有 startedAt：退回 creatTime，耗时 30 分钟（含排队，属上限）
		bson.M{
			"name": prefix + "-no-start", "status": 3,
			"creatTime": fmtTime(base.Add(30 * time.Minute)),
			"endTime":   fmtTime(base.Add(60 * time.Minute)),
		},
		// 运行中：不该进样本
		bson.M{
			"name": prefix + "-running", "status": 1,
			"creatTime": fmtTime(base), "startedAt": fmtTime(base), "endTime": "",
		},
		// 时间倒挂：跳过
		bson.M{
			"name": prefix + "-bad", "status": 3,
			"creatTime": fmtTime(base), "endTime": fmtTime(base.Add(-5 * time.Minute)),
		},
	}
	if _, err := coll.InsertMany(ctx, docs); err != nil {
		t.Fatalf("seed tasks: %v", err)
	}
	t.Cleanup(func() {
		_, _ = coll.DeleteMany(context.Background(), bson.M{"name": bson.M{"$regex": "^" + prefix}})
	})

	svc := NewRepository()
	samples := svc.RecentTaskDurationsMinutes(ctx, 50)
	want := map[float64]bool{10: false, 30: false}
	for _, v := range samples {
		if _, ok := want[v]; ok {
			want[v] = true
		}
	}
	for minutes, seen := range want {
		if !seen {
			t.Fatalf("样本里缺少 %v 分钟（拿到 %v）", minutes, samples)
		}
	}
}
