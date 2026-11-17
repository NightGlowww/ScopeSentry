package site

import (
	"context"
	"fmt"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// maxChainRoots 限制一次链路查询里参与的根域数量：
// 正常数据里一个 host 只属于一个根域，出现多个说明历史脏数据，取前几个足够。
const maxChainRoots = 5

// Repository 站点读模型仓库。
//
// 只读：聚合 asset 集合出站点行，读取 subdomain 集合出子域链路。
// 不写库、不做去重/删除，删除语义（ARL 的 delete 会连带删底层资产）留给后续迭代。
type Repository interface {
	// ListSites 按 host+port 聚合站点行，match 由 service 层拼好（关键字/项目过滤）
	ListSites(ctx context.Context, match bson.M, pageIndex, pageSize int) ([]models.SiteRow, int64, error)
	// GetSubdomainChain 取某个 host 的子域链路（host 自身 + 其根域下的全部子域），按 time 倒序，最多 limit 条
	GetSubdomainChain(ctx context.Context, host string, limit int) ([]models.Subdomain, int64, error)
}

type repository struct {
	assetColl     *mongo.Collection
	subdomainColl *mongo.Collection
}

// NewRepository 创建站点读模型仓库实例
func NewRepository() Repository {
	return &repository{
		assetColl:     mongodb.DB.Collection("asset"),
		subdomainColl: mongodb.DB.Collection("subdomain"),
	}
}

// ListSites 站点列表聚合。
//
// 管道设计：
//  1. $match      先按关键字/项目过滤，尽量让索引和后续分组少扫数据；
//  2. $group      以 {host, port} 为 _id 聚合出 assetCount/lastSeen，
//     并用 $top(sortBy time:-1) 取「最新一条资产」的展示字段——
//     比先全局 $sort 再 $first 省一次全量排序（大集合上 $sort 才是瓶颈）；
//  3. $project    把 _id 拆平、缺失字段兜底成空串/0；
//  4. $sort       按 lastSeen 倒序，host/port 兜底保证分页稳定；
//  5. $facet      一次聚合同时拿 total 和分页数据，避免两次扫描。
func (r *repository) ListSites(ctx context.Context, match bson.M, pageIndex, pageSize int) ([]models.SiteRow, int64, error) {
	skip := int64((pageIndex - 1) * pageSize)
	limit := int64(pageSize)

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: match}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: bson.D{
				{Key: "host", Value: "$host"},
				{Key: "port", Value: "$port"},
			}},
			{Key: "assetCount", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "lastSeen", Value: bson.D{{Key: "$max", Value: "$time"}}},
			{Key: "latest", Value: bson.D{{Key: "$top", Value: bson.D{
				{Key: "sortBy", Value: bson.D{{Key: "time", Value: -1}}},
				{Key: "output", Value: bson.D{
					{Key: "ip", Value: "$ip"},
					{Key: "title", Value: "$title"},
					{Key: "statuscode", Value: "$statuscode"},
					{Key: "project", Value: "$project"},
					{Key: "url", Value: "$url"},
				}},
			}}}},
		}}},
		{{Key: "$project", Value: bson.D{
			{Key: "_id", Value: 0},
			{Key: "host", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$_id.host", ""}}}},
			{Key: "port", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$_id.port", ""}}}},
			{Key: "ip", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$latest.ip", ""}}}},
			{Key: "title", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$latest.title", ""}}}},
			{Key: "statusCode", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$latest.statuscode", 0}}}},
			{Key: "project", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$latest.project", ""}}}},
			{Key: "url", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$latest.url", ""}}}},
			{Key: "assetCount", Value: 1},
			{Key: "lastSeen", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$lastSeen", ""}}}},
		}}},
		{{Key: "$sort", Value: bson.D{
			{Key: "lastSeen", Value: -1},
			{Key: "host", Value: 1},
			{Key: "port", Value: 1},
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

	// 站点聚合要按 host+port 分组，大资产表下分组结果可能超过 100MB 内存上限
	opts := options.Aggregate().SetAllowDiskUse(true)
	cursor, err := r.assetColl.Aggregate(ctx, pipeline, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to aggregate sites: %w", err)
	}
	defer cursor.Close(ctx)

	var facets []struct {
		Total []struct {
			Count int64 `bson:"count"`
		} `bson:"total"`
		Data []models.SiteRow `bson:"data"`
	}
	if err := cursor.All(ctx, &facets); err != nil {
		return nil, 0, fmt.Errorf("failed to decode site facets: %w", err)
	}

	if len(facets) == 0 {
		return []models.SiteRow{}, 0, nil
	}

	rows := facets[0].Data
	if rows == nil {
		rows = []models.SiteRow{}
	}
	var total int64
	if len(facets[0].Total) > 0 {
		total = facets[0].Total[0].Count
	}
	return rows, total, nil
}

// GetSubdomainChain 取子域链路。
//
// 「链路」= 该 host 自身的历史记录 + 它所属根域下的全部子域：
//   - 传根域（example.com）：整棵子域树；
//   - 传子域（a.example.com）：先查它自己记录里的 rootDomain，再按该根域取整棵树 +
//     它自身的历史。只按 host 精确查的话，子域点进去只能看到自己，链路没有意义。
//
// 按 time 倒序（time 是 "2006-01-02 15:04:05" 字符串，字典序即时间序），最多 limit 条。
// total 是未截断前命中的总条数，前端据此判断是否被截断。
func (r *repository) GetSubdomainChain(ctx context.Context, host string, limit int) ([]models.Subdomain, int64, error) {
	roots, err := r.resolveRootDomains(ctx, host)
	if err != nil {
		return nil, 0, err
	}
	// host 自己始终参与匹配（根域没有 rootDomain 记录时也能出结果）
	candidates := append([]string{host}, roots...)

	filter := bson.M{
		"$or": bson.A{
			bson.M{"host": host},
			bson.M{"rootDomain": bson.M{"$in": candidates}},
		},
	}

	total, err := r.subdomainColl.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count subdomain chain for %s: %w", host, err)
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "time", Value: -1}}).
		SetLimit(int64(limit)).
		SetProjection(bson.M{
			"host":       1,
			"type":       1,
			"value":      1,
			"ip":         1,
			"time":       1,
			"tags":       1,
			"project":    1,
			"taskName":   1,
			"rootDomain": 1,
		})

	cursor, err := r.subdomainColl.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query subdomain chain for %s: %w", host, err)
	}
	defer cursor.Close(ctx)

	var chain []models.Subdomain
	if err := cursor.All(ctx, &chain); err != nil {
		return nil, 0, fmt.Errorf("failed to decode subdomain chain for %s: %w", host, err)
	}
	if chain == nil {
		chain = []models.Subdomain{}
	}

	return chain, total, nil
}

// resolveRootDomains 找出该 host 归属的根域（取它自身子域记录里的 rootDomain）。
func (r *repository) resolveRootDomains(ctx context.Context, host string) ([]string, error) {
	values, err := r.subdomainColl.Distinct(ctx, "rootDomain", bson.M{
		"host":       host,
		"rootDomain": bson.M{"$exists": true, "$ne": ""},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to resolve rootDomain for %s: %w", host, err)
	}

	roots := make([]string, 0, len(values))
	for _, value := range values {
		if root, ok := value.(string); ok && root != "" && root != host {
			roots = append(roots, root)
			if len(roots) >= maxChainRoots {
				break
			}
		}
	}
	return roots, nil
}
