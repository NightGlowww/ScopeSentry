// Package projectaggregation 是项目详情页的只读聚合仓库。
//
// 数据来源均为现成集合（asset / subdomain / UrlScan / vulnerability / SensitiveResult /
// DirScanResult / IPAsset / scheduled），不新增写入链路，不改任何集合结构。
// 性能约束（docs/ARL_NEXT_PARITY.md）：
//   - 列表一律 $facet 一次拿 total + 当前页，不允许"先全量再内存分页"；
//   - 排序走索引 {project:1,time:-1}，聚合统一 SetAllowDiskUse(true)；
//   - pageSize 上限由 service 层校验（>200 → 400）。
package projectaggregation

import (
	"context"
	"errors"
	"fmt"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// 集合名常量：与节点侧写入保持一致，避免各处手写字符串拼错
const (
	CollAsset     = "asset"
	CollSubdomain = "subdomain"
	CollURLScan   = "UrlScan"
	CollVuln      = "vulnerability"
	CollSensitive = "SensitiveResult"
	CollDirScan   = "DirScanResult"
	CollIPAsset   = "IPAsset"
	CollScheduled = "scheduled"
	CollProject   = "project"
)

// Repository 项目聚合只读仓库
type Repository interface {
	// FindProject 按 _id 取项目；不存在返回 (nil, nil)
	FindProject(ctx context.Context, id primitive.ObjectID) (*models.ProjectAggregationInfo, error)
	// NextRunTime 取定时任务下次执行时间（scheduled._id == 项目 id）；没有返回空串
	NextRunTime(ctx context.Context, jobID string) (string, error)
	// Count 精确计数（带 filter）
	Count(ctx context.Context, collection string, filter bson.M) (int64, error)
	// VulLevelStats 漏洞等级分布
	VulLevelStats(ctx context.Context, match bson.M) ([]models.ProjectAggregationLevelCount, error)
	// VulList 漏洞分页列表
	VulList(ctx context.Context, match bson.M, pageIndex, pageSize int) ([]models.ProjectAggregationVulRow, int64, error)
	// SubdomainList 子域名分页列表
	SubdomainList(ctx context.Context, match bson.M, pageIndex, pageSize int) ([]models.ProjectAggregationSubdomainRow, int64, error)
	// PortGroups 端口聚合（按 port 分组）
	PortGroups(ctx context.Context, match bson.M, pageIndex, pageSize int) ([]models.ProjectAggregationPortRow, int64, error)
	// ServiceGroups 服务聚合（按 service 分组）
	ServiceGroups(ctx context.Context, match bson.M, pageIndex, pageSize int) ([]models.ProjectAggregationServiceRow, int64, error)
	// Explain 返回聚合的 executionStats（只用于测试/性能核对）
	Explain(ctx context.Context, collection string, pipeline mongo.Pipeline) (bson.M, error)
}

type repository struct{ db *mongo.Database }

// NewRepository 创建聚合仓库
func NewRepository() Repository { return &repository{db: mongodb.DB} }

func (r *repository) FindProject(ctx context.Context, id primitive.ObjectID) (*models.ProjectAggregationInfo, error) {
	var doc struct {
		Name           string   `bson:"name"`
		Tag            string   `bson:"tag"`
		ScheduledTasks bool     `bson:"scheduledTasks"`
		Hour           int      `bson:"hour"`
		AssetCount     int64    `bson:"AssetCount"`
		RootDomains    []string `bson:"root_domains"`
	}
	err := r.db.Collection(CollProject).FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find project %s: %w", id.Hex(), err)
	}
	rootDomains := doc.RootDomains
	if rootDomains == nil {
		rootDomains = []string{}
	}
	return &models.ProjectAggregationInfo{
		ID:             id.Hex(),
		Name:           doc.Name,
		Tag:            doc.Tag,
		ScheduledTasks: doc.ScheduledTasks,
		Hour:           doc.Hour,
		AssetCount:     int(doc.AssetCount),
		RootDomains:    rootDomains,
	}, nil
}

