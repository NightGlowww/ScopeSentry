package export

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

// 批量导出（对标 ARL-Next batchExport / export）的查询执行层。
//
// 这一层只做「按描述发查询 + 把游标交回去」，type -> 集合/管道/字段的语义在
// services/export 里（schema.go）。导出永远走游标，不落 All/ReadAll。

// Mode 查询形态。
type Mode string

const (
	// ModeFind 逐条读（Find + Sort + Projection）。
	ModeFind Mode = "find"
	// ModeAggregate 聚合读（Aggregate，必须 allowDiskUse）。
	ModeAggregate Mode = "aggregate"
)

// BatchQuery 一次导出查询的描述。
type BatchQuery struct {
	Collection string
	Mode       Mode
	Filter     bson.M
	Sort       bson.D
	Projection bson.D
	Pipeline   mongo.Pipeline
	// Limit > 0 时限制返回行数（find 模式；aggregate 模式用 $limit 阶段）
	Limit int64
}

// BatchRepository 批量导出的只读仓库。
type BatchRepository interface {
	// Cursor 打开游标（调用方负责 Close）。
	Cursor(ctx context.Context, q BatchQuery) (*mongo.Cursor, error)
	// CountDocuments 计数，limit>0 时最多数到 limit 就停（用于行数上限的提前判定）。
	CountDocuments(ctx context.Context, collection string, filter bson.M, limit int64) (int64, error)
	// CountPipeline 聚合计数：pipeline 末尾必须已接 $count。
	CountPipeline(ctx context.Context, collection string, pipeline mongo.Pipeline) (int64, error)
	// Explain 查询的 explain（queryPlanner 精度），用于取证「导出走索引」。
	Explain(ctx context.Context, q BatchQuery) (bson.M, error)
	// FindTask 按任务 id 取任务；不存在返回 (nil, nil)。
	FindTask(ctx context.Context, taskID string) (*models.Task, error)
}

type batchRepository struct {
	db *mongo.Database
}

// NewBatchRepository 创建批量导出仓库实例。
func NewBatchRepository() BatchRepository {
	return &batchRepository{db: mongodb.DB}
}

// Cursor 打开游标。find 走 {project|taskName,time:-1} 索引直接出顺序；
// aggregate 走管道，统一 SetAllowDiskUse(true)——聚合结果可能超过 100MB 内存上限。
func (r *batchRepository) Cursor(ctx context.Context, q BatchQuery) (*mongo.Cursor, error) {
	coll := r.db.Collection(q.Collection)
	switch q.Mode {
	case ModeAggregate:
		opts := options.Aggregate().SetAllowDiskUse(true)
		cursor, err := coll.Aggregate(ctx, q.Pipeline, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to aggregate %s: %w", q.Collection, err)
		}
		return cursor, nil
	default:
		opts := options.Find()
		if len(q.Sort) > 0 {
			opts.SetSort(q.Sort)
		}
		if len(q.Projection) > 0 {
			opts.SetProjection(q.Projection)
		}
		// 预览等场景会传 limit：必须在查询层截断，否则会把整个集合拉回内存。
		if q.Limit > 0 {
			opts.SetLimit(q.Limit)
		}
		// 批量大小与游标分批一致：一次 1000 条，边读边写，不把结果集堆在内存里。
		opts.SetBatchSize(batchCursorSize)
		cursor, err := coll.Find(ctx, q.Filter, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to find %s: %w", q.Collection, err)
		}
		return cursor, nil
	}
}

// batchCursorSize 游标每次从 Mongo 拉的文档数。
const batchCursorSize = 1000

// CountDocuments 计数。limit > 0 时 count 命令带 limit，Mongo 数到上限即停，
// 不需要扫全集合就能判定「超过导出行数上限」。
func (r *batchRepository) CountDocuments(ctx context.Context, collection string, filter bson.M, limit int64) (int64, error) {
	opts := options.Count()
	if limit > 0 {
		opts.SetLimit(limit)
	}
	total, err := r.db.Collection(collection).CountDocuments(ctx, filter, opts)
	if err != nil {
		return 0, fmt.Errorf("failed to count %s: %w", collection, err)
	}
	return total, nil
}

// CountPipeline 聚合计数（pipeline 末尾必须是 $count: "n"）。
func (r *batchRepository) CountPipeline(ctx context.Context, collection string, pipeline mongo.Pipeline) (int64, error) {
	opts := options.Aggregate().SetAllowDiskUse(true)
	cursor, err := r.db.Collection(collection).Aggregate(ctx, pipeline, opts)
	if err != nil {
		return 0, fmt.Errorf("failed to count aggregate %s: %w", collection, err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	// 计数结果只有一条文档，这里也不用 All/ReadAll：保持「导出链路上不缓冲结果集」的约束。
	if !cursor.Next(ctx) {
		if err := cursor.Err(); err != nil {
			return 0, fmt.Errorf("failed to read aggregate count on %s: %w", collection, err)
		}
		return 0, nil
	}
	var out struct {
		N int64 `bson:"n"`
	}
	if err := cursor.Decode(&out); err != nil {
		return 0, fmt.Errorf("failed to decode aggregate count on %s: %w", collection, err)
	}
	return out.N, nil
}

// Explain 查询 explain（queryPlanner）。
//
// find 形态直接 explain find 命令；aggregate 形态 explain aggregate 命令——
// 两种形态的 $match 都要求落到索引上（测试里断言 winningPlan 含 IXSCAN、无 SORT）。
func (r *batchRepository) Explain(ctx context.Context, q BatchQuery) (bson.M, error) {
	var inner bson.D
	switch q.Mode {
	case ModeAggregate:
		inner = bson.D{
			{Key: "aggregate", Value: q.Collection},
			{Key: "pipeline", Value: q.Pipeline},
			{Key: "cursor", Value: bson.D{}},
		}
	default:
		inner = bson.D{{Key: "find", Value: q.Collection}}
		if len(q.Filter) > 0 {
			inner = append(inner, bson.E{Key: "filter", Value: q.Filter})
		}
		if len(q.Sort) > 0 {
			inner = append(inner, bson.E{Key: "sort", Value: q.Sort})
		}
		if len(q.Projection) > 0 {
			inner = append(inner, bson.E{Key: "projection", Value: q.Projection})
		}
		inner = append(inner, bson.E{Key: "batchSize", Value: batchCursorSize})
	}

	cmd := bson.D{
		{Key: "explain", Value: inner},
		{Key: "verbosity", Value: "queryPlanner"},
	}
	var out bson.M
	if err := r.db.RunCommand(ctx, cmd).Decode(&out); err != nil {
		return nil, fmt.Errorf("failed to explain %s: %w", q.Collection, err)
	}
	return out, nil
}

// FindTask 按任务 id 查任务。
//
// 任务集合里业务 id 在 "id" 字段（Task.TaskID），历史/手工数据可能只有 ObjectID，
// 所以两条路都试：先 id，再 _id（当传入的是 24 位 hex 时）。
func (r *batchRepository) FindTask(ctx context.Context, taskID string) (*models.Task, error) {
	coll := r.db.Collection("task")
	var task models.Task
	err := coll.FindOne(ctx, bson.M{"id": taskID}).Decode(&task)
	if err == nil {
		return &task, nil
	}
	if err != mongo.ErrNoDocuments {
		return nil, fmt.Errorf("failed to find task %s: %w", taskID, err)
	}
	oid, parseErr := primitive.ObjectIDFromHex(taskID)
	if parseErr != nil {
		return nil, nil
	}
	if err := coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&task); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find task %s by _id: %w", taskID, err)
	}
	return &task, nil
}
