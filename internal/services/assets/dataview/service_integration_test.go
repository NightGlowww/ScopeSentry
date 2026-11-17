//go:build integration

package dataview

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
)

// 文件泄露 / NPoC 服务：空集合可读、有数据可分页过滤、按 id 删除
func TestDataViewListAndDelete(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	fl := mongodb.DB.Collection(FileLeakCollection)
	nc := mongodb.DB.Collection(NpocCollection)
	t.Cleanup(func() {
		_, _ = fl.DeleteMany(context.Background(), bson.M{"site": bson.M{"$regex": "^" + suffix}})
		_, _ = nc.DeleteMany(context.Background(), bson.M{"target": bson.M{"$regex": "^" + suffix}})
	})

	svc := NewService()
	emptyRows, emptyTotal, err := svc.ListFileLeak(ctx, models.FileLeakListRequest{PageIndex: 1, PageSize: 10})
	if err != nil || emptyTotal != 0 || len(emptyRows) != 0 {
		t.Fatalf("empty fileleak = %d/%d err=%v", len(emptyRows), emptyTotal, err)
	}

	if _, err := fl.InsertMany(ctx, []interface{}{
		bson.M{"url": "https://" + suffix + ".example.com/a.zip", "site": suffix + "-a", "title": "backup", "status_code": 200, "content_length": 1024, "insert_time": "2026-11-18 10:00:00"},
		bson.M{"url": "https://" + suffix + ".example.com/b.sql", "site": suffix + "-b", "title": "db", "status_code": 403, "content_length": 2048, "insert_time": "2026-11-18 10:00:01"},
	}); err != nil {
		t.Fatalf("seed fileleak: %v", err)
	}
	rows, total, err := svc.ListFileLeak(ctx, models.FileLeakListRequest{Keyword: suffix, PageIndex: 1, PageSize: 10})
	if err != nil || total != 2 || len(rows) != 2 {
		t.Fatalf("fileleak list = %d/%d err=%v", len(rows), total, err)
	}
	filtered, ftotal, err := svc.ListFileLeak(ctx, models.FileLeakListRequest{Keyword: suffix, StatusCode: 403, PageIndex: 1, PageSize: 10})
	if err != nil || ftotal != 1 || len(filtered) != 1 {
		t.Fatalf("fileleak status filter = %d/%d err=%v", len(filtered), ftotal, err)
	}
	id, _ := rows[0]["id"].(string)
	if id == "" {
		t.Fatalf("row missing id: %+v", rows[0])
	}
	deleted, err := svc.DeleteFileLeak(ctx, []string{id})
	if err != nil || deleted != 1 {
		t.Fatalf("delete fileleak = %d err=%v", deleted, err)
	}
	if _, err := svc.DeleteFileLeak(ctx, nil); err == nil {
		t.Fatal("empty ids should be rejected")
	}

	if _, err := nc.InsertMany(ctx, []interface{}{
		bson.M{"scheme": "redis", "host": suffix + ".example.com", "port": "6379", "target": suffix + ".example.com:6379", "insert_time": "2026-11-18 11:00:00"},
		bson.M{"scheme": "ssh", "host": suffix + ".example.com", "port": "22", "target": suffix + ".example.com:22", "insert_time": "2026-11-18 11:00:01"},
	}); err != nil {
		t.Fatalf("seed npoc: %v", err)
	}
	nrows, ntotal, err := svc.ListNpoc(ctx, models.NpocServiceListRequest{Keyword: suffix, PageIndex: 1, PageSize: 10})
	if err != nil || ntotal != 2 || len(nrows) != 2 {
		t.Fatalf("npoc list = %d/%d err=%v", len(nrows), ntotal, err)
	}
	schemeRows, stotal, err := svc.ListNpoc(ctx, models.NpocServiceListRequest{Keyword: suffix, Scheme: "redis", PageIndex: 1, PageSize: 10})
	if err != nil || stotal != 1 || len(schemeRows) != 1 {
		t.Fatalf("npoc scheme filter = %d/%d err=%v", len(schemeRows), stotal, err)
	}
	nid, _ := nrows[0]["id"].(string)
	if _, err := svc.DeleteNpoc(ctx, []string{nid}); err != nil {
		t.Fatalf("delete npoc: %v", err)
	}
}
