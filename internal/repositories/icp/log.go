package icp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// icp_task_log 读写。
//
// 日志是任务可观测性的唯一来源：provider 重试、暂停原因、同步/补全结果都写在这里，
// 前端用 log 端点轮询最近 200 行（不落 println，也不混进进程日志）。

// 日志级别
const (
	IcpLogLevelInfo  = "info"
	IcpLogLevelWarn  = "warn"
	IcpLogLevelError = "error"
)

// LogRepository icp_task_log 数据访问
type LogRepository interface {
	Append(ctx context.Context, taskID, level, line string) error
	// Recent 取最近 limit 行，按时间升序返回（直接可以渲染）
	Recent(ctx context.Context, taskID string, limit int) ([]models.IcpTaskLog, error)
	DeleteByTaskIDs(ctx context.Context, taskIDs []string) (int64, error)
}

type logRepository struct{}

// NewLogRepository 创建日志仓库
func NewLogRepository() LogRepository { return &logRepository{} }

func (r *logRepository) coll() (*mongo.Collection, error) {
	if mongodb.DB == nil {
		return nil, errors.New("icp_task_log: database not initialized")
	}
	return mongodb.DB.Collection(CollectionLog), nil
}

func (r *logRepository) Append(ctx context.Context, taskID, level, line string) error {
	collection, err := r.coll()
	if err != nil {
		return err
	}
	entry := models.IcpTaskLog{
		TaskID:    strings.TrimSpace(taskID),
		Level:     level,
		Line:      line,
		CreatedAt: time.Now(),
	}
	if _, err := collection.InsertOne(ctx, entry); err != nil {
		return fmt.Errorf("icp_task_log: append: %w", err)
	}
	return nil
}

func (r *logRepository) Recent(ctx context.Context, taskID string, limit int) ([]models.IcpTaskLog, error) {
	collection, err := r.coll()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > models.IcpMaxLogLines {
		limit = models.IcpMaxLogLines
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}).
		SetLimit(int64(limit))
	cursor, err := collection.Find(ctx, bson.M{"taskId": strings.TrimSpace(taskID)}, opts)
	if err != nil {
		return nil, fmt.Errorf("icp_task_log: recent: %w", err)
	}
	defer cursor.Close(ctx)
	var items []models.IcpTaskLog
	if err := cursor.All(ctx, &items); err != nil {
		return nil, fmt.Errorf("icp_task_log: recent decode: %w", err)
	}
	// 倒序取最近 N 条，再翻回升序，前端无需自己排序
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	return items, nil
}

func (r *logRepository) DeleteByTaskIDs(ctx context.Context, taskIDs []string) (int64, error) {
	collection, err := r.coll()
	if err != nil {
		return 0, err
	}
	ids := make([]string, 0, len(taskIDs))
	for _, id := range taskIDs {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			ids = append(ids, trimmed)
		}
	}
	if len(ids) == 0 {
		return 0, nil
	}
	res, err := collection.DeleteMany(ctx, bson.M{"taskId": bson.M{"$in": ids}})
	if err != nil {
		return 0, fmt.Errorf("icp_task_log: delete: %w", err)
	}
	return res.DeletedCount, nil
}
