//go:build integration

package mongodb

import (
	"context"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

// 这些集合的列表接口都按 time 倒序分页；没有 time 索引时会退化成全集合 SORT。
// 实测 20 万条：29ms（SORT，docsExamined=200000）→ 3ms（LIMIT，docsExamined=20）。
func TestLargeResultCollectionsHaveTimeIndex(t *testing.T) {
	if DB == nil {
		t.Skip("mongo not initialized")
	}
	if err := createIndexes(DB); err != nil {
		t.Fatalf("createIndexes: %v", err)
	}

	collections := []string{"UrlScan", "crawler", "SensitiveResult", "DirScanResult", "vulnerability", "PageMonitoring", "PocList"}
	for _, name := range collections {
		t.Run(name, func(t *testing.T) {
			cursor, err := DB.Collection(name).Indexes().List(context.Background())
			if err != nil {
				t.Fatalf("list indexes: %v", err)
			}
			defer cursor.Close(context.Background())

			found := false
			for cursor.Next(context.Background()) {
				var spec struct {
					Key bson.D `bson:"key"`
				}
				if err := cursor.Decode(&spec); err != nil {
					t.Fatalf("decode index spec: %v", err)
				}
				if len(spec.Key) == 1 && spec.Key[0].Key == "time" {
					found = true
				}
			}
			if err := cursor.Err(); err != nil {
				t.Fatalf("iterate indexes: %v", err)
			}
			if !found {
				t.Fatalf("%s has no time index", name)
			}
		})
	}
}