func (r *repository) NextRunTime(ctx context.Context, jobID string) (string, error) {
	var doc struct {
		NextRunTime primitive.DateTime `bson:"next_run_time"`
	}
	err := r.db.Collection(CollScheduled).FindOne(ctx, bson.M{"_id": jobID}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return "", nil
		}
		return "", fmt.Errorf("failed to find scheduled job %s: %w", jobID, err)
	}
	if doc.NextRunTime == 0 {
		return "", nil
	}
	return doc.NextRunTime.Time().Format("2006-01-02 15:04:05"), nil
}

func (r *repository) Count(ctx context.Context, collection string, filter bson.M) (int64, error) {
	n, err := r.db.Collection(collection).CountDocuments(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("failed to count %s: %w", collection, err)
	}
	return n, nil
}

func (r *repository) VulLevelStats(ctx context.Context, match bson.M) ([]models.ProjectAggregationLevelCount, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: match}},
		{{Key: "$group", Value: bson.M{"_id": "$level", "count": bson.M{"$sum": 1}}}},
		{{Key: "$match", Value: bson.M{"_id": bson.M{"$nin": []interface{}{"", nil}}}}},
		{{Key: "$sort", Value: bson.D{{Key: "count", Value: -1}, {Key: "_id", Value: 1}}}},
	}
	cursor, err := r.db.Collection(CollVuln).Aggregate(ctx, pipeline, options.Aggregate().SetAllowDiskUse(true))
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate vulnerability levels: %w", err)
	}
	defer cursor.Close(ctx)

	rows := make([]models.ProjectAggregationLevelCount, 0, 6)
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("failed to decode vulnerability levels: %w", err)
	}
	return rows, nil
}

// runFacet 执行 $facet 管道，把单条结果文档里的 {total:[{n}], rows:[...]} 解成 (rows, total)。
//
// 泛型只用于解码目标类型；查询语义全部在调用方拼装好的 pipeline 里。
func runFacet[T any](ctx context.Context, db *mongo.Database, collection string, pipeline mongo.Pipeline) ([]T, int64, error) {
	cursor, err := db.Collection(collection).Aggregate(ctx, pipeline, options.Aggregate().SetAllowDiskUse(true))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to aggregate %s: %w", collection, err)
	}
	defer cursor.Close(ctx)

	if !cursor.Next(ctx) {
		if err := cursor.Err(); err != nil {
			return nil, 0, fmt.Errorf("failed to iterate %s: %w", collection, err)
		}
		return []T{}, 0, nil
	}

	var out struct {
		Total []struct {
			N int64 `bson:"n"`
		} `bson:"total"`
		Rows []T `bson:"rows"`
	}
	if err := cursor.Decode(&out); err != nil {
		return nil, 0, fmt.Errorf("failed to decode %s aggregation: %w", collection, err)
	}
	var total int64
	if len(out.Total) > 0 {
		total = out.Total[0].N
	}
	if out.Rows == nil {
		out.Rows = []T{}
	}
	return out.Rows, total, nil
}

// facetStage 生成 $facet：total 只数一次，rows 走 skip/limit
func facetStage(pageIndex, pageSize int, sort bson.D) bson.M {
	rows := []bson.M{}
	if sort != nil {
		rows = append(rows, bson.M{"$sort": sort})
	}
	rows = append(rows,
		bson.M{"$skip": int64((pageIndex - 1) * pageSize)},
		bson.M{"$limit": int64(pageSize)},
	)
	return bson.M{
		"total": []bson.M{{"$count": "n"}},
		"rows":  rows,
	}
}

func (r *repository) VulList(ctx context.Context, match bson.M, pageIndex, pageSize int) ([]models.ProjectAggregationVulRow, int64, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: match}},
		// 索引 {project:1,time:-1} 直接提供顺序，无需内存排序
		{{Key: "$sort", Value: bson.D{{Key: "time", Value: -1}}}},
		{{Key: "$project", Value: bson.M{
			"id":      bson.M{"$toString": "$_id"},
			"url":     1,
			"vulname": 1,
			"level":   1,
			"time":    1,
			"matched": 1,
		}}},
		{{Key: "$facet", Value: facetStage(pageIndex, pageSize, nil)}},
	}
	return runFacet[models.ProjectAggregationVulRow](ctx, r.db, CollVuln, pipeline)
}

