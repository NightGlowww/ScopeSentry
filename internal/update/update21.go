package update

import (
	"context"
	"strconv"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/constants"
	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Update21 给「终态任务」补产出快照（stats）。
//
// 背景：stats 是 2026-11-28 才加的字段，只有之后完成的任务才有快照。
// 历史任务的战果一直是实时 count，且列表新增的「产出」列对它们只能显示「—」。
// 这里按当前数据补一份冻结快照——此刻统计到的就是此刻的事实，写下来之后不再漂移。
//
// 幂等：只处理 status∈{3,4} 且还没有 stats 的任务，已有的一个都不动。
// 放在后台跑：历史任务可能上百条，每条 4 次 count，不该卡启动。
func Update21() {
	go func() {
		ctx := context.Background()
		col := mongodb.DB.Collection("task")
		cur, err := col.Find(ctx, bson.M{
			"status": bson.M{"$in": []int{constants.TaskStatusFinished, constants.TaskStatusFailed}},
			"stats":  bson.M{"$exists": false},
		}, options.Find().SetProjection(bson.M{"_id": 1, "name": 1}).SetLimit(2000))
		if err != nil {
			logger.Error("update21: find terminal tasks: " + err.Error())
			return
		}
		defer func() { _ = cur.Close(ctx) }()

		type row struct {
			ID   any    `bson:"_id"`
			Name string `bson:"name"`
		}
		specs := []struct{ key, coll string }{
			{"subdomain", "subdomain"},
			{"url", "UrlScan"},
			{"asset", "asset"},
			{"vulnerability", "vulnerability"},
		}
		now := time.Now().Format("2006-01-02 15:04:05")
		filled := 0
		for cur.Next(ctx) {
			var r row
			if cur.Decode(&r) != nil || r.Name == "" {
				continue
			}
			stats := bson.M{}
			for _, s := range specs {
				n, cerr := mongodb.DB.Collection(s.coll).CountDocuments(ctx, bson.M{"taskName": r.Name})
				if cerr == nil {
					stats[s.key] = n
				}
			}
			if _, uerr := col.UpdateOne(ctx, bson.M{"_id": r.ID, "stats": bson.M{"$exists": false}}, bson.M{
				"$set": bson.M{"stats": stats, "statsFrozenAt": now},
			}); uerr == nil {
				filled++
			}
		}
		if filled > 0 {
			logger.Info("update21: backfilled task stats, count=" + strconv.Itoa(filled))
		}
	}()
}
