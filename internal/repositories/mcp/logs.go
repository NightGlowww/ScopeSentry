package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	mongodriver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const logsCollection = "mcp_call_logs"

// LogRepository MCP 调用审计日志
type LogRepository interface {
	// InsertLogs 批量写入（批量器调用，ordered=false）
	InsertLogs(ctx context.Context, logs []models.McpCallLog) error
	// ListLogs 分页查询（createdAt 倒序）
	ListLogs(ctx context.Context, q models.McpLogQuery) ([]models.McpCallLog, int64, error)
	// IterateLogs 游标遍历（CSV 导出用，不把全量结果读进内存）
	IterateLogs(ctx context.Context, q models.McpLogQuery, limit int, fn func(*models.McpCallLog) error) error
	// CountLogs 统计
	CountLogs(ctx context.Context, q models.McpLogQuery) (int64, error)
	// TrimLogs 只保留最近 keep 条，返回删除条数
	TrimLogs(ctx context.Context, keep int64) (int64, error)
}

type logRepository struct{}

// NewLogRepository 创建日志仓库。
// 集合按调用期解析：DB 未初始化时不在包初始化阶段 panic。
func NewLogRepository() LogRepository {
	return &logRepository{}
}

func (r *logRepository) coll() (*mongodriver.Collection, error) {
	if mongodb.DB == nil {
		return nil, fmt.Errorf("mcp_call_logs: database not initialized")
	}
	return mongodb.DB.Collection(logsCollection), nil
}

// InsertLogs 批量写入；单条文档失败不阻塞其它文档（ordered=false）
func (r *logRepository) InsertLogs(ctx context.Context, logs []models.McpCallLog) error {
	if len(logs) == 0 {
		return nil
	}
	collection, err := r.coll()
	if err != nil {
		return err
	}
	docs := make([]interface{}, 0, len(logs))
	for i := range logs {
		if logs[i].CreatedAt.IsZero() {
			logs[i].CreatedAt = time.Now()
		}
		docs = append(docs, logs[i])
	}
	if _, err := collection.InsertMany(ctx, docs, options.InsertMany().SetOrdered(false)); err != nil {
		return fmt.Errorf("failed to insert mcp call logs: %w", err)
	}
	return nil
}

// buildLogFilter 统一构造过滤条件（列表 / 导出 / 计数共用，保证三者口径一致）
func buildLogFilter(q models.McpLogQuery) bson.M {
	filter := bson.M{}
	if q.KeyID != "" {
		filter["keyId"] = q.KeyID
	}
	if q.Tool != "" {
		filter["tool"] = q.Tool
	}
	if q.Status != "" {
		filter["status"] = q.Status
	}
	if q.From != nil || q.To != nil {
		rangeCond := bson.M{}
		if q.From != nil {
			rangeCond["$gte"] = *q.From
		}
		if q.To != nil {
			rangeCond["$lte"] = *q.To
		}
		filter["createdAt"] = rangeCond
	}
	return filter
}

// ListLogs 分页查询（走 {createdAt:-1} / {keyId:1,createdAt:-1} 索引）
func (r *logRepository) ListLogs(ctx context.Context, q models.McpLogQuery) ([]models.McpCallLog, int64, error) {
	collection, err := r.coll()
	if err != nil {
		return nil, 0, err
	}
	pageIndex, pageSize := normalizePage(q.PageIndex, q.PageSize)
	filter := buildLogFilter(q)

	total, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count mcp call logs: %w", err)
	}
	opts := options.Find().
		SetSkip(int64((pageIndex - 1) * pageSize)).
		SetLimit(int64(pageSize)).
		SetSort(bson.D{{Key: "createdAt", Value: -1}})
	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list mcp call logs: %w", err)
	}
	defer cursor.Close(ctx)

	var logs []models.McpCallLog
	if err := cursor.All(ctx, &logs); err != nil {
		return nil, 0, fmt.Errorf("failed to decode mcp call logs: %w", err)
	}
	return logs, total, nil
}

// IterateLogs 游标流式遍历，limit<=0 表示不限制
func (r *logRepository) IterateLogs(ctx context.Context, q models.McpLogQuery, limit int, fn func(*models.McpCallLog) error) error {
	collection, err := r.coll()
	if err != nil {
		return err
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: -1}}).
		SetBatchSize(1000)
	if limit > 0 {
		opts.SetLimit(int64(limit))
	}
	cursor, err := collection.Find(ctx, buildLogFilter(q), opts)
	if err != nil {
		return fmt.Errorf("failed to iterate mcp call logs: %w", err)
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var row models.McpCallLog
		if err := cursor.Decode(&row); err != nil {
			return fmt.Errorf("failed to decode mcp call log: %w", err)
		}
		if err := fn(&row); err != nil {
			return err
		}
	}
	return cursor.Err()
}

// CountLogs 统计
func (r *logRepository) CountLogs(ctx context.Context, q models.McpLogQuery) (int64, error) {
	collection, err := r.coll()
	if err != nil {
		return 0, err
	}
	return collection.CountDocuments(ctx, buildLogFilter(q))
}

// TrimLogs 找到第 keep 条的时间分界，删除更早的记录。
// createdAt 上有索引，Find+Skip 只需扫 keep+1 条索引项。
func (r *logRepository) TrimLogs(ctx context.Context, keep int64) (int64, error) {
	if keep <= 0 {
		return 0, nil
	}
	collection, err := r.coll()
	if err != nil {
		return 0, err
	}
	var boundary struct {
		CreatedAt time.Time `bson:"createdAt"`
	}
	err = collection.FindOne(ctx,
		bson.M{},
		options.FindOne().
			SetSort(bson.D{{Key: "createdAt", Value: -1}}).
			SetSkip(keep-1).
			SetProjection(bson.M{"createdAt": 1}),
	).Decode(&boundary)
	if err != nil {
		if err == mongodriver.ErrNoDocuments {
			return 0, nil // 总量还没到上限
		}
		return 0, fmt.Errorf("failed to locate mcp call log trim boundary: %w", err)
	}
	res, err := collection.DeleteMany(ctx, bson.M{"createdAt": bson.M{"$lt": boundary.CreatedAt}})
	if err != nil {
		return 0, fmt.Errorf("failed to trim mcp call logs: %w", err)
	}
	return res.DeletedCount, nil
}

func normalizePage(pageIndex, pageSize int) (int, int) {
	if pageIndex < 1 {
		pageIndex = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > models.McpMaxPageSize {
		pageSize = models.McpMaxPageSize
	}
	return pageIndex, pageSize
}
