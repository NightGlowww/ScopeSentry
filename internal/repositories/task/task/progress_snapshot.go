package task

import (
	"context"
	"fmt"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/services/task/tasksnapshot"
	"go.mongodb.org/mongo-driver/bson"
)

// progressSnapshotCollection 存任务完成时的逐目标进度明细（issue #278）。
const progressSnapshotCollection = "TaskProgressSnapshot"

// CollectProgressSnapshot 读取 Redis 里该任务的全部逐目标进度，整理成待落库的行。
func (r *repository) CollectProgressSnapshot(ctx context.Context, taskID string) ([]models.TaskProgressSnapshot, error) {
	prefix := tasksnapshot.KeyPrefix(taskID)
	keys, err := r.redisClient.Keys(ctx, prefix+"*").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list progress keys: %w", err)
	}
	if len(keys) == 0 {
		return nil, nil
	}

	dataByKey := make(map[string]map[string]string, len(keys))
	for _, key := range keys {
		data, err := r.redisClient.HGetAll(ctx, key).Result()
		if err != nil {
			// 单个目标读失败不影响其它目标，快照能救多少是多少
			continue
		}
		dataByKey[key] = data
	}

	return tasksnapshot.BuildRows(taskID, keys, dataByKey), nil
}

// SaveProgressSnapshot 覆盖写入某个任务的进度快照（先删后插，天然幂等）。
func (r *repository) SaveProgressSnapshot(ctx context.Context, taskID string, rows []models.TaskProgressSnapshot) error {
	coll := r.db.Collection(progressSnapshotCollection)
	if _, err := coll.DeleteMany(ctx, bson.M{"taskID": taskID}); err != nil {
		return fmt.Errorf("failed to clear old progress snapshot: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}

	now := time.Now()
	docs := make([]interface{}, 0, len(rows))
	for _, row := range rows {
		row.TaskID = taskID
		row.UpdatedAt = now
		docs = append(docs, row)
	}
	if _, err := coll.InsertMany(ctx, docs); err != nil {
		return fmt.Errorf("failed to insert progress snapshot: %w", err)
	}
	return nil
}

// GetProgressSnapshot 读取某个任务的进度快照，返回 target → 节点写入的哈希。
func (r *repository) GetProgressSnapshot(ctx context.Context, taskID string) (map[string]map[string]string, error) {
	coll := r.db.Collection(progressSnapshotCollection)
	cursor, err := coll.Find(ctx, bson.M{"taskID": taskID})
	if err != nil {
		return nil, fmt.Errorf("failed to query progress snapshot: %w", err)
	}
	defer cursor.Close(ctx)

	result := make(map[string]map[string]string)
	for cursor.Next(ctx) {
		var row models.TaskProgressSnapshot
		if err := cursor.Decode(&row); err != nil {
			return nil, fmt.Errorf("failed to decode progress snapshot: %w", err)
		}
		if row.Target == "" || len(row.Data) == 0 {
			continue
		}
		result[row.Target] = row.Data
	}
	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate progress snapshot: %w", err)
	}
	return result, nil
}

// DeleteProgressSnapshot 删除一批任务的进度快照（删任务时调用）。
func (r *repository) DeleteProgressSnapshot(ctx context.Context, taskIDs ...string) error {
	if len(taskIDs) == 0 {
		return nil
	}
	_, err := r.db.Collection(progressSnapshotCollection).DeleteMany(ctx, bson.M{"taskID": bson.M{"$in": taskIDs}})
	if err != nil {
		return fmt.Errorf("failed to delete progress snapshot: %w", err)
	}
	return nil
}
