// Package assetcollapse 处理「跨 IP 同质默认页」：同一份响应正文出现在 >=10 个不同 IP 上，
// 基本可以判定为默认后端/边缘兜底页，而不是各自独立的资产（对照 ARL-Next 的收敛策略）。
//
// 只打标不删除：默认页也可能是真实资产（例如整站挂在同一模板上），删了不可逆。
// 标记字段 defaultBackend=true，供界面过滤与后续统计使用。
package assetcollapse

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
)

// 一份正文出现在多少个不同 IP 上才算默认页。ARL-Next 取 10。
const minDistinctIPs = 10

// RunOnce 扫一批高频正文并打标，返回 (hashes, assets)。
func RunOnce(ctx context.Context, limit int32) (int, int64, error) {
	if mongodb.DB == nil {
		return 0, 0, fmt.Errorf("mongodb not ready")
	}
	coll := mongodb.DB.Collection("asset")
	pipeline := []bson.M{
		{"$match": bson.M{"bodyhash": bson.M{"$nin": []interface{}{"", nil}}, "defaultBackend": bson.M{"$ne": true}}},
		{"$group": bson.M{"_id": "$bodyhash", "ips": bson.M{"$addToSet": "$ip"}, "n": bson.M{"$sum": 1}}},
		{"$match": bson.M{"n": bson.M{"$gte": minDistinctIPs}}},
		{"$project": bson.M{"ips": bson.M{"$size": "$ips"}, "n": 1}},
		{"$match": bson.M{"ips": bson.M{"$gte": minDistinctIPs}}},
		{"$limit": limit},
	}
	cur, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		return 0, 0, err
	}
	defer cur.Close(ctx)
	var rows []struct {
		ID  string `bson:"_id"`
		N   int    `bson:"n"`
		IPs int    `bson:"ips"`
	}
	if err = cur.All(ctx, &rows); err != nil {
		return 0, 0, err
	}
	var total int64
	for _, r := range rows {
		res, uerr := coll.UpdateMany(ctx, bson.M{"bodyhash": r.ID}, bson.M{"$set": bson.M{
			"defaultBackend":    true,
			"defaultBackendIPs": r.IPs,
		}})
		if uerr != nil {
			logger.Error("assetcollapse: update failed: " + uerr.Error())
			continue
		}
		total += res.ModifiedCount
	}
	return len(rows), total, nil
}

// Start 每 6 小时跑一次；ASSET_COLLAPSE=off 关闭。
func Start(ctx context.Context) {
	if os.Getenv("ASSET_COLLAPSE") == "off" {
		logger.Info("assetcollapse: disabled by ASSET_COLLAPSE=off")
		return
	}
	go func() {
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for {
			hashes, assets, err := RunOnce(ctx, 200)
			if err != nil {
				logger.Error("assetcollapse: run failed: " + err.Error())
			} else if hashes > 0 {
				logger.Info(fmt.Sprintf("assetcollapse: hashes=%d assets=%d", hashes, assets))
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
