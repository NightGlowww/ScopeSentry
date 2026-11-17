//go:build integration

package tagcolor

import (
	"context"
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"go.mongodb.org/mongo-driver/bson"
)

// issue #217：标签颜色覆盖要能按 tag 覆盖写、能列出、能删掉回到默认配色。
func TestTagColorRoundTrip(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	ctx := context.Background()
	repo := NewRepository()
	tag := "tagcolor-test-" + t.Name()
	t.Cleanup(func() { _ = repo.Delete(context.Background(), tag) })
	_ = repo.Delete(ctx, tag)

	if err := repo.Set(ctx, tag, "success"); err != nil {
		t.Fatalf("set: %v", err)
	}
	colors, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if colors[tag] != "success" {
		t.Fatalf("colors[%q] = %q, want success", tag, colors[tag])
	}

	// 同 tag 再写一次是覆盖，不是新增
	if err := repo.Set(ctx, tag, "#ff8800"); err != nil {
		t.Fatalf("set again: %v", err)
	}
	count, err := mongodb.DB.Collection(CollectionName).CountDocuments(ctx, bson.M{"tag": tag})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("document count = %d, want 1 (upsert should overwrite)", count)
	}
	colors, err = repo.List(ctx)
	if err != nil {
		t.Fatalf("list after overwrite: %v", err)
	}
	if colors[tag] != "#ff8800" {
		t.Fatalf("colors[%q] = %q, want #ff8800", tag, colors[tag])
	}

	if err := repo.Delete(ctx, tag); err != nil {
		t.Fatalf("delete: %v", err)
	}
	colors, err = repo.List(ctx)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if _, exists := colors[tag]; exists {
		t.Fatalf("override still present after delete")
	}
}
