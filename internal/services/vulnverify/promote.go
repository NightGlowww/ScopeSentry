package vulnverify

// 漏洞「确认」策略。
//
// 背景：看板上 已确认 一直是 0，因为整条链路只写 verifyState（verified/weak/skipped），
// 从来没人把 status 推到 5。也就是说复核跑得很认真，但结果没进业务状态。
//
// 这里补上那一跳，规则只有三条：
//   1. verifyState=verified（按模板的 matcher 在真实响应上成立）→ status=5 已确认；
//   2. 模板已被标 excluded（NQ / 不可复验）→ 不确认，连记录一起挪进隔离区；
//   3. weak / skipped 不动，继续留在「候选」里等人看。
//
// 人工改过的状态不会被覆盖：只在 status 缺失或等于 1（未处理）时才推。

import (
	"context"
	"fmt"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// templateExcluded 模板是否已被标记为排除（NQ / 不可复验 / 写操作）。
func templateExcluded(ctx context.Context, vulnID string) (excluded bool, known bool) {
	if vulnID == "" {
		return false, false
	}
	var doc struct {
		Excluded *bool `bson:"excluded"`
	}
	coll := mongodb.DB.Collection("PocList")
	if oid, err := primitive.ObjectIDFromHex(vulnID); err == nil {
		if coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&doc) == nil {
			return doc.Excluded != nil && *doc.Excluded, true
		}
	}
	if coll.FindOne(ctx, bson.M{"id": vulnID}).Decode(&doc) == nil {
		return doc.Excluded != nil && *doc.Excluded, true
	}
	return false, false
}

// PromoteOnce 把已验证的记录推进「已确认」，把模板已排除的记录挪进隔离区。
// 返回 (confirmed, quarantined, err)。
func PromoteOnce(ctx context.Context) (int, int, error) {
	if mongodb.DB == nil {
		return 0, 0, nil
	}
	cur, err := mongodb.DB.Collection("vulnerability").Find(ctx, bson.M{
		"verifyState": "verified",
		"$or":         []bson.M{{"status": 1}, {"status": bson.M{"$exists": false}}},
	})
	if err != nil {
		return 0, 0, err
	}
	var docs []bson.M
	if err := cur.All(ctx, &docs); err != nil {
		return 0, 0, err
	}
	_ = cur.Close(ctx)

	confirmed, quarantined := 0, 0
	filters := bson.M{"$or": []bson.M{{"status": 1}, {"status": bson.M{"$exists": false}}}, "statusSource": bson.M{"$ne": "human"}}
	for _, d := range docs {
		vulnID, _ := d["vulnid"].(string)
		if ex, known := templateExcluded(ctx, vulnID); known && ex {
			// 模板已经被判定为 NQ / 不可复验：这条历史记录留着只会继续污染看板。
			d["quarantinedAt"] = time.Now().Format("2006-01-02 15:04:05")
			d["reason"] = "template excluded (NQ or unverifiable)"
			if _, err := mongodb.DB.Collection("vulnerabilityQuarantine").InsertOne(ctx, d); err == nil {
				hash, _ := d["hash"].(string)
				if hash != "" {
					_, _ = mongodb.DB.Collection("vulnerabilityDetail").DeleteMany(ctx, bson.M{"hash": hash})
				}
				_, _ = mongodb.DB.Collection("vulnerability").DeleteOne(ctx, bson.M{"_id": d["_id"]})
				quarantined++
			}
			continue
		}

		sel := bson.M{"_id": d["_id"]}
		for k, v := range filters {
			sel[k] = v
		}
		res, err := mongodb.DB.Collection("vulnerability").UpdateOne(ctx, sel, bson.M{"$set": bson.M{
			"status":      5,
			"confirmedBy": "auto:vulnverify",
			"confirmedAt": time.Now().Format("2006-01-02 15:04:05"),
		}})
		if err == nil && res.MatchedCount > 0 {
			confirmed++
		}
	}
	if confirmed > 0 || quarantined > 0 {
		logger.Info(fmt.Sprintf("vulnverify: promoted %d to confirmed, quarantined %d (excluded template)", confirmed, quarantined))
	}
	return confirmed, quarantined, nil
}
