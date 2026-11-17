package dashboard

import (
	"context"
	"fmt"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	redisdb "github.com/Autumn-27/ScopeSentry/internal/database/redis"
	redisdriver "github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// 仪表盘读模型仓库：只读，不写任何集合。
//
// 统计口径：
//   - 总量走 countDocuments；空条件时走集合元数据（EstimatedDocumentCount），
//     不把任何文档拉回进程；
//   - 趋势走 $group 按天聚合（$substrCP 取 "YYYY-MM-DD"），同样不拉文档；
//   - 节点数来自 Redis 心跳键（node:*），节点状态的唯一来源就是 Redis。

// allowedCollections 允许统计的集合白名单。
// 集合名是内部常量拼进来的，白名单只是防手滑写错集合名、顺带把口径固定下来。
var allowedCollections = map[string]bool{
	"asset":           true,
	"subdomain":       true,
	"UrlScan":         true,
	"vulnerability":   true,
	"SensitiveResult": true,
	"task":            true,
}

// NodeHeartbeat 节点心跳（只取判定存活需要的字段，不把整个 hash 拉回来）
type NodeHeartbeat struct {
	Name       string
	State      string
	UpdateTime string
}

// Repository 仪表盘读模型仓库
type Repository interface {
	// Count 计数：空 filter 走集合元数据，非空走 countDocuments（吃索引）
	Count(ctx context.Context, collection string, filter bson.M) (int64, error)
	// TrendByDay 在 [startStr, endStr] 内按天（YYYY-MM-DD）分组计数
	TrendByDay(ctx context.Context, collection, timeField string, base bson.M, startStr, endStr string) (map[string]int64, error)
	// ExplainTrend 返回趋势聚合的 queryPlanner explain（集成测试验证走索引）
	ExplainTrend(ctx context.Context, collection, timeField string, base bson.M, startStr, endStr string) (bson.M, error)
	// NodeHeartbeats 返回所有节点心跳（SCAN + pipeline，不逐键往返）
	NodeHeartbeats(ctx context.Context) ([]NodeHeartbeat, error)
}

type repository struct {
	db          *mongo.Database
	redisClient *redisdriver.Client
}

// NewRepository 创建仪表盘读模型仓库
func NewRepository() Repository {
	return &repository{
		db:          mongodb.DB,
		redisClient: redisdb.Client,
	}
}

// Count 统计文档数。
//
// 空 filter 用 EstimatedDocumentCount：它读的是集合元数据（O(1)），
// 在百万级资产表上和 countDocuments({}) 差两三个数量级，且不会把文档读出来。
// 有 filter 时必须用 countDocuments，元数据帮不上忙，条件会落到索引上。
func (r *repository) Count(ctx context.Context, collection string, filter bson.M) (int64, error) {
	if !allowedCollections[collection] {
		return 0, fmt.Errorf("dashboard: collection %q is not allowed for counting", collection)
	}
	coll := r.db.Collection(collection)
	if len(filter) == 0 {
		return coll.EstimatedDocumentCount(ctx)
	}
	return coll.CountDocuments(ctx, filter)
}

// dayExpr 取文档时间字段的前 10 个字符（"YYYY-MM-DD"）。
//
// 为什么不用 $dateFromString：库里的时间是 "2006-01-02 15:04:05" 字符串，
// 由服务端按配置时区（默认 Asia/Shanghai）写入，前缀就是当地日期；
// 直接截前 10 个字符既不会引入时区二次换算，也不会因为个别文档格式异常
// （短字符串 / 非字符串）让整个聚合报错 —— $cond 兜到空串，再由下游 $match 剔除。
func dayExpr(timeField string) bson.M {
	asString := bson.M{"$convert": bson.M{
		"input":   "$" + timeField,
		"to":      "string",
		"onError": "",
		"onNull":  "",
	}}
	return bson.M{"$cond": bson.A{
		bson.M{"$gte": bson.A{bson.M{"$strLenCP": asString}, 10}},
		bson.M{"$substrCP": bson.A{asString, 0, 10}},
		"",
	}}
}

// trendMatch 拼趋势的时间窗口 + 业务条件。
//
// 单个条件时不套 $and，保持计划干净（$match 直接吃 {time: {$gte,$lte}} 的索引）；
// base 本身是 $and 时把条件摊平，避免出现 $and[$and[...]] 这种嵌套，
// 让计划器能直接把所有等值条件一起匹配到复合索引上。
func trendMatch(timeField string, base bson.M, startStr, endStr string) bson.M {
	window := bson.M{timeField: bson.M{"$gte": startStr, "$lte": endStr}}
	conds := bson.A{window}

	switch {
	case len(base) == 0:
	case len(base) == 1:
		if nested, ok := base["$and"].(bson.A); ok {
			conds = append(conds, nested...)
			break
		}
		conds = append(conds, base)
	default:
		conds = append(conds, base)
	}

	if len(conds) == 1 {
		return window
	}
	return bson.M{"$and": conds}
}

// trendPipeline 按天聚合管道
func trendPipeline(timeField string, base bson.M, startStr, endStr string) mongo.Pipeline {
	return mongo.Pipeline{
		{{Key: "$match", Value: trendMatch(timeField, base, startStr, endStr)}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: dayExpr(timeField)},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
		// 时间字段缺失/太短的文档会落到空串上，统计里没有意义
		{{Key: "$match", Value: bson.M{"_id": bson.M{"$ne": ""}}}},
	}
}

// TrendByDay 执行按天聚合，返回 {日期: 计数}。
// 没有数据的日期不会出现在结果里，补零由 service 层做（它才知道要补哪几天）。
func (r *repository) TrendByDay(ctx context.Context, collection, timeField string, base bson.M, startStr, endStr string) (map[string]int64, error) {
	if !allowedCollections[collection] {
		return nil, fmt.Errorf("dashboard: collection %q is not allowed for trend", collection)
	}
	if strings.TrimSpace(timeField) == "" {
		return nil, fmt.Errorf("dashboard: empty time field")
	}

	cursor, err := r.db.Collection(collection).Aggregate(ctx, trendPipeline(timeField, base, startStr, endStr),
		options.Aggregate().SetAllowDiskUse(true))
	if err != nil {
		return nil, fmt.Errorf("dashboard: trend aggregate on %s: %w", collection, err)
	}
	defer cursor.Close(ctx)

	var rows []struct {
		Day   string `bson:"_id"`
		Count int64  `bson:"count"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("dashboard: decode trend rows on %s: %w", collection, err)
	}

	out := make(map[string]int64, len(rows))
	for _, row := range rows {
		out[row.Day] = row.Count
	}
	return out, nil
}

// ExplainTrend 返回趋势聚合的 queryPlanner 计划。
func (r *repository) ExplainTrend(ctx context.Context, collection, timeField string, base bson.M, startStr, endStr string) (bson.M, error) {
	cmd := bson.D{
		{Key: "explain", Value: bson.D{
			{Key: "aggregate", Value: collection},
			{Key: "pipeline", Value: trendPipeline(timeField, base, startStr, endStr)},
			{Key: "cursor", Value: bson.D{}},
		}},
		{Key: "verbosity", Value: "queryPlanner"},
	}
	var out bson.M
	if err := r.db.RunCommand(ctx, cmd).Decode(&out); err != nil {
		return nil, fmt.Errorf("dashboard: explain trend on %s: %w", collection, err)
	}
	return out, nil
}

// NodeHeartbeats 读所有节点心跳。
//
// 用 SCAN 而不是 KEYS（KEYS 在大 key 空间会阻塞 Redis）；
// 拿到 key 列表后用一条 pipeline 批量 HGETALL，避免逐节点往返。
// 节点数量级很小（部署级个位数到几十），这里不做分页。
func (r *repository) NodeHeartbeats(ctx context.Context) ([]NodeHeartbeat, error) {
	if r.redisClient == nil {
		return nil, fmt.Errorf("dashboard: redis client is not initialized")
	}

	keys := make([]string, 0, 16)
	var cursor uint64
	for {
		batch, next, err := r.redisClient.Scan(ctx, cursor, "node:*", 256).Result()
		if err != nil {
			return nil, fmt.Errorf("dashboard: scan node keys: %w", err)
		}
		keys = append(keys, batch...)
		cursor = next
		if cursor == 0 {
			break
		}
	}
	if len(keys) == 0 {
		return []NodeHeartbeat{}, nil
	}

	pipe := r.redisClient.Pipeline()
	cmds := make([]*redisdriver.MapStringStringCmd, len(keys))
	for i, key := range keys {
		cmds[i] = pipe.HGetAll(ctx, key)
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redisdriver.Nil {
		return nil, fmt.Errorf("dashboard: read node heartbeats: %w", err)
	}

	out := make([]NodeHeartbeat, 0, len(keys))
	for i, key := range keys {
		values, err := cmds[i].Result()
		if err != nil {
			continue
		}
		out = append(out, NodeHeartbeat{
			Name:       strings.TrimPrefix(key, "node:"),
			State:      values["state"],
			UpdateTime: values["updateTime"],
		})
	}
	return out, nil
}
