package ip

import (
	"context"
	"fmt"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ReadModelRepository IP 读模型仓库（对标 ARL-Next assetIP 的列表 / 反查域名）。
//
// 与 Repository 的区别：Repository 读的是节点写好的 IPAsset 集合（已经聚合过），
// 这里直接从 asset 集合现算，保证「站点/IP」两个读模型口径一致、不依赖回填。
// 只读，不写任何集合。
type ReadModelRepository interface {
	// ListIPs 按 ip 聚合 IP 行，match 由 service 层拼好（关键字/前缀过滤）
	ListIPs(ctx context.Context, match bson.M, pageIndex, pageSize int) ([]models.IPRow, int64, error)
	// ListDomainsByIP 反查某个 IP 上的域名：asset.host + subdomain.ip。
	// 返回 (列表, 去重域名总数)；列表最多 limit 条，total 不受截断影响。
	ListDomainsByIP(ctx context.Context, ip string, limit int) ([]models.IPDomainItem, int64, error)
}

type readModelRepository struct {
	assetColl *mongo.Collection
}

// NewReadModelRepository 创建 IP 读模型仓库实例
func NewReadModelRepository() ReadModelRepository {
	return &readModelRepository{
		assetColl: mongodb.DB.Collection("asset"),
	}
}

// ListIPs IP 列表聚合。
//
// 管道设计：
//  1. $match    先按 ip 存在性 + 关键字/前缀过滤；
//  2. $group     以 ip 为 _id，$addToSet 收端口(port+protocol)和域名(host)；
//  3. $project   排序去重后的数组（$sortArray，保证前 20 个是稳定顺序），过滤空 host；
//  4. $project   在同一管道里再算 portCount/domainCount 并对数组做 $slice 20——
//     计数用的是截断前的完整数组，所以 count 是真实数量，数组只是展示用；
//  5. $sort      按 lastSeen 倒序 + ip 兜底，保证分页稳定；
//  6. $facet     一次聚合同时拿 total 和分页数据。
func (r *readModelRepository) ListIPs(ctx context.Context, match bson.M, pageIndex, pageSize int) ([]models.IPRow, int64, error) {
	skip := int64((pageIndex - 1) * pageSize)
	limit := int64(pageSize)

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: match}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$ip"},
			{Key: "ports", Value: bson.D{{Key: "$addToSet", Value: bson.D{
				{Key: "port", Value: "$port"},
				{Key: "protocol", Value: "$service"},
			}}}},
			{Key: "hosts", Value: bson.D{{Key: "$addToSet", Value: "$host"}}},
			{Key: "assetCount", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "lastSeen", Value: bson.D{{Key: "$max", Value: "$time"}}},
		}}},
		{{Key: "$project", Value: bson.D{
			{Key: "_id", Value: 0},
			{Key: "ip", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$_id", ""}}}},
			{Key: "assetCount", Value: 1},
			{Key: "lastSeen", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$lastSeen", ""}}}},
			{Key: "ports", Value: bson.D{{Key: "$sortArray", Value: bson.D{
				{Key: "input", Value: "$ports"},
				{Key: "sortBy", Value: bson.D{{Key: "port", Value: 1}}},
			}}}},
			{Key: "domains", Value: bson.D{{Key: "$sortArray", Value: bson.D{
				{Key: "input", Value: bson.D{{Key: "$filter", Value: bson.D{
					{Key: "input", Value: "$hosts"},
					{Key: "as", Value: "h"},
					{Key: "cond", Value: bson.D{{Key: "$and", Value: bson.A{
						bson.D{{Key: "$ne", Value: bson.A{"$$h", ""}}},
						bson.D{{Key: "$ne", Value: bson.A{"$$h", nil}}},
					}}}},
				}}}},
				{Key: "sortBy", Value: 1},
			}}}},
		}}},
		{{Key: "$project", Value: bson.D{
			{Key: "ip", Value: 1},
			{Key: "assetCount", Value: 1},
			{Key: "lastSeen", Value: 1},
			{Key: "portCount", Value: bson.D{{Key: "$size", Value: "$ports"}}},
			{Key: "domainCount", Value: bson.D{{Key: "$size", Value: "$domains"}}},
			{Key: "ports", Value: bson.D{{Key: "$slice", Value: bson.A{"$ports", 20}}}},
			{Key: "domains", Value: bson.D{{Key: "$slice", Value: bson.A{"$domains", 20}}}},
		}}},
		{{Key: "$sort", Value: bson.D{
			{Key: "lastSeen", Value: -1},
			{Key: "ip", Value: 1},
		}}},
		{{Key: "$facet", Value: bson.M{
			"total": mongo.Pipeline{
				{{Key: "$count", Value: "count"}},
			},
			"data": mongo.Pipeline{
				{{Key: "$skip", Value: skip}},
				{{Key: "$limit", Value: limit}},
			},
		}}},
	}

	opts := options.Aggregate().SetAllowDiskUse(true)
	cursor, err := r.assetColl.Aggregate(ctx, pipeline, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to aggregate ip assets: %w", err)
	}
	defer cursor.Close(ctx)

	var facets []struct {
		Total []struct {
			Count int64 `bson:"count"`
		} `bson:"total"`
		Data []models.IPRow `bson:"data"`
	}
	if err := cursor.All(ctx, &facets); err != nil {
		return nil, 0, fmt.Errorf("failed to decode ip facets: %w", err)
	}

	if len(facets) == 0 {
		return []models.IPRow{}, 0, nil
	}

	rows := facets[0].Data
	if rows == nil {
		rows = []models.IPRow{}
	}
	var total int64
	if len(facets[0].Total) > 0 {
		total = facets[0].Total[0].Count
	}
	return rows, total, nil
}

