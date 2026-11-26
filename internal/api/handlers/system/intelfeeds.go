package system

// 多源情报订阅的只读视图：有哪些源、跑没跑成功、最近抓到什么。
import (
	"context"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// IntelFeeds 返回订阅源状态与最近条目（默认 20 条）。
func IntelFeeds(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	if mongodb.DB == nil {
		response.Success(c, gin.H{"feeds": []any{}, "items": []any{}}, "api.success")
		return
	}
	var feeds []bson.M
	if cur, err := mongodb.DB.Collection("intel_feeds").Find(ctx, bson.M{}); err == nil {
		_ = cur.All(ctx, &feeds)
		_ = cur.Close(ctx)
	}
	var items []bson.M
	opts := options.Find().SetSort(bson.M{"addedAt": -1}).SetLimit(20)
	if cur, err := mongodb.DB.Collection("intel_items").Find(ctx, bson.M{}, opts); err == nil {
		_ = cur.All(ctx, &items)
		_ = cur.Close(ctx)
	}
	total, _ := mongodb.DB.Collection("intel_items").CountDocuments(ctx, bson.M{})
	response.Success(c, gin.H{"feeds": feeds, "items": items, "total": total}, "api.success")
}
