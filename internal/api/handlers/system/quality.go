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
	// 排除「排队过期」被标记失败的任务：它们没跑过，算进完成数会把 24h 产出看板带偏。
	finished, _ := task.CountDocuments(ctx, bson.M{
		"endTime":    bson.M{"$gte": cutoff},
		"status":     bson.M{"$in": []int{3, 4}},
		"failReason": bson.M{"$not": bson.M{"$regex": "^排队超过"}},
	})
	out["tasks"] = gin.H{"created24h": started, "finished24h": finished}

	// 2) 漏洞：24h 新增 + 等级分布
	//
	// 注意两个窗口必须分开命名：之前 byLevel 只统计 24h，却和全量 total 并排返回，
	// 读起来就是「总数 3 条，只有 1 条 low」—— 其实是另外 2 条在 24h 之前（2026-11-26 踩过）。
	// 现在 byLevel24h 与 byLevelAll 都返回，谁也不冒充谁；byLevel 保留为 24h 的旧名兼容。
	fresh, _ := vuln.CountDocuments(ctx, bson.M{"time": bson.M{"$gte": cutoff}})
	levelAgg := func(match bson.M) gin.H {
		res := gin.H{}
		if cur, err := vuln.Aggregate(ctx, bson.A{
			bson.M{"$match": match},
			bson.M{"$group": bson.M{"_id": "$level", "n": bson.M{"$sum": 1}}},
		}); err == nil {
			var rows []bson.M
			_ = cur.All(ctx, &rows)
			_ = cur.Close(ctx)
			for _, r := range rows {
				if k, ok := r["_id"].(string); ok && k != "" {
					res[k] = r["n"]
				}
			}
		}
		return res
	}
	byLevel24 := levelAgg(bson.M{"time": bson.M{"$gte": cutoff}})
	byLevelAll := levelAgg(bson.M{})
	out["vulnerabilities"] = gin.H{
		"new24h":     fresh,
		"byLevel24h": byLevel24,
		"byLevelAll": byLevelAll,
		"byLevel":    byLevel24, // 兼容旧调用方
		"total":      mustCount(ctx, vuln, bson.M{}),
	}

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

	// 4.5) 证据完整度：漏洞详情里到底有没有原始请求/响应。
	//
	// 为什么单独统计：nuclei 的 ResponseSaveSize 一旦为 0，ResultEvent.Response 会被截成
	// 空串，详情表全是 0 字节——而漏洞列表看起来一切正常（2026-11-26 就是这么埋了几天）。
	// 把这个比例摆到看板上，退化了当场能看见，不用等人工翻详情才发现。
	det := mongodb.DB.Collection("vulnerabilityDetail")
	detTotal := mustCount(ctx, det, bson.M{})
	withResp := mustCount(ctx, det, bson.M{"response": bson.M{"$nin": []interface{}{"", nil}}})
	withReq := mustCount(ctx, det, bson.M{"request": bson.M{"$nin": []interface{}{"", nil}}})
	out["evidence"] = gin.H{
		"details":      detTotal,
		"withResponse": withResp,
		"withRequest":  withReq,
	}

	// 5) 断流告警（watchdog 写入 system_alerts）
	alertRows := []bson.M{}
	if cur, err := mongodb.DB.Collection("system_alerts").Find(ctx, bson.M{"active": true}); err == nil {
		_ = cur.All(ctx, &alertRows)
		_ = cur.Close(ctx)
	}
	out["alerts"] = alertRows

	response.Success(c, out, "api.success")
}

func mustCount(ctx context.Context, coll *mongo.Collection, filter bson.M) int64 {
	n, _ := coll.CountDocuments(ctx, filter)
	return n
}
