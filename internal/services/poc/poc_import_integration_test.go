//go:build integration

package poc

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
)

// 导入语义需要真实 Mongo（$set 更新 / _id 稳定性），挂在 integration 标签下。
// 连接串走 SCOPESENTRY_TEST_MONGODB_URI，并且始终切到独立临时库，避免写脏真实数据。
func setupPocTestDB(t *testing.T) {
	t.Helper()
	uri := os.Getenv("SCOPESENTRY_TEST_MONGODB_URI")
	if uri == "" {
		t.Skip("SCOPESENTRY_TEST_MONGODB_URI not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri).SetServerSelectionTimeout(10*time.Second))
	if err != nil {
		t.Fatalf("connect mongo: %v", err)
	}
	db := client.Database(fmt.Sprintf("scopesentry_poc_test_%d", time.Now().UnixNano()))
	mongodb.Client = client
	mongodb.DB = db
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
		mongodb.DB = nil
		mongodb.Client = nil
	})
}

func writePocZip(t *testing.T, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "poc.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("file close: %v", err)
	}
	return path
}

func templateBody(severity, marker, tags string) string {
	return templateBodyFor("scopesentry-test-import", severity, marker, tags)
}

func templateBodyFor(id, severity, marker, tags string) string {
	return fmt.Sprintf("id: %s\n"+
		"info:\n"+
		"  name: Import Semantics Test\n"+
		"  severity: %s\n"+
		"  description: matcher marker %s\n"+
		"  tags: %s\n"+
		"http:\n"+
		"  - method: GET\n"+
		"    path:\n"+
		"      - '{{BaseURL}}/%s'\n"+
		"    matchers:\n"+
		"      - type: word\n"+
		"        words:\n"+
		"          - '%s'\n", id, severity, marker, tags, marker, marker)
}

func fetchPoc(t *testing.T, templateID string) bson.M {
	t.Helper()
	var doc bson.M
	if err := mongodb.DB.Collection("PocList").FindOne(context.Background(), bson.M{"id": templateID}).Decode(&doc); err != nil {
		t.Fatalf("find poc %s: %v", templateID, err)
	}
	return doc
}

func TestImportPocOverwriteSemantics(t *testing.T) {
	setupPocTestDB(t)
	const templateID = "scopesentry-test-import"
	t.Cleanup(func() {
		_, _ = mongodb.DB.Collection("PocList").DeleteMany(context.Background(), bson.M{"id": templateID})
		_, _ = mongodb.DB.Collection("PocList").DeleteMany(context.Background(), bson.M{"id": "scopesentry-test-upsert"})
	})

	svc := NewService()
	ctx := context.Background()

	// 1) 首次导入：新增
	resp, err := svc.ImportPocWithOptions(ctx, writePocZip(t, map[string]string{"t.yaml": templateBody("high", "alpha", "tech,wordpress")}), true)
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	if resp.SuccessNum != 1 || resp.UpdateNum != 0 || resp.RepeatNum != 0 {
		t.Fatalf("first import counts: %+v", resp)
	}
	first := fetchPoc(t, templateID)
	firstID, ok := first["_id"].(primitive.ObjectID)
	if !ok {
		t.Fatalf("unexpected _id type %T", first["_id"])
	}
	if hash, _ := first["content_hash"].(string); len(hash) != 64 {
		t.Fatalf("content_hash not stored: %q", hash)
	}

	// 2) 同 id 内容变化：更新，且 _id 不变（VulList 引用不断）
	resp, err = svc.ImportPocWithOptions(ctx, writePocZip(t, map[string]string{"t.yaml": templateBody("critical", "beta", "tech,cve")}), true)
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if resp.UpdateNum != 1 || resp.SuccessNum != 0 || resp.RepeatNum != 0 {
		t.Fatalf("update counts: %+v", resp)
	}
	updated := fetchPoc(t, templateID)
	updatedID, _ := updated["_id"].(primitive.ObjectID)
	if updatedID != firstID {
		t.Fatalf("_id changed on update: %s -> %s", firstID.Hex(), updatedID.Hex())
	}
	if content, _ := updated["content"].(string); !strings.Contains(content, "beta") || strings.Contains(content, "alpha") {
		t.Fatalf("content not replaced: %q", content)
	}
	if level, _ := updated["level"].(string); level != "critical" {
		t.Fatalf("level not updated: %q", level)
	}
	if tags, _ := updated["tags"].(bson.A); len(tags) != 2 || tags[1] != "cve" {
		t.Fatalf("tags not updated: %#v", updated["tags"])
	}

	// 3) 原样再导一次：计入 repeat
	resp, err = svc.ImportPocWithOptions(ctx, writePocZip(t, map[string]string{"t.yaml": templateBody("critical", "beta", "tech,cve")}), true)
	if err != nil {
		t.Fatalf("third import: %v", err)
	}
	if resp.RepeatNum != 1 || resp.UpdateNum != 0 {
		t.Fatalf("repeat counts: %+v", resp)
	}

	// 4) 仅空白差异：归一化后视为没变
	whitespace := strings.ReplaceAll(templateBody("critical", "beta", "tech,cve"), "\n", "  \n") + "\n\n"
	resp, err = svc.ImportPocWithOptions(ctx, writePocZip(t, map[string]string{"t.yaml": whitespace}), true)
	if err != nil {
		t.Fatalf("whitespace import: %v", err)
	}
	if resp.RepeatNum != 1 || resp.UpdateNum != 0 {
		t.Fatalf("whitespace-only change should be a repeat: %+v", resp)
	}

	// 5) overwrite=false：真实变化也不覆盖
	resp, err = svc.ImportPocWithOptions(ctx, writePocZip(t, map[string]string{"t.yaml": templateBody("low", "gamma", "misc")}), false)
	if err != nil {
		t.Fatalf("no-overwrite import: %v", err)
	}
	if resp.RepeatNum != 1 || resp.UpdateNum != 0 {
		t.Fatalf("overwrite=false counts: %+v", resp)
	}
	final := fetchPoc(t, templateID)
	if content, _ := final["content"].(string); !strings.Contains(content, "beta") {
		t.Fatal("content changed despite overwrite=false")
	}

	// 6) UpsertPoc：created / unchanged / updated
	createdID, action, err := svc.UpsertPoc(ctx, templateBodyFor("scopesentry-test-upsert", "medium", "delta", "tech"))
	if err != nil {
		t.Fatalf("upsert create: %v", err)
	}
	if action != "created" || createdID == "" {
		t.Fatalf("upsert create action=%q", action)
	}
	sameID, action, err := svc.UpsertPoc(ctx, templateBodyFor("scopesentry-test-upsert", "medium", "delta", "tech"))
	if err != nil || action != "unchanged" || sameID != createdID {
		t.Fatalf("upsert unchanged action=%q id=%s err=%v", action, sameID, err)
	}
	updatedID2, action, err := svc.UpsertPoc(ctx, templateBodyFor("scopesentry-test-upsert", "high", "epsilon", "tech"))
	if err != nil || action != "updated" || updatedID2 != createdID {
		t.Fatalf("upsert update action=%q id=%s err=%v", action, updatedID2, err)
	}
}
