//go:build integration

package httpstatus

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
)

// HTTP 状态视图：分档/精确码/keyword/project/分页
func TestHTTPStatusListFilters(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	ctx := context.Background()
	project := fmt.Sprintf("http-status-%d", time.Now().UnixNano())
	hostPrefix := fmt.Sprintf("hs-%d-", time.Now().UnixNano())
	coll := mongodb.DB.Collection("asset")
	cleanup := func() {
		_, _ = coll.DeleteMany(context.Background(), bson.M{"project": project})
	}
	cleanup()
	t.Cleanup(cleanup)

	docs := []interface{}{
		bson.M{"host": hostPrefix + "ok.example.com", "port": "443", "statuscode": 200, "title": "ok", "project": project, "time": "2026-11-18 10:00:00"},
		bson.M{"host": hostPrefix + "redirect.example.com", "port": "443", "statuscode": 301, "title": "moved", "project": project, "time": "2026-11-18 10:00:01"},
		bson.M{"host": hostPrefix + "missing.example.com", "port": "443", "statuscode": 404, "title": "not found", "project": project, "time": "2026-11-18 10:00:02"},
		bson.M{"host": hostPrefix + "error.example.com", "port": "443", "statuscode": 500, "title": "boom", "project": project, "time": "2026-11-18 10:00:03"},
	}
	if _, err := coll.InsertMany(ctx, docs); err != nil {
		t.Fatalf("seed: %v", err)
	}

	svc := NewService()
	all, err := svc.List(ctx, models.HTTPStatusListRequest{Project: project, PageIndex: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if all.Total != 4 || len(all.List) != 4 {
		t.Fatalf("all = %d/%d, want 4/4", all.Total, len(all.List))
	}
	if all.List[0].StatusCode != 500 {
		t.Fatalf("sort by time desc expected 500 first, got %d", all.List[0].StatusCode)
	}

	for _, tc := range []struct {
		status string
		want   int64
	}{
		{"2xx", 1}, {"3xx", 1}, {"4xx", 1}, {"5xx", 1}, {"404", 1},
	} {
		res, err := svc.List(ctx, models.HTTPStatusListRequest{Project: project, Status: tc.status, PageIndex: 1, PageSize: 10})
		if err != nil {
			t.Fatalf("list %s: %v", tc.status, err)
		}
		if res.Total != tc.want {
			t.Fatalf("%s total = %d, want %d", tc.status, res.Total, tc.want)
		}
	}
	if _, err := svc.List(ctx, models.HTTPStatusListRequest{Status: "6xx"}); err == nil {
		t.Fatal("6xx should be rejected")
	}

	kw, err := svc.List(ctx, models.HTTPStatusListRequest{Project: project, Keyword: "redirect", PageIndex: 1, PageSize: 10})
	if err != nil || kw.Total != 1 || kw.List[0].StatusCode != 301 {
		t.Fatalf("keyword filter = %+v err=%v", kw, err)
	}

	page, err := svc.List(ctx, models.HTTPStatusListRequest{Project: project, PageIndex: 2, PageSize: 2})
	if err != nil || len(page.List) != 2 || page.Total != 4 {
		t.Fatalf("page 2 = %d rows total=%d err=%v", len(page.List), page.Total, err)
	}
}
