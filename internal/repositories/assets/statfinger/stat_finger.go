package statfinger

import (
	"context"
	"fmt"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// 聚合里保留的样例 host 个数：由 $topN 在服务端截断，不把全量 host 拉回内存。
const sampleHostLimit = 5

// Repository 指纹统计读模型仓库（对标 ARL-Next assetStatFinger）。
//
// 数据源是 asset 集合的 technologies 数组，只读，不写任何集合。
// 与 IPAsset 那种「节点先回填好再读」的模型不同：这里每次现算，
// 保证统计口径永远跟资产表一致，不依赖回填任务跑过。
type Repository interface {
	// ListTechnologies 按技术名聚合统计。
	// preMatch 在 $unwind 之前过滤（能用上 technologies/project 索引），
	// postMatch 在 $unwind 之后过滤（关键字只能落在技术名标量上）。
	ListTechnologies(ctx context.Context, preMatch, postMatch bson.M, pageIndex, pageSize int) ([]models.StatFingerRow, int64, error)
	// ListAssetsByTechnology 反查使用某技术的资产（按 time 倒序分页）
	ListAssetsByTechnology(ctx context.Context, filter bson.M, pageIndex, pageSize int) ([]models.StatFingerAssetRow, int64, error)
	// ExplainTechnologies 返回统计聚合的 explain（queryPlanner），用于验证走索引
	ExplainTechnologies(ctx context.Context, preMatch, postMatch bson.M, pageIndex, pageSize int) (bson.M, error)
	// ExplainAssetsByTechnology 返回反查资产查询的 explain（queryPlanner）
	ExplainAssetsByTechnology(ctx context.Context, filter bson.M, pageIndex, pageSize int) (bson.M, error)
}

type repository struct {
	db        *mongo.Database
	assetColl *mongo.Collection
}

// NewRepository 创建指纹统计读模型仓库实例
func NewRepository() Repository {
	return &repository{
		db:        mongodb.DB,
		assetColl: mongodb.DB.Collection("asset"),
	}
}

// technologyPipeline 指纹统计聚合管道。
//
// 设计要点（过滤/排序/截断全部在服务端做，不拉回内存处理）：
//  1. $match 先按 project + technologies 存在性收窄，前面的条件是索引友好的；
//  2. $unwind 把 technologies 数组摊平成「一行一个技术」；
//  3. $match 过滤空技术名 + 关键字（必须在 unwind 之后，否则关键字会命中同一文档里
//     另一个技术名，把不相关的技术一起带进来）；
//  4. $group 先按「技术 × 资产 _id」去重：同一资产里重复写了同一个技术时，
//     出现次数要算多次、资产数只能算一次；
//  5. $group 再按技术名汇总，$topN 在服务端只保留 5 个样例 host；
//  6. $facet 一次聚合同时出 total 和分页数据，避免两次全量扫描。
func technologyPipeline(preMatch, postMatch bson.M, pageIndex, pageSize int) mongo.Pipeline {
	skip := int64((pageIndex - 1) * pageSize)
	limit := int64(pageSize)

	afterUnwind := bson.M{
		"technologies": bson.M{"$ne": ""},
	}
	if len(postMatch) > 0 {
		afterUnwind = bson.M{"$and": bson.A{afterUnwind, postMatch}}
	}

	return mongo.Pipeline{
		{{Key: "$match", Value: preMatch}},
		{{Key: "$unwind", Value: "$technologies"}},
		{{Key: "$match", Value: afterUnwind}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: bson.D{
				{Key: "technology", Value: "$technologies"},
				{Key: "asset", Value: "$_id"},
			}},
			{Key: "occurrences", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "host", Value: bson.D{{Key: "$first", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$host", ""}}}}}},
			{Key: "time", Value: bson.D{{Key: "$first", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$time", ""}}}}}},
		}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$_id.technology"},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: "$occurrences"}}},
			{Key: "assetCount", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "lastSeen", Value: bson.D{{Key: "$max", Value: "$time"}}},
			{Key: "samples", Value: bson.D{{Key: "$topN", Value: bson.D{
				{Key: "n", Value: int64(sampleHostLimit)},
				{Key: "sortBy", Value: bson.D{{Key: "host", Value: 1}}},
				{Key: "output", Value: "$host"},
			}}}},
		}}},
		{{Key: "$facet", Value: bson.M{
			"total": mongo.Pipeline{
				{{Key: "$count", Value: "count"}},
			},
			"data": mongo.Pipeline{
				// count 倒序，同 count 按技术名升序兜底，保证分页稳定
				{{Key: "$sort", Value: bson.D{{Key: "count", Value: -1}, {Key: "_id", Value: 1}}}},
				{{Key: "$skip", Value: skip}},
				{{Key: "$limit", Value: limit}},
				{{Key: "$project", Value: bson.D{
					{Key: "_id", Value: 0},
					{Key: "name", Value: "$_id"},
					{Key: "count", Value: 1},
					{Key: "assetCount", Value: 1},
					{Key: "lastSeen", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$lastSeen", ""}}}},
					// $topN 可能带进空 host（IP 型资产没有 host），先剔掉；
					// 再按出现顺序去重：同一 host 的不同端口也是不同资产文档，
					// 样例里重复同一个 host 会白占名额。
					{Key: "samples", Value: bson.D{{Key: "$reduce", Value: bson.D{
						{Key: "input", Value: bson.D{{Key: "$filter", Value: bson.D{
							{Key: "input", Value: "$samples"},
							{Key: "as", Value: "h"},
							{Key: "cond", Value: bson.D{{Key: "$ne", Value: bson.A{"$$h", ""}}}},
						}}}},
						{Key: "initialValue", Value: bson.A{}},
						{Key: "in", Value: bson.D{{Key: "$cond", Value: bson.A{
							bson.D{{Key: "$in", Value: bson.A{"$$this", "$$value"}}},
							"$$value",
							bson.D{{Key: "$concatArrays", Value: bson.A{"$$value", bson.A{"$$this"}}}},
						}}}},
					}}}},
				}}},
			},
		}}},
	}
}

// ListTechnologies 按技术名聚合统计。
func (r *repository) ListTechnologies(ctx context.Context, preMatch, postMatch bson.M, pageIndex, pageSize int) ([]models.StatFingerRow, int64, error) {
	pipeline := technologyPipeline(preMatch, postMatch, pageIndex, pageSize)

	opts := options.Aggregate().SetAllowDiskUse(true)
	cursor, err := r.assetColl.Aggregate(ctx, pipeline, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to aggregate technologies: %w", err)
	}
	defer cursor.Close(ctx)

	var facets []struct {
		Total []struct {
			Count int64 `bson:"count"`
		} `bson:"total"`
		Data []models.StatFingerRow `bson:"data"`
	}
	if err := cursor.All(ctx, &facets); err != nil {
		return nil, 0, fmt.Errorf("failed to decode technology facets: %w", err)
	}
	if len(facets) == 0 {
		return []models.StatFingerRow{}, 0, nil
	}

	rows := facets[0].Data
	if rows == nil {
		rows = []models.StatFingerRow{}
	}
	var total int64
	if len(facets[0].Total) > 0 {
		total = facets[0].Total[0].Count
	}
	return rows, total, nil
}

// assetDoc 反查资产时的解码结构：_id 用 ObjectID 接、在仓库层转字符串，
// 这样查询仍然是普通 find（能用上 technologies+time 索引），
// 不需要为了 $toString 把 find 改成聚合。
type assetDoc struct {
	ID         primitive.ObjectID `bson:"_id"`
	Host       string             `bson:"host"`
	Port       string             `bson:"port"`
	IP         string             `bson:"ip"`
	URL        string             `bson:"url"`
	Title      string             `bson:"title"`
	StatusCode int                `bson:"statuscode"`
	Service    string             `bson:"service"`
	Project    string             `bson:"project"`
	RootDomain string             `bson:"rootDomain"`
	Time       string             `bson:"time"`
}

// assetProjection 只取列表需要的字段，body/screenshot/rawheaders 这些大字段一律不取。
var assetProjection = bson.D{
	{Key: "host", Value: 1},
	{Key: "port", Value: 1},
	{Key: "ip", Value: 1},
	{Key: "url", Value: 1},
	{Key: "title", Value: 1},
	{Key: "statuscode", Value: 1},
	{Key: "service", Value: 1},
	{Key: "project", Value: 1},
	{Key: "rootDomain", Value: 1},
	{Key: "time", Value: 1},
}

// assetFindOptions 反查资产的 find 选项。
// 只按 time 倒序：{technologies:1, time:-1} 能直接提供顺序，
// 再加 host 之类的次级排序字段就会退化成 blocking sort。
func assetFindOptions(pageIndex, pageSize int) *options.FindOptions {
	return options.Find().
		SetProjection(assetProjection).
		SetSort(bson.D{{Key: "time", Value: -1}}).
		SetSkip(int64((pageIndex - 1) * pageSize)).
		SetLimit(int64(pageSize))
}

// ListAssetsByTechnology 反查使用某技术的资产。
func (r *repository) ListAssetsByTechnology(ctx context.Context, filter bson.M, pageIndex, pageSize int) ([]models.StatFingerAssetRow, int64, error) {
	total, err := r.assetColl.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count assets by technology: %w", err)
	}
	if total == 0 {
		return []models.StatFingerAssetRow{}, 0, nil
	}

	cursor, err := r.assetColl.Find(ctx, filter, assetFindOptions(pageIndex, pageSize))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to find assets by technology: %w", err)
	}
	defer cursor.Close(ctx)

	var docs []assetDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, 0, fmt.Errorf("failed to decode assets by technology: %w", err)
	}

	rows := make([]models.StatFingerAssetRow, 0, len(docs))
	for _, d := range docs {
		rows = append(rows, models.StatFingerAssetRow{
			ID:         d.ID.Hex(),
			Host:       d.Host,
			Port:       d.Port,
			IP:         d.IP,
			URL:        d.URL,
			Title:      d.Title,
			StatusCode: d.StatusCode,
			Service:    d.Service,
			Project:    d.Project,
			RootDomain: d.RootDomain,
			Time:       d.Time,
		})
	}
	return rows, total, nil
}

// ExplainTechnologies 统计聚合的 explain。verbosity=queryPlanner 足够看走没走索引，
// 同时避免 executionStats 在高基数字段上真的把聚合跑一遍。
func (r *repository) ExplainTechnologies(ctx context.Context, preMatch, postMatch bson.M, pageIndex, pageSize int) (bson.M, error) {
	return r.explainAggregate(ctx, "asset", technologyPipeline(preMatch, postMatch, pageIndex, pageSize))
}

// ExplainAssetsByTechnology 反查资产查询的 explain。
func (r *repository) ExplainAssetsByTechnology(ctx context.Context, filter bson.M, pageIndex, pageSize int) (bson.M, error) {
	cmd := bson.D{
		{Key: "explain", Value: bson.D{
			{Key: "find", Value: "asset"},
			{Key: "filter", Value: filter},
			{Key: "sort", Value: bson.D{{Key: "time", Value: -1}}},
			{Key: "skip", Value: int64((pageIndex - 1) * pageSize)},
			{Key: "limit", Value: int64(pageSize)},
		}},
		{Key: "verbosity", Value: "queryPlanner"},
	}
	var out bson.M
	if err := r.db.RunCommand(ctx, cmd).Decode(&out); err != nil {
		return nil, fmt.Errorf("failed to explain asset query: %w", err)
	}
	return out, nil
}

func (r *repository) explainAggregate(ctx context.Context, coll string, pipeline mongo.Pipeline) (bson.M, error) {
	cmd := bson.D{
		{Key: "explain", Value: bson.D{
			{Key: "aggregate", Value: coll},
			{Key: "pipeline", Value: pipeline},
			{Key: "cursor", Value: bson.D{}},
		}},
		{Key: "verbosity", Value: "queryPlanner"},
	}
	var out bson.M
	if err := r.db.RunCommand(ctx, cmd).Decode(&out); err != nil {
		return nil, fmt.Errorf("failed to explain aggregate on %s: %w", coll, err)
	}
	return out, nil
}
