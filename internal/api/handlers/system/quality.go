package system

// 产出质量看板：回答「扫描器到底挖出了什么、LLM 怎么看」。
//
// 为什么需要它：这几天最耗时的排查不是修 bug，而是**看不出系统有没有在产出**——
// 漏洞集合一直是 3 条，没人知道是「没挖」还是「挖了没中」。
// 这个端点把三件事变成一眼可见的数字：
//   1. 24 小时内跑了多少任务、完成多少；
//   2. 新产出多少漏洞、按等级分布；
//   3. LLM 判定分布 + 隔离区按原因/模板聚合（哪个模板最吵）。

import (
	"context"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// QualityReport 只读统计，不改任何数据。
func QualityReport(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	if mongodb.DB == nil {
		response.Success(c, gin.H{"error": "db not ready"}, "api.success")
		return
	}
	cutoff := time.Now().Add(-24 * time.Hour).Format("2006-01-02 15:04:05")
	vuln := mongodb.DB.Collection("vulnerability")
	task := mongodb.DB.Collection("task")
	quar := mongodb.DB.Collection("vulnerabilityQuarantine")

	out := gin.H{}

	// 1) 24h 内任务：开始 / 完成
	started, _ := task.CountDocuments(ctx, bson.M{"creatTime": bson.M{"$gte": cutoff}})
	finished, _ := task.CountDocuments(ctx, bson.M{"endTime": bson.M{"$gte": cutoff}, "status": bson.M{"$in": []int{3, 4}}})
	out["tasks"] = gin.H{"created24h": started, "finished24h": finished}

	// 2) 24h 内新漏洞 + 等级分布
	fresh, _ := vuln.CountDocuments(ctx, bson.M{"time": bson.M{"$gte": cutoff}})
	byLevel := gin.H{}
	if cur, err := vuln.Aggregate(ctx, bson.A{
		bson.M{"$match": bson.M{"time": bson.M{"$gte": cutoff}}},
		bson.M{"$group": bson.M{"_id": "$level", "n": bson.M{"$sum": 1}}},
	}); err == nil {
		var rows []bson.M
		_ = cur.All(ctx, &rows)
		_ = cur.Close(ctx)
		for _, r := range rows {
			if k, ok := r["_id"].(string); ok {
				byLevel[k] = r["n"]
			}
		}
	}
	out["vulnerabilities"] = gin.H{"new24h": fresh, "byLevel": byLevel, "total": mustCount(ctx, vuln, bson.M{})}

	// 3) LLM 判定分布（全量）
	verdicts := gin.H{"confirmed": 0, "rejected": 0, "needsHuman": 0, "unjudged": 0}
	if cur, err := vuln.Aggregate(ctx, bson.A{
		bson.M{"$group": bson.M{"_id": "$jev.decision", "n": bson.M{"$sum": 1}}},
	}); err == nil {
		var rows []bson.M
		_ = cur.All(ctx, &rows)
		_ = cur.Close(ctx)
		for _, r := range rows {
			switch r["_id"] {
			case "confirmed":
				verdicts["confirmed"] = r["n"]
			case "rejected":
				verdicts["rejected"] = r["n"]
			case "needs_human":
				verdicts["needsHuman"] = r["n"]
			default:
				verdicts["unjudged"] = r["n"]
			}
		}
	}
	out["llm"] = verdicts

	// 4) 隔离区：按原因 + 按模板（找最吵的模板）
	qReasons := gin.H{}
	if cur, err := quar.Aggregate(ctx, bson.A{
		bson.M{"$group": bson.M{"_id": "$reason", "n": bson.M{"$sum": 1}}},
		bson.M{"$sort": bson.M{"n": -1}},
	}); err == nil {
		var rows []bson.M
		_ = cur.All(ctx, &rows)
		_ = cur.Close(ctx)
		for _, r := range rows {
			if k, ok := r["_id"].(string); ok && k != "" {
				qReasons[k] = r["n"]
			}
		}
	}
	topTpl := []bson.M{}
	if cur, err := quar.Aggregate(ctx, bson.A{
		bson.M{"$group": bson.M{"_id": "$vulname", "n": bson.M{"$sum": 1}}},
		bson.M{"$sort": bson.M{"n": -1}},
		bson.M{"$limit": 5},
	}); err == nil {
		_ = cur.All(ctx, &topTpl)
		_ = cur.Close(ctx)
	}
	out["quarantine"] = gin.H{"total": mustCount(ctx, quar, bson.M{}), "byReason": qReasons, "topTemplates": topTpl}

	response.Success(c, out, "api.success")
}

func mustCount(ctx context.Context, coll *mongo.Collection, filter bson.M) int64 {
	n, _ := coll.CountDocuments(ctx, filter)
	return n
}
