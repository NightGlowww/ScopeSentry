//go:build integration

package mongodb

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/Autumn-27/ScopeSentry/internal/constants"
)

// 资产包播种需要真实 Mongo（GridFS + 唯一索引），因此挂在 integration 标签下。
// 连接串走 SCOPESENTRY_TEST_MONGODB_URI，未设置则跳过，避免无库环境下红。
//
// 覆盖点：
//  1. 首次播种：字典 meta / GridFS 内容 / POC 条目数量与内嵌包一致；
//  2. 下载路径：按 meta _id 的 hex 能读回与源文件逐字节相同的内容；
//  3. 幂等：连续两次调用不产生重复条目（版本标记 + 存在性检查双闸）；
//  4. 行级索引未建时 entries_synced=false，由字典服务首次预览时回填，不在启动路径上做。
func TestSeedAssetPacksIntegration(t *testing.T) {
	uri := os.Getenv("SCOPESENTRY_TEST_MONGODB_URI")
	if uri == "" {
		t.Skip("SCOPESENTRY_TEST_MONGODB_URI not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri).SetServerSelectionTimeout(10*time.Second))
	if err != nil {
		t.Fatalf("connect mongo: %v", err)
	}
	defer client.Disconnect(context.Background())

	dbName := fmt.Sprintf("scopesentry_assetpack_test_%d", time.Now().UnixNano())
	db := client.Database(dbName)
	defer db.Drop(context.Background())

	if err := SeedAssetPacks(db); err != nil {
		t.Fatalf("first seed: %v", err)
	}

	packs, err := constants.EmbeddedDictPacks()
	if err != nil {
		t.Fatalf("EmbeddedDictPacks: %v", err)
	}
	templates, err := constants.POCPacks()
	if err != nil {
		t.Fatalf("POCPacks: %v", err)
	}

	assertCount(t, ctx, db, "dictionary", int64(len(packs)))
	assertCount(t, ctx, db, "PocList", int64(len(templates)))

	files, err := db.Collection("fs.files").CountDocuments(ctx, bson.M{})
	if err != nil {
		t.Fatalf("count fs.files: %v", err)
	}
	if files != int64(len(packs)) {
		t.Fatalf("fs.files = %d, want %d", files, len(packs))
	}

	for _, pack := range packs {
		var meta bson.M
		if err := db.Collection("dictionary").FindOne(ctx, bson.M{"name": pack.Name, "category": pack.Kind}).Decode(&meta); err != nil {
			t.Fatalf("find dictionary %s/%s: %v", pack.Kind, pack.Name, err)
		}
		if synced, _ := meta["entries_synced"].(bool); synced {
			t.Fatalf("pack %s/%s should not be line-indexed at seed time", pack.Kind, pack.Name)
		}
		if got, _ := meta["line_count"].(int64); got != int64(pack.Lines) {
			t.Fatalf("pack %s/%s line_count = %v, want %d", pack.Kind, pack.Name, meta["line_count"], pack.Lines)
		}
		oid, ok := meta["_id"].(primitive.ObjectID)
		if !ok {
			t.Fatalf("pack %s/%s has unexpected _id type %T", pack.Kind, pack.Name, meta["_id"])
		}
		content, err := ReadGridFSFile(db, oid.Hex())
		if err != nil {
			t.Fatalf("read gridfs %s: %v", oid.Hex(), err)
		}
		want, err := constants.DictPackContent(pack)
		if err != nil {
			t.Fatalf("DictPackContent %s/%s: %v", pack.Kind, pack.Name, err)
		}
		if string(content) != string(want) {
			t.Fatalf("gridfs content mismatch for %s/%s: got %d bytes want %d", pack.Kind, pack.Name, len(content), len(want))
		}
	}

	for _, template := range templates {
		var row bson.M
		if err := db.Collection("PocList").FindOne(ctx, bson.M{"id": template.ID}).Decode(&row); err != nil {
			t.Fatalf("find poc %s: %v", template.ID, err)
		}
		if got, _ := row["content"].(string); got != template.Content {
			t.Fatalf("poc %s stored content mismatch", template.ID)
		}
		if level, _ := row["level"].(string); level == "" {
			t.Fatalf("poc %s stored without level", template.ID)
		}
	}

	// 幂等：再跑一次不能翻倍
	if err := SeedAssetPacks(db); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	assertCount(t, ctx, db, "dictionary", int64(len(packs)))
	assertCount(t, ctx, db, "PocList", int64(len(templates)))

	var marker bson.M
	if err := db.Collection("config").FindOne(ctx, bson.M{"name": assetPackMarkerName}).Decode(&marker); err != nil {
		t.Fatalf("read marker: %v", err)
	}
	expected, err := constants.AssetPackDigest()
	if err != nil {
		t.Fatalf("AssetPackDigest: %v", err)
	}
	if value, _ := marker["value"].(string); value != expected {
		t.Fatalf("marker value = %v, want content digest %s", marker["value"], expected)
	}
}

func assertCount(t *testing.T, ctx context.Context, db *mongo.Database, coll string, want int64) {
	t.Helper()
	got, err := db.Collection(coll).CountDocuments(ctx, bson.M{})
	if err != nil {
		t.Fatalf("count %s: %v", coll, err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", coll, got, want)
	}
}
