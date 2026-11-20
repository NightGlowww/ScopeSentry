package node

import (
	"context"
	"fmt"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// 节点内存采样：回答「这台是一直 100% 还是偶尔冲高」。
// 不新起调度器：在节点列表读取时惰性采样（≥4 分钟才写一条），
// 有人看的时候才有数据，正好符合「趋势是给人看的」（2026-11-20）。
const (
	MemSampleCollection = "node_mem_samples"
	memSampleInterval   = 4 * time.Minute
	memSampleWindow     = time.Hour
)

// EnsureMemSample 按需写入一条采样（同一节点 4 分钟内只写一条）。
func (r *repository) EnsureMemSample(ctx context.Context, name string, mem, running float64) {
	if mem <= 0 || mongodb.DB == nil {
		return
	}
	coll := mongodb.DB.Collection(MemSampleCollection)
	opts := options.FindOne().SetSort(bson.D{{Key: "at", Value: -1}})
	var last struct {
		At time.Time `bson:"at"`
	}
	if err := coll.FindOne(ctx, bson.M{"node": name}, opts).Decode(&last); err == nil {
		if time.Since(last.At) < memSampleInterval {
			return
		}
	}
	_, _ = coll.InsertOne(ctx, bson.M{
		"node":    name,
		"at":      time.Now(),
		"mem":     mem,
		"running": running,
	})
}

// MemPeaks 返回每个节点最近一小时的峰值与均值（无采样数据的节点不出现）。
func (r *repository) MemPeaks(ctx context.Context) map[string][2]float64 {
	out := map[string][2]float64{}
	if mongodb.DB == nil {
		return out
	}
	cut := time.Now().Add(-memSampleWindow)
	cur, err := mongodb.DB.Collection(MemSampleCollection).Aggregate(ctx, []bson.M{
		{"$match": bson.M{"at": bson.M{"$gte": cut}}},
		{"$group": bson.M{
			"_id": "$node",
			"max": bson.M{"$max": "$mem"},
			"avg": bson.M{"$avg": "$mem"},
		}},
	})
	if err != nil {
		return out
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		var row struct {
			Node string  `bson:"_id"`
			Max  float64 `bson:"max"`
			Avg  float64 `bson:"avg"`
		}
		if cur.Decode(&row) == nil && row.Node != "" {
			out[row.Node] = [2]float64{row.Max, row.Avg}
		}
	}
	return out
}

var _ = fmt.Sprintf

// MemHighMinutes 返回每个节点「连续处于高压（≥90%）」的分钟数。
// 用惰性采样的时间序列算：从最新一条往前，只要相邻样本间隔 <10 分钟且都 ≥90，就继续累计。
// 用途：界面区分「刚冲高」和「已经高压 45 分钟」——后者才是要处理的（2026-11-21）。
func (r *repository) MemHighMinutes(ctx context.Context) map[string]int {
	out := map[string]int{}
	if mongodb.DB == nil {
		return out
	}
	cur, err := mongodb.DB.Collection(MemSampleCollection).Find(ctx, bson.M{},
		options.Find().SetSort(bson.D{{Key: "at", Value: -1}}).SetLimit(2000))
	if err != nil {
		return out
	}
	defer cur.Close(ctx)
	var rows []struct {
		Node string    `bson:"node"`
		At   time.Time `bson:"at"`
		Mem  float64   `bson:"mem"`
	}
	if cur.All(ctx, &rows) != nil {
		return out
	}
	lastSeen := map[string]time.Time{}
	for _, s := range rows {
		if s.Node == "" {
			continue
		}
		prev, seen := lastSeen[s.Node]
		if !seen {
			lastSeen[s.Node] = s.At
			if s.Mem >= 90 {
				out[s.Node] = 4 // 第一条高压样本按采样间隔计
			}
			continue
		}
		gap := prev.Sub(s.At)
		if s.Mem >= 90 && gap <= 10*time.Minute {
			out[s.Node] += int(gap.Minutes()) + 4
		} else if s.Mem < 90 {
			break // 遇到低压样本，连续段结束
		}
	}
	return out
}
