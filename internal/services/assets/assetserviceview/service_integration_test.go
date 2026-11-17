//go:build integration

package assetserviceview

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
)

// 服务维度：按 service 聚合资产/主机/端口/样例，空 service 排除
func TestAssetServiceAggregation(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	ctx := context.Background()
	project := fmt.Sprintf("svc-view-%d", time.Now().UnixNano())
	prefix := fmt.Sprintf("svc-%d-", time.Now().UnixNano())
	coll := mongodb.DB.Collection("asset")
	cleanup := func() {
		_, _ = coll.DeleteMany(context.Background(), bson.M{"project": project})
	}
	cleanup()
	t.Cleanup(cleanup)

	docs := []interface{}{}
	for i := 0; i < 3; i++ {
		docs = append(docs, bson.M{
			"host": fmt.Sprintf("%sweb%d.example.com", prefix, i), "port": "443", "service": "https",
			"project": project, "time": fmt.Sprintf("2026-11-18 10:00:0%d", i),
		})
	}
	docs = append(docs,
		bson.M{"host": prefix + "ssh.example.com", "port": "22", "service": "ssh", "project": project, "time": "2026-11-18 10:01:00"},
		bson.M{"host": prefix + "ssh2.example.com", "port": "22", "service": "ssh", "project": project, "time": "2026-11-18 10:01:01"},
		bson.M{"host": prefix + "empty.example.com", "port": "80", "service": "", "project": project, "time": "2026-11-18 10:02:00"},
	)
	if _, err := coll.InsertMany(ctx, docs); err != nil {
		t.Fatalf("seed: %v", err)
	}

	svc := NewService()
	res, err := svc.List(ctx, models.AssetServiceListRequest{Project: project, PageIndex: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if res.Total != 2 || len(res.List) != 2 {
		t.Fatalf("total=%d rows=%d, want 2 (empty service excluded)", res.Total, len(res.List))
	}
	if res.List[0].Service != "https" || res.List[0].AssetCount != 3 || res.List[0].HostCount != 3 || res.List[0].PortCount != 1 {
		t.Fatalf("https row = %+v", res.List[0])
	}
	if res.List[0].LastSeen != "2026-11-18 10:00:02" {
		t.Fatalf("lastSeen = %q", res.List[0].LastSeen)
	}
	if len(res.List[0].Samples) != 3 {
		t.Fatalf("samples = %v", res.List[0].Samples)
	}
	if res.List[1].Service != "ssh" || res.List[1].AssetCount != 2 {
		t.Fatalf("ssh row = %+v", res.List[1])
	}

	kw, err := svc.List(ctx, models.AssetServiceListRequest{Project: project, Keyword: "ss", PageIndex: 1, PageSize: 20})
	if err != nil || kw.Total != 1 || kw.List[0].Service != "ssh" {
		t.Fatalf("keyword = %+v err=%v", kw, err)
	}
	page, err := svc.List(ctx, models.AssetServiceListRequest{Project: project, PageIndex: 2, PageSize: 1})
	if err != nil || len(page.List) != 1 || page.List[0].Service != "ssh" || page.Total != 2 {
		t.Fatalf("page2 = %+v err=%v", page, err)
	}
}
