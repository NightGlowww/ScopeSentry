//go:build integration

package task

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	redisdb "github.com/Autumn-27/ScopeSentry/internal/database/redis"
	"github.com/Autumn-27/ScopeSentry/internal/services/task/tasksnapshot"
)

// 复现 #278：任务完成时 Redis 明细会被删掉，快照必须能独立回放同一份数据。
func TestProgressSnapshotRoundTrip(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	if redisdb.Client == nil {
		t.Skip("redis not initialized")
	}
	ctx := context.Background()
	repo := NewRepository()
	taskID := fmt.Sprintf("snap-%d", time.Now().UnixNano())
	prefix := tasksnapshot.KeyPrefix(taskID)
	keys := []string{prefix + "a.example.com", prefix + "b.example.com"}
	for i, key := range keys {
		if err := redisdb.Client.HSet(ctx, key, "All", fmt.Sprintf("stage-%d", i)).Err(); err != nil {
			t.Fatalf("seed redis: %v", err)
		}
	}
	t.Cleanup(func() {
		_ = redisdb.Client.Del(context.Background(), keys...).Err()
		_ = repo.DeleteProgressSnapshot(context.Background(), taskID)
	})

	rows, err := repo.CollectProgressSnapshot(ctx, taskID)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("collected %d rows, want 2", len(rows))
	}

	if err := repo.SaveProgressSnapshot(ctx, taskID, rows); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := repo.GetProgressSnapshot(ctx, taskID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("snapshot has %d targets, want 2", len(got))
	}
	if got["a.example.com"]["All"] != "stage-0" || got["b.example.com"]["All"] != "stage-1" {
		t.Fatalf("snapshot data mismatch: %+v", got)
	}

	// 覆盖写：再次保存只保留最新一份，不叠加
	if err := repo.SaveProgressSnapshot(ctx, taskID, rows[:1]); err != nil {
		t.Fatalf("save again: %v", err)
	}
	got, err = repo.GetProgressSnapshot(ctx, taskID)
	if err != nil {
		t.Fatalf("get after replace: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("snapshot after replace has %d targets, want 1", len(got))
	}

	if err := repo.DeleteProgressSnapshot(ctx, taskID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, err = repo.GetProgressSnapshot(ctx, taskID)
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("snapshot still has %d targets after delete", len(got))
	}
}
