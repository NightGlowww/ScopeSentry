//go:build integration

package redislist_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/Autumn-27/ScopeSentry/internal/utils/redislist"
)

// newClient 自建连接而不是复用 internal/database/redis 的全局 Client：
// 这个测试只关心 LTRIM 的真实语义，不该被应用配置加载顺序绑住。
// 连不上就 Skip，本地没起 Redis 时不会误报。
func newClient(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		ip := os.Getenv("REDIS_IP")
		if ip == "" {
			ip = "127.0.0.1"
		}
		port := os.Getenv("REDIS_PORT")
		if port == "" {
			port = "6379"
		}
		addr = net.JoinHostPort(ip, port)
	}
	client := redis.NewClient(&redis.Options{Addr: addr, Password: os.Getenv("REDIS_PASSWORD")})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		t.Skipf("redis not reachable at %s: %v", addr, err)
	}
	return client
}

func cleanupKey(t *testing.T, client *redis.Client, key string) {
	t.Helper()
	t.Cleanup(func() {
		_ = client.Del(context.Background(), key).Err()
		_ = client.Close()
	})
}

func TestPushCappedTrimsToNewestEntries(t *testing.T) {
	client := newClient(t)
	key := fmt.Sprintf("test:redislist:tail:%d", time.Now().UnixNano())
	cleanupKey(t, client, key)

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := redislist.PushCapped(ctx, client, key, 3, fmt.Sprintf("line-%d", i)); err != nil {
			t.Fatalf("push %d: %v", i, err)
		}
	}

	got, err := client.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		t.Fatalf("lrange: %v", err)
	}
	want := []string{"line-2", "line-3", "line-4"}
	if len(got) != len(want) {
		t.Fatalf("list = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("list = %v, want %v", got, want)
		}
	}

	// 上限就是硬上限：长度不会随写入次数增长
	if n, err := client.LLen(ctx, key).Result(); err != nil || n != 3 {
		t.Fatalf("llen = %d (err=%v), want 3", n, err)
	}
}

func TestPushHeadCappedTrimsToNewestEntries(t *testing.T) {
	client := newClient(t)
	key := fmt.Sprintf("test:redislist:head:%d", time.Now().UnixNano())
	cleanupKey(t, client, key)

	ctx := context.Background()
	for i := 0; i < 4; i++ {
		if err := redislist.PushHeadCapped(ctx, client, key, 2, fmt.Sprintf("line-%d", i)); err != nil {
			t.Fatalf("push %d: %v", i, err)
		}
	}

	got, err := client.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		t.Fatalf("lrange: %v", err)
	}
	want := []string{"line-3", "line-2"}
	if len(got) != len(want) {
		t.Fatalf("list = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("list = %v, want %v", got, want)
		}
	}
}
