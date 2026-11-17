package redis

import (
	"context"
	"fmt"
	"github.com/Autumn-27/ScopeSentry/internal/logger"

	"github.com/Autumn-27/ScopeSentry/internal/config"
	"github.com/Autumn-27/ScopeSentry/internal/utils/redislist"
	"github.com/redis/go-redis/v9"
)

var Client *redis.Client

func init() {
	// 创建 Redis 客户端
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", config.GlobalConfig.Redis.IP, config.GlobalConfig.Redis.Port),
		Password: config.GlobalConfig.Redis.Password,
		DB:       0,
	})

	// 测试连接
	if err := client.Ping(context.Background()).Err(); err != nil {
		logger.Error(fmt.Sprintf("failed to connect to Redis: %v", err))
		return
	}

	Client = client
	logger.Info("Redis connected successfully")
	return
}

func Close() error {
	if Client != nil {
		if err := Client.Close(); err != nil {
			return fmt.Errorf("failed to close Redis connection: %v", err)
		}
	}
	return nil
}

// SendPluginLogToRedis 追加一条插件日志。
//
// 插件日志同样是无限增长的 list（issue #285）：插件一直输出时 key 会越堆越大，
// 前端一次拉全量会把服务拖慢。这里与节点日志共用同一个上限配置
// （logs.total_logs），每次写入后只保留最新 N 条。
func SendPluginLogToRedis(key string, msg string) error {
	if Client == nil {
		return fmt.Errorf("redis client is not initialized")
	}
	maxLen := redislist.MaxLenFromConfig(config.GetTotalLogs())
	if err := redislist.PushCapped(context.Background(), Client, key, maxLen, msg); err != nil {
		return fmt.Errorf("failed to add values to list: %w", err)
	}
	return nil
}
