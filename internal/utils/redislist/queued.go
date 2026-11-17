package redislist

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// QueueClient 在 Client 基础上多一个 EXPIRE：任务队列需要滑动 TTL。
type QueueClient interface {
	Client
	Expire(ctx context.Context, key string, ttl time.Duration) *redis.BoolCmd
}

// PushQueued 追加到队列尾部，并刷新整条队列的存活时间（滑动 TTL）。
//
// issue #200：TaskInfo:<taskID>、NodeTask:<node> 这类队列只在任务删除时清，
// 节点长期离线 / 被移除时消息会永远留在 Redis 里。加滑动 TTL 后：
//   - 队列一直在被投递的任务，TTL 每次写入都被续上，不会中途过期；
//   - 真正没人消费的队列，最后一条投递后 TTL 到期自动回收；
//   - 节点重新上线会走 checkNodeTask 重新投递待开始/运行中的任务，
//     所以过期丢掉的只是“早就没人领的旧消息”。
func PushQueued(ctx context.Context, c QueueClient, key string, ttl time.Duration, values ...interface{}) error {
	return pushQueued(ctx, c, key, ttl, false, values...)
}

// PushQueuedHead 与 PushQueued 相同，但用 LPush（最新在前）语义。
// 任务目标列表历史上就是 LPush，保留顺序以免节点侧消费顺序变化。
func PushQueuedHead(ctx context.Context, c QueueClient, key string, ttl time.Duration, values ...interface{}) error {
	return pushQueued(ctx, c, key, ttl, true, values...)
}

func pushQueued(ctx context.Context, c QueueClient, key string, ttl time.Duration, head bool, values ...interface{}) error {
	if len(values) == 0 {
		return nil
	}
	var err error
	if head {
		err = c.LPush(ctx, key, values...).Err()
	} else {
		err = c.RPush(ctx, key, values...).Err()
	}
	if err != nil {
		return fmt.Errorf("failed to push %q: %w", key, err)
	}
	if ttl > 0 {
		if err := c.Expire(ctx, key, ttl).Err(); err != nil {
			return fmt.Errorf("failed to set ttl on %q: %w", key, err)
		}
	}
	return nil
}

// PushQueuedCapped 追加到队列尾部，同时限制长度并设置滑动 TTL。
//
// 配置刷新这类「状态型」消息既不需要无限堆积（节点只关心最新的几条），
// 也不该永久留在 Redis 里（节点被删/长期离线就是垃圾）。
func PushQueuedCapped(ctx context.Context, c QueueClient, key string, maxLen int64, ttl time.Duration, values ...interface{}) error {
	if err := PushQueued(ctx, c, key, ttl, values...); err != nil {
		return err
	}
	if maxLen <= 0 {
		maxLen = DefaultMaxLen
	}
	if err := c.LTrim(ctx, key, -maxLen, -1).Err(); err != nil {
		return fmt.Errorf("failed to trim %q to %d entries: %w", key, maxLen, err)
	}
	return nil
}
