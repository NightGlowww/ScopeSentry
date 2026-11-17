package task

import (
	"context"
	"fmt"

	"github.com/Autumn-27/ScopeSentry/internal/services/task/taskmsg"
)

// RemoveQueuedNodeTasks 删除 NodeTask:* 里属于该任务、但还没被节点领取的消息。
//
// 停止/删除任务时只置状态、广播 stop_task 是不够的：任务消息还躺在 Redis 队列里，
// 节点如果在停止之后才上线（或重启），会把这条消息当新任务领走，用户看到的“已停止”任务又跑了起来。
// 这里按 payload 里的 taskID 精确匹配，用 LREM 精确值删除，不影响同一队列里的其它任务。
func (r *repository) RemoveQueuedNodeTasks(ctx context.Context, taskID string) (int64, error) {
	if taskID == "" || r.redisClient == nil {
		return 0, nil
	}
	keys, err := r.redisClient.Keys(ctx, "NodeTask:*").Result()
	if err != nil {
		return 0, fmt.Errorf("failed to list node task queues: %w", err)
	}

	var removed int64
	for _, key := range keys {
		values, err := r.redisClient.LRange(ctx, key, 0, -1).Result()
		if err != nil {
			// 单个队列读失败不影响其它节点
			continue
		}
		for _, value := range values {
			if !taskmsg.BelongsTo([]byte(value), taskID) {
				continue
			}
			n, err := r.redisClient.LRem(ctx, key, 0, value).Result()
			if err != nil {
				continue
			}
			removed += n
		}
	}
	return removed, nil
}
