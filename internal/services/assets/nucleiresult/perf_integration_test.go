//go:build integration

package nucleiresult

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

// 性能用例（可选）：PERF=1 时跑 2 万条漏洞的列表查询，把耗时打到测试日志里。
func TestNucleiResultPerformance(t *testing.T) {
	if os.Getenv("PERF") != "1" {
		t.Skip("set PERF=1 to run the performance case")
	}
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	if err := mongodb.EnsureIndexes(mongodb.DB); err != nil {
		t.Fatalf("ensure indexes: %v", err)
	}

	const vulnCount = 20000
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	project := "perf-nuclei-" + suffix
	hostPrefix := fmt.Sprintf("perf-nuc-%s", suffix)

	coll := mongodb.DB.Collection("vulnerability")
	t.Cleanup(func() {
		_, _ = coll.DeleteMany(context.Background(), bson.M{"project": project})
	})

	severities := []string{"critical", "high", "medium", "low", "info"}
	batch := make([]interface{}, 0, 1000)
	for i := 0; i < vulnCount; i++ {
		batch = append(batch, bson.M{
			"vulnid":     fmt.Sprintf("cve-2021-%04d", i%9000),
			"vulnname":   fmt.Sprintf("template-%d", i%50),
			"level":      severities[i%len(severities)],
			"url":        fmt.Sprintf("https://%s-%05d.example.com/admin", hostPrefix, i),
			"matched":    fmt.Sprintf("matched-%d", i),
			"project":    project,
			"rootDomain": hostPrefix + ".example.com",
			"taskName":   "perf-task",
			"status":     1,
			"time":       fmt.Sprintf("2026-11-%02d %02d:%02d:00", i%28+1, i%24, i%60),
			"hash":       fmt.Sprintf("%s-hash-%d", project, i),
		})
		if len(batch) == cap(batch) {
			if _, err := coll.InsertMany(context.Background(), batch); err != nil {
				t.Fatalf("seed batch: %v", err)
			}
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

	cases := []struct {
		name string
		req  models.NucleiResultListRequest
	}{
		{"project", models.NucleiResultListRequest{Project: project, PageIndex: 1, PageSize: 200}},
		{"project+severity", models.NucleiResultListRequest{Project: project, Severity: "critical", PageIndex: 1, PageSize: 200}},
		{"severity only", models.NucleiResultListRequest{Severity: "critical", PageIndex: 1, PageSize: 200}},
		{"no filter", models.NucleiResultListRequest{PageIndex: 1, PageSize: 200}},
		{"templateId substring", models.NucleiResultListRequest{Project: project, TemplateID: "cve-2021-442", PageIndex: 1, PageSize: 200}},
	}
	for _, tc := range cases {
		start := time.Now()
		res, err := svc.List(ctx, tc.req)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		t.Logf("[PERF] nuclei_result list(%s, vulns=%d): %v, total=%d, rows=%d",
			tc.name, vulnCount, time.Since(start), res.Total, len(res.List))
	}
}
