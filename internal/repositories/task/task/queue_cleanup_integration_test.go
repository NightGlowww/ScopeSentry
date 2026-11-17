//go:build integration

package task

import (
	"context"
	"fmt"
	"testing"
	"time"

	redisdb "github.com/Autumn-27/ScopeSentry/internal/database/redis"
)

// 停止/删除任务时，队列里还没被领取的消息必须一起清掉，否则节点重新上线还会把任务跑起来。
func TestRemoveQueuedNodeTasks(t *testing.T) {
	if redisdb.Client == nil {
		t.Skip("redis not initialized")
	}
	ctx := context.Background()
	key := fmt.Sprintf("NodeTask:test-%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = redisdb.Client.Del(context.Background(), key).Err() })

	payloads := []string{
		`{"id":"taskA","name":"a"}`,
		`{"id":"taskB","name":"b"}`,
		`{"ID":"page_monitoring","type":"page_monitoring"}`,
	}
	for _, p := range payloads {
		if err := redisdb.Client.RPush(ctx, key, p).Err(); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	repo := NewRepository()
	removed, err := repo.RemoveQueuedNodeTasks(ctx, "taskA")
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}

	left, err := redisdb.Client.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		t.Fatalf("lrange: %v", err)
	}
	if len(left) != 2 {
		t.Fatalf("remaining = %v, want 2 entries", left)
	}
	for _, v := range left {
		if v == payloads[0] {
			t.Fatalf("stopped task payload still queued")
		}
	}

	// 幂等：再清一次没有可删的，返回 0 且不报错
	removed, err = repo.RemoveQueuedNodeTasks(ctx, "taskA")
	if err != nil || removed != 0 {
		t.Fatalf("second remove = %d, err = %v; want 0, nil", removed, err)
	}
}
