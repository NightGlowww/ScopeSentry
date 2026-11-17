package task

import (
	"context"
	"fmt"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// targetControlCollection 任务目标暂停记录（issue #277）
const targetControlCollection = "TaskTargetControl"

// AddTargetControl 记录某个目标被暂停；同一个 (taskId, target) 幂等。
func (r *repository) AddTargetControl(ctx context.Context, taskID, target string) error {
	if r.db == nil {
		return fmt.Errorf("database is not initialized")
	}
	_, err := r.db.Collection(targetControlCollection).UpdateOne(
		ctx,
		bson.M{"taskId": taskID, "target": target},
		bson.M{"$set": bson.M{"taskId": taskID, "target": target, "pausedAt": time.Now()}},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("failed to add target control: %w", err)
	}
	return nil
}

// RemoveTargetControl 删除暂停记录（恢复目标时调用）。
func (r *repository) RemoveTargetControl(ctx context.Context, taskID, target string) error {
	if r.db == nil {
		return fmt.Errorf("database is not initialized")
	}
	_, err := r.db.Collection(targetControlCollection).DeleteMany(ctx, bson.M{"taskId": taskID, "target": target})
	if err != nil {
		return fmt.Errorf("failed to remove target control: %w", err)
	}
	return nil
}

// ListTargetControls 返回某个任务里被暂停的目标。
func (r *repository) ListTargetControls(ctx context.Context, taskID string) ([]models.TaskTargetControl, error) {
	if r.db == nil {
		return nil, nil
	}
	cursor, err := r.db.Collection(targetControlCollection).Find(ctx, bson.M{"taskId": taskID})
	if err != nil {
		return nil, fmt.Errorf("failed to list target controls: %w", err)
	}
	defer cursor.Close(ctx)
	var result []models.TaskTargetControl
	if err := cursor.All(ctx, &result); err != nil {
		return nil, fmt.Errorf("failed to decode target controls: %w", err)
	}
	return result, nil
}

// RemoveTaskTarget 从 TaskInfo:<taskId> 里摘掉一个还没被领取的目标，返回是否真的摘到。
func (r *repository) RemoveTaskTarget(ctx context.Context, taskID, target string) (int64, error) {
	if r.redisClient == nil {
		return 0, fmt.Errorf("redis is not initialized")
	}
	key := fmt.Sprintf("TaskInfo:%s", taskID)
	n, err := r.redisClient.LRem(ctx, key, 0, target).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to remove task target: %w", err)
	}
	return n, nil
}

// PrependTaskTarget 把目标放回队列头部（恢复暂停时优先重扫）。
func (r *repository) PrependTaskTarget(ctx context.Context, taskID, target string) error {
	if r.redisClient == nil {
		return fmt.Errorf("redis is not initialized")
	}
	key := fmt.Sprintf("TaskInfo:%s", taskID)
	if err := r.redisClient.LPush(ctx, key, target).Err(); err != nil {
		return fmt.Errorf("failed to prepend task target: %w", err)
	}
	// 队列也要续期，避免恢复的目标因为 TTL 到期丢掉
	return r.redisClient.Expire(ctx, key, TaskQueueTTL).Err()
}
