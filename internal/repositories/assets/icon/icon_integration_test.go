//go:build integration

// 图标回源需要真实 MongoDB（icon 集合的 fav3 唯一索引、asset.iconcontent 历史数据），
// 因此挂在 integration 标签下。连接串走 SCOPESENTRY_TEST_MONGODB_URI，未设置则跳过，
// 与其它集成测试保持一致；库名带时间戳，跑完整库删除，不污染真实数据。
package icon

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const testPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func TestFindContentIntegration(t *testing.T) {
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

	dbName := fmt.Sprintf("scopesentry_icon_test_%d", time.Now().UnixNano())
	db := client.Database(dbName)
	defer db.Drop(context.Background())

	previous := mongodb.DB
	mongodb.DB = db
	defer func() { mongodb.DB = previous }()

	repo := NewRepository()

	// 1. 扫描节点写入的 icon 集合。
	if _, err := db.Collection("icon").InsertOne(ctx, bson.M{"fav3": "123456789", "content": testPNGBase64}); err != nil {
		t.Fatalf("seed icon: %v", err)
	}
	content, err := repo.FindContent(ctx, "123456789")
	if err != nil {
		t.Fatalf("FindContent(icon): %v", err)
	}
	if content != testPNGBase64 {
		t.Fatalf("内容不一致: %q", content)
	}

	// 2. 不存在的 hash 必须是 ErrNoDocuments，而不是空字符串或其它错误。
	if _, err := repo.FindContent(ctx, "404404404"); !errors.Is(err, mongo.ErrNoDocuments) {
		t.Fatalf("不存在时应返回 ErrNoDocuments，实际: %v", err)
	}

	// 3. 空 hash 直接当不存在，避免无谓查询。
	if _, err := repo.FindContent(ctx, "  "); !errors.Is(err, mongo.ErrNoDocuments) {
		t.Fatalf("空 hash 应返回 ErrNoDocuments，实际: %v", err)
	}

	// 4. 1.8 之前的实例：图标还留在资产文档里。
	legacy := base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nlegacy"))
	if _, err := db.Collection("asset").InsertOne(ctx, bson.M{
		"host":        "legacy.example.com",
		"port":        "443",
		"faviconmmh3": "-42",
		"iconcontent": legacy,
	}); err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	content, err = repo.FindContent(ctx, "-42")
	if err != nil {
		t.Fatalf("FindContent(legacy): %v", err)
	}
	if content != legacy {
		t.Fatalf("历史数据回源内容不一致: %q", content)
	}

	// 5. icon 集合里内容为空时，应继续回退到资产文档。
	if _, err := db.Collection("icon").InsertOne(ctx, bson.M{"fav3": "-42", "content": ""}); err != nil {
		t.Fatalf("seed empty icon: %v", err)
	}
	content, err = repo.FindContent(ctx, "-42")
	if err != nil {
		t.Fatalf("空 content 应回退到 asset: %v", err)
	}
	if content != legacy {
		t.Fatalf("空 content 回退结果不一致: %q", content)
	}
}
