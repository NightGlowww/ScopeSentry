// Package redislist 提供带长度上限的 Redis 日志列表写入。
//
// 背景（issue #285）：节点日志、插件日志都是直接 RPush/LPush 追加的 list，
// 没有任何长度上限；节点离线、插件刷屏时 key 会无限增长，前端再用 LRange 0 -1
// 拉全量，既拖慢接口也吃内存。
//
// 旧实现在订阅端用「计数到阈值就 DEL 整个 key」兜底：会把历史日志一次性清空，
// 而且计数只存在于单进程内存里——重启归零、多实例各数一份，实际并不可靠。
//
// 这里改成每次写入后跟一次 LTRIM，只保留最新 N 条：
//   - 上限对每个 key 恒定，不依赖进程内状态；
//   - 重启、多实例都成立；
//   - 不会出现「整段日志突然消失」。
package redislist

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// DefaultMaxLen 是配置缺省/非法时使用的每 key 保留条数。
const DefaultMaxLen int64 = 1000

// Client 只声明用到的三个命令，既能在真实 *redis.Client 上工作，
// 也便于用假对象做单测（不依赖 Mongo/Redis）。
type Client interface {
	RPush(ctx context.Context, key string, values ...interface{}) *redis.IntCmd
	LPush(ctx context.Context, key string, values ...interface{}) *redis.IntCmd
	LTrim(ctx context.Context, key string, start, stop int64) *redis.StatusCmd
}

// MaxLenFromConfig 把配置值收敛成可用的上限。
// 配置缺失、被填成 0 或负数时回落到 DefaultMaxLen，避免退化成「不裁剪」。
func MaxLenFromConfig(configured int) int64 {
	if configured <= 0 {
		return DefaultMaxLen
	}
	return int64(configured)
}

// PushCapped 追加到列表尾部（RPush 语义）后只保留最新 maxLen 条。
func PushCapped(ctx context.Context, c Client, key string, maxLen int64, values ...interface{}) error {
	if len(values) == 0 {
		return nil
	}
	if maxLen <= 0 {
		maxLen = DefaultMaxLen
	}
	if err := c.RPush(ctx, key, values...).Err(); err != nil {
		return fmt.Errorf("failed to push %q: %w", key, err)
	}
	// 尾部是最新的一条，保留最后 maxLen 条
	if err := c.LTrim(ctx, key, -maxLen, -1).Err(); err != nil {
		return fmt.Errorf("failed to trim %q to %d entries: %w", key, maxLen, err)
	}
	return nil
}

// PushHeadCapped 追加到列表头部（LPush 语义，最新的在前）后只保留 maxLen 条。
func PushHeadCapped(ctx context.Context, c Client, key string, maxLen int64, values ...interface{}) error {
	if len(values) == 0 {
		return nil
	}
	if maxLen <= 0 {
		maxLen = DefaultMaxLen
	}
	if err := c.LPush(ctx, key, values...).Err(); err != nil {
		return fmt.Errorf("failed to push %q: %w", key, err)
	}
	// 头部是最新的一条，保留最前 maxLen 条
	if err := c.LTrim(ctx, key, 0, maxLen-1).Err(); err != nil {
		return fmt.Errorf("failed to trim %q to %d entries: %w", key, maxLen, err)
	}
	return nil
}