func (r *repository) SubdomainList(ctx context.Context, match bson.M, pageIndex, pageSize int) ([]models.ProjectAggregationSubdomainRow, int64, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: match}},
		{{Key: "$sort", Value: bson.D{{Key: "time", Value: -1}}}},
		{{Key: "$project", Value: bson.M{
			"id":   bson.M{"$toString": "$_id"},
			"host": 1,
			"type": 1,
			// value / ip 可能为 null；前端 formatter 直接 forEach，必须归一成数组
			"value":      bson.M{"$ifNull": bson.A{"$value", bson.A{}}},
			"ip":         bson.M{"$ifNull": bson.A{"$ip", bson.A{}}},
			"rootDomain": 1,
			"time":       1,
		}}},
		{{Key: "$facet", Value: facetStage(pageIndex, pageSize, nil)}},
	}
	return runFacet[models.ProjectAggregationSubdomainRow](ctx, r.db, CollSubdomain, pipeline)
}

func (r *repository) PortGroups(ctx context.Context, match bson.M, pageIndex, pageSize int) ([]models.ProjectAggregationPortRow, int64, error) {
	group := bson.M{
		"_id":     "$port",
		"count":   bson.M{"$sum": 1},
		"host":    bson.M{"$first": "$host"},
		"ip":      bson.M{"$first": "$ip"},
		"service": bson.M{"$first": "$service"},
		"time":    bson.M{"$max": "$time"},
	}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: match}},
		// 先按 time 倒序（走 {project:1,time:-1}），组内 $first 即该端口最近一条资产
		{{Key: "$sort", Value: bson.D{{Key: "time", Value: -1}}}},
		{{Key: "$group", Value: group}},
		{{Key: "$match", Value: bson.M{"_id": bson.M{"$nin": []interface{}{"", nil}}}}},
		{{Key: "$addFields", Value: bson.M{
			"id":   bson.M{"$toString": "$_id"},
			"port": bson.M{"$toString": "$_id"},
		}}},
		{{Key: "$facet", Value: facetStage(pageIndex, pageSize, bson.D{{Key: "count", Value: -1}, {Key: "_id", Value: 1}})}},
	}
	return runFacet[models.ProjectAggregationPortRow](ctx, r.db, CollAsset, pipeline)
}

func (r *repository) ServiceGroups(ctx context.Context, match bson.M, pageIndex, pageSize int) ([]models.ProjectAggregationServiceRow, int64, error) {
	// service 为空时退到 type（http / https / other），与 Python 版 project_aggregation.py 一致
	serviceKey := bson.M{"$cond": bson.A{
		bson.M{"$gt": bson.A{bson.M{"$strLenCP": bson.M{"$ifNull": bson.A{"$service", ""}}}, 0}},
		"$service",
		"$type",
	}}
	group := bson.M{
		"_id":   serviceKey,
		"count": bson.M{"$sum": 1},
		"host":  bson.M{"$first": "$host"},
		"ip":    bson.M{"$first": "$ip"},
		"port":  bson.M{"$first": "$port"},
		"time":  bson.M{"$max": "$time"},
	}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: match}},
		{{Key: "$sort", Value: bson.D{{Key: "time", Value: -1}}}},
		{{Key: "$group", Value: group}},
		{{Key: "$match", Value: bson.M{"_id": bson.M{"$nin": []interface{}{"", nil}}}}},
		{{Key: "$addFields", Value: bson.M{
			"id":      bson.M{"$toString": "$_id"},
			"service": bson.M{"$toString": "$_id"},
		}}},
		{{Key: "$facet", Value: facetStage(pageIndex, pageSize, bson.D{{Key: "count", Value: -1}, {Key: "_id", Value: 1}})}},
	}
	return runFacet[models.ProjectAggregationServiceRow](ctx, r.db, CollAsset, pipeline)
}

func (r *repository) Explain(ctx context.Context, collection string, pipeline mongo.Pipeline) (bson.M, error) {
	cmd := bson.D{
		{Key: "explain", Value: bson.D{
			{Key: "aggregate", Value: collection},
			{Key: "pipeline", Value: pipeline},
			{Key: "cursor", Value: bson.M{}},
		}},
		{Key: "verbosity", Value: "executionStats"},
	}
	var out bson.M
	if err := r.db.RunCommand(ctx, cmd).Decode(&out); err != nil {
		return nil, fmt.Errorf("failed to explain %s: %w", collection, err)
	}
	return out, nil
}
