//go:build integration

package statfinger

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
)

// 性能用例（可选）：PERF=1 时跑 1 万资产的统计与反查，把耗时打到测试日志里。
// 默认跳过，避免常规集成测试被数据集准备拖慢。
func TestStatFingerPerformance(t *testing.T) {
	if os.Getenv("PERF") != "1" {
		t.Skip("set PERF=1 to run the performance case")
	}
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	if err := mongodb.EnsureIndexes(mongodb.DB); err != nil {
		t.Fatalf("ensure indexes: %v", err)
	}

	const assetCount = 10000
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	project := "perf-statfinger-" + suffix
	hostPrefix := fmt.Sprintf("perf-sf-%s-", suffix)

	coll := mongodb.DB.Collection("asset")
	t.Cleanup(func() {
		_, _ = coll.DeleteMany(context.Background(), bson.M{"project": project})
	})

	techPool := []string{"nginx", "php", "jquery", "tomcat", "wordpress", "react", "vue", "spring", "django", "flask"}
	ipPrefix := fmt.Sprintf("198.22.%d.", time.Now().UnixNano()%200+20)
	batch := make([]interface{}, 0, 1000)
	flushed := 0
	for i := 0; i < assetCount; i++ {
		batch = append(batch, bson.M{
			"host":    fmt.Sprintf("%s%05d.example.com", hostPrefix, i),
			"port":    "443",
			"ip":      fmt.Sprintf("%s%d", ipPrefix, i%250+1),
			"url":     fmt.Sprintf("https://%s%05d.example.com", hostPrefix, i),
			"project": project,
			"time":    fmt.Sprintf("2026-11-18 %02d:%02d:00", 10+i%10, i%60),
			"technologies": []string{
				techPool[i%len(techPool)],
				techPool[(i/7)%len(techPool)],
				techPool[(i/13)%len(techPool)],
			},
		})
		if len(batch) == cap(batch) {
			if _, err := coll.InsertMany(context.Background(), batch); err != nil {
				t.Fatalf("seed batch %d: %v", flushed, err)
			}
			flushed++
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		if _, err := coll.InsertMany(context.Background(), batch); err != nil {
			t.Fatalf("seed tail batch: %v", err)
		}
	}

	svc := NewService()
	ctx := context.Background()

	start := time.Now()
	res, err := svc.ListTechnologies(ctx, models.StatFingerListRequest{Project: project, PageIndex: 1, PageSize: 200})
	if err != nil {
		t.Fatalf("ListTechnologies(project): %v", err)
	}
	t.Logf("[PERF] stat_finger list(project=%s, assets=%d): %v, total=%d, rows=%d",
		project, assetCount, time.Since(start), res.Total, len(res.List))

	// 全库统计（不传 project）：这是统计类接口最重的形状，耗时单独记一笔
	start = time.Now()
	resAll, err := svc.ListTechnologies(ctx, models.StatFingerListRequest{PageIndex: 1, PageSize: 200})
	if err != nil {
		t.Fatalf("ListTechnologies(all): %v", err)
	}
	t.Logf("[PERF] stat_finger list(no project filter): %v, total technologies=%d, rows=%d",
		time.Since(start), resAll.Total, len(resAll.List))

	start = time.Now()
	assets, err := svc.ListAssetsByTechnology(ctx, models.StatFingerAssetsRequest{
		Technology: "nginx", Project: project, PageIndex: 1, PageSize: 200,
	})
	if err != nil {
		t.Fatalf("ListAssetsByTechnology: %v", err)
	}
	t.Logf("[PERF] stat_finger assets(technology=nginx, project): %v, total=%d, rows=%d",
		time.Since(start), assets.Total, len(assets.List))
}