// ListDomainsByIP 反查某个 IP 上的域名。
//
// 两个来源合到一条聚合里：asset.host（直接扫到的域名）+ subdomain.ip（DNS 解析记录）。
// $unionWith 让 subdomain 的命中走同一次聚合，避免应用层两次查询再手工合并；
// 最后按域名分组，$addToSet 标记来源，time 取最大值作为 lastSeen。
func (r *readModelRepository) ListDomainsByIP(ctx context.Context, ip string, limit int) ([]models.IPDomainItem, int64, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"ip":   ip,
			"host": bson.M{"$exists": true, "$ne": ""},
		}}},
		{{Key: "$project", Value: bson.D{
			{Key: "_id", Value: 0},
			{Key: "host", Value: 1},
			{Key: "time", Value: 1},
			{Key: "source", Value: bson.D{{Key: "$literal", Value: "asset"}}},
		}}},
		{{Key: "$unionWith", Value: bson.D{
			{Key: "coll", Value: "subdomain"},
			{Key: "pipeline", Value: mongo.Pipeline{
				{{Key: "$match", Value: bson.M{
					"ip":   ip,
					"host": bson.M{"$exists": true, "$ne": ""},
				}}},
				{{Key: "$project", Value: bson.D{
					{Key: "_id", Value: 0},
					{Key: "host", Value: 1},
					{Key: "time", Value: 1},
					{Key: "source", Value: bson.D{{Key: "$literal", Value: "subdomain"}}},
				}}},
			}},
		}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$host"},
			{Key: "sources", Value: bson.D{{Key: "$addToSet", Value: "$source"}}},
			{Key: "lastSeen", Value: bson.D{{Key: "$max", Value: "$time"}}},
		}}},
		// total 与数据分开出：total 是去重后的真实域名数，data 才是被 limit 截断的列表，
		// 避免「域名超过上限时 total 也跟着缩水」。
		{{Key: "$facet", Value: bson.M{
			"total": mongo.Pipeline{
				{{Key: "$count", Value: "count"}},
			},
			"data": mongo.Pipeline{
				{{Key: "$sort", Value: bson.D{{Key: "_id", Value: 1}}}},
				{{Key: "$limit", Value: int64(limit)}},
				{{Key: "$project", Value: bson.D{
					{Key: "_id", Value: 0},
					{Key: "domain", Value: "$_id"},
					{Key: "sources", Value: bson.D{{Key: "$sortArray", Value: bson.D{
						{Key: "input", Value: "$sources"},
						{Key: "sortBy", Value: 1},
					}}}},
					{Key: "lastSeen", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$lastSeen", ""}}}},
				}}},
			},
		}}},
	}

	opts := options.Aggregate().SetAllowDiskUse(true)
	cursor, err := r.assetColl.Aggregate(ctx, pipeline, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to aggregate domains for ip %s: %w", ip, err)
	}
	defer cursor.Close(ctx)

	var facets []struct {
		Total []struct {
			Count int64 `bson:"count"`
		} `bson:"total"`
		Data []models.IPDomainItem `bson:"data"`
	}
	if err := cursor.All(ctx, &facets); err != nil {
		return nil, 0, fmt.Errorf("failed to decode domains for ip %s: %w", ip, err)
	}

	if len(facets) == 0 {
		return []models.IPDomainItem{}, 0, nil
	}
	items := facets[0].Data
	if items == nil {
		items = []models.IPDomainItem{}
	}
	var total int64
	if len(facets[0].Total) > 0 {
		total = facets[0].Total[0].Count
	}
	return items, total, nil
}
