//go:build integration

package node

import (
	"context"
	"fmt"
	"testing"
	"time"

	redisdb "github.com/Autumn-27/ScopeSentry/internal/database/redis"
)

// 删节点只删 node:<name> 会留下 refresh_config / NodeTask 孤儿 key。
func TestDeleteNodesCleansRelatedQueues(t *testing.T) {
	if redisdb.Client == nil {
		t.Skip("redis not initialized")
	}
	ctx := context.Background()
	name := fmt.Sprintf("node-test-%d", time.Now().UnixNano())
	keys := []string{"node:" + name, "refresh_config:" + name, "NodeTask:" + name}
	other := "refresh_config:keep-me"
	t.Cleanup(func() {
		_ = redisdb.Client.Del(context.Background(), append(keys, other)...).Err()
	})

	for _, key := range keys {
		if err := redisdb.Client.Set(ctx, key, "x", 0).Err(); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}
	if err := redisdb.Client.RPush(ctx, other, "x").Err(); err != nil {
		t.Fatalf("seed other: %v", err)
	}

	repo := NewRepository()
	if err := repo.DeleteNodes(ctx, []string{name}); err != nil {
		t.Fatalf("delete: %v", err)
	}

	for _, key := range keys {
		if n := redisdb.Client.Exists(ctx, key).Val(); n != 0 {
			t.Fatalf("%s still exists after delete", key)
		}
	}
	if n := redisdb.Client.Exists(ctx, other).Val(); n != 1 {
		t.Fatalf("unrelated key was removed")
	}
}

// 配置刷新消息要限长并带 TTL，避免离线节点无限堆积。
func TestRefreshConfigCapsAndExpires(t *testing.T) {
	if redisdb.Client == nil {
		t.Skip("redis not initialized")
	}
	ctx := context.Background()
	name := fmt.Sprintf("node-cap-%d", time.Now().UnixNano())
	key := "refresh_config:" + name
	t.Cleanup(func() { _ = redisdb.Client.Del(context.Background(), key).Err() })

	repo := NewRepository()
	for i := 0; i < refreshConfigMaxLen+20; i++ {
		if err := repo.RefreshConfig(ctx, name, fmt.Sprintf("msg-%d", i)); err != nil {
			t.Fatalf("push %d: %v", i, err)
		}
	}
	if n := redisdb.Client.LLen(ctx, key).Val(); n != refreshConfigMaxLen {
		t.Fatalf("len = %d, want %d", n, refreshConfigMaxLen)
	}
	if ttl := redisdb.Client.TTL(ctx, key).Val(); ttl <= 0 {
		t.Fatalf("ttl = %v, want > 0", ttl)
	}
}
