package system

// 多源情报订阅：只读视图 + 立即刷新。
import (
	"context"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/services/intelfeed"
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
		_ = cur.Close()
	}
	var items []bson.M
	opts := options.Find().SetSort(bson.M{"addedAt": -1}).SetLimit(20)
	if cur, err := mongodb.DB.Collection("intel_items").Find(ctx, bson.M{}, opts); err == nil {
		_ = cur.All(ctx, &items)
		_ = cur.Close()
	}
	total, _ := mongodb.DB.Collection("intel_items").CountDocuments(ctx, bson.M{})
	response.Success(c, gin.H{"feeds": feeds, "items": items, "total": total}, "api.success")
}

// IntelFeedsRun 立即跑一轮抓取，无视 intervalHours。
// body: {"id": "helpnet"}，id 省略 = 跑全部启用的源。
// 单个源最坏 3 次重试 + 40s 超时，全量跑一轮上限给 5 分钟。
func IntelFeedsRun(c *gin.Context) {
	var body struct {
		ID string `json:"id"`
	}
	_ = c.ShouldBindJSON(&body)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
	defer cancel()
	res, err := intelfeed.RunNow(ctx, body.ID)
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, gin.H{"results": res}, "api.success")
}
