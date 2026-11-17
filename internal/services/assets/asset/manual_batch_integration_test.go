//go:build integration

package asset

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
)

// 批量录入要能区分「成功 / 重复 / 非法」，前端才能给出准确提示；
// 共享的 project/tags 要落到每条成功的记录上。
func TestAddAssetsBatch(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	svc := NewService()
	prefix := fmt.Sprintf("batch-%d", time.Now().UnixNano())
	hostA := prefix + "-a.example.com"
	hostB := prefix + "-b.example.com"
	hostC := prefix + "-c.example.com"
	hosts := []string{hostA, hostB, hostC}
	t.Cleanup(func() {
		_, _ = mongodb.DB.Collection("asset").DeleteMany(context.Background(), bson.M{"host": bson.M{"$in": hosts}})
	})

	if _, err := svc.AddAsset(newGinCtx(), &models.AssetAddRequest{Host: hostA, Port: "443"}); err != nil {
		t.Fatalf("seed existing: %v", err)
	}

	result, err := svc.AddAssets(newGinCtx(), &models.AssetAddBatchRequest{
		Project: "proj-batch",
		Tags:    []string{"batch"},
		Items: []models.AssetAddRequest{
			{Host: hostA, Port: "443"}, // 重复
			{Host: hostB, Port: "8443"},
			{Host: hostC},
			{Host: "   "}, // 非法
		},
	})
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if result.Inserted != 2 || result.Duplicated != 1 || result.Invalid != 1 {
		t.Fatalf("result = %+v, want 2 inserted / 1 duplicated / 1 invalid", result)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("errors = %v, want 1 entry", result.Errors)
	}

	cursor, err := mongodb.DB.Collection("asset").Find(context.Background(), bson.M{"host": bson.M{"$in": []string{hostB, hostC}}})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	var docs []models.Asset
	if err := cursor.All(context.Background(), &docs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("inserted docs = %d, want 2", len(docs))
	}
	for _, doc := range docs {
		if doc.Project != "proj-batch" {
			t.Errorf("%s project = %q, want proj-batch", doc.Host, doc.Project)
		}
		if len(doc.Tags) != 1 || doc.Tags[0] != "batch" {
			t.Errorf("%s tags = %v, want [batch]", doc.Host, doc.Tags)
		}
	}
}
