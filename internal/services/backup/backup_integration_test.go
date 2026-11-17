//go:build integration

package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	roundTripCollection = "BackupRoundTripTest"
	roundTripMarker     = "backupRoundTripMarker"
)

var archiveNamePattern = regexp.MustCompile(`^backup-[0-9]{8}-[0-9]{6}(-[0-9]+)?\.tar\.gz$`)

// 说明：真实连库的集成测试。只操作 ScopeSentryLocalTest 里的临时集合，
// 恢复时逐文档 upsert 回同一个测试库，不会触碰生产数据。
func TestBackupRoundTrip(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized (set MONGODB_IP/MONGODB_USER/MONGODB_PASSWORD/MONGODB_DATABASE)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	collection := mongodb.DB.Collection(roundTripCollection)
	if err := collection.Drop(ctx); err != nil {
		t.Fatalf("drop test collection: %v", err)
	}
	t.Cleanup(func() {
		_ = mongodb.DB.Collection(roundTripCollection).Drop(context.Background())
	})

	ids := []primitive.ObjectID{primitive.NewObjectID(), primitive.NewObjectID(), primitive.NewObjectID()}
	markers := []string{"round-trip-1", "round-trip-2", "round-trip-3"}
	seed := []interface{}{
		bson.M{"_id": ids[0], "name": "alpha", roundTripMarker: markers[0], "count": int32(1), "tags": bson.A{"a", "b"}},
		bson.M{"_id": ids[1], "name": "beta", roundTripMarker: markers[1], "count": int64(2)},
		bson.M{"_id": ids[2], "name": "gamma", roundTripMarker: markers[2], "nested": bson.M{"k": "v"}},
	}
	if _, err := collection.InsertMany(ctx, seed); err != nil {
		t.Fatalf("seed test collection: %v", err)
	}

	backupDir := filepath.Join(t.TempDir(), "files", "backups")
	dataRoot := t.TempDir()
	writeFixture(t, filepath.Join(dataRoot, "uploads", "u1.txt"), "hello upload")
	writeFixture(t, filepath.Join(dataRoot, "files", "export", "e1.txt"), "hello export")
	// files/backups 必须被排除，否则备份包会把自己套进去。
	writeFixture(t, filepath.Join(dataRoot, "files", "backups", "old.tar.gz"), "should not be packed")

	svc := NewServiceWithPaths(backupDir, dataRoot)

	// ---- create ----
	info, err := svc.Create(ctx, "integration: "+roundTripCollection)
	if err != nil {
		t.Fatalf("create backup: %v", err)
	}
	if !archiveNamePattern.MatchString(info.Name) {
		t.Fatalf("unexpected backup name %q", info.Name)
	}
	archivePath, err := svc.Path(info.Name)
	if err != nil {
		t.Fatalf("resolve backup path: %v", err)
	}
	if got := hashFile(t, archivePath); got != info.SHA256 {
		t.Fatalf("create returned sha256 %s, file sha256 %s", info.SHA256, got)
	}
	stat, err := os.Stat(archivePath)
	if err != nil {
		t.Fatalf("stat backup: %v", err)
	}
	if stat.Size() != info.Size || info.Size == 0 {
		t.Fatalf("create returned size %d, file size %d", info.Size, stat.Size())
	}
	// 备份包内含整库数据（含口令哈希），落盘权限必须是 0600。
	if perm := stat.Mode().Perm(); perm != 0o600 {
		t.Fatalf("backup archive mode is %o, want 600", perm)
	}

	// ---- manifest 内容（集合清单 + 文档数 + 目录文件数）----
	manifest, entries := readArchive(t, archivePath)
	if err := VerifyManifest(manifest); err != nil {
		t.Fatalf("manifest in archive does not verify: %v", err)
	}
	if manifest.Version != BackupVersion {
		t.Fatalf("unexpected format version %d", manifest.Version)
	}
	var roundTrip *models.BackupCollectionEntry
	for i := range manifest.Collections {
		if manifest.Collections[i].Name == roundTripCollection {
			roundTrip = &manifest.Collections[i]
		}
	}
	if roundTrip == nil {
		t.Fatalf("manifest does not list %s: %+v", roundTripCollection, manifest.Collections)
	}
	if roundTrip.Documents != int64(len(seed)) {
		t.Fatalf("manifest says %d documents, want %d", roundTrip.Documents, len(seed))
	}
	if got := entries[roundTrip.File]; got == 0 {
		t.Fatalf("archive does not contain %s (entries: %v)", roundTrip.File, keysOf(entries))
	}
	if manifest.Uploads.Files != 1 || manifest.Uploads.Bytes != int64(len("hello upload")) {
		t.Fatalf("unexpected uploads summary: %+v", manifest.Uploads)
	}
	if manifest.Files.Files != 1 || manifest.Files.Bytes != int64(len("hello export")) {
		t.Fatalf("unexpected files summary: %+v", manifest.Files)
	}
	for _, name := range []string{"manifest.json", "collections/" + roundTripCollection + ".bson", "uploads/u1.txt", "files/export/e1.txt"} {
		if _, ok := entries[name]; !ok {
			t.Fatalf("archive entry %q missing (entries: %v)", name, keysOf(entries))
		}
	}
	if _, ok := entries["files/backups/old.tar.gz"]; ok {
		t.Fatal("files/backups must be excluded from the backup archive")
	}

	// ---- list ----
	list, err := svc.List()
	if err != nil {
		t.Fatalf("list backups: %v", err)
	}
	listed := findBackup(t, list, info.Name)
	if listed.SHA256 != info.SHA256 || listed.Size != info.Size || listed.Note != "integration: "+roundTripCollection {
		t.Fatalf("list entry mismatch: %+v", listed)
	}

	// ---- 路径穿越闸 ----
	if _, err := svc.Path("../" + info.Name); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("expected ErrInvalidName for traversal path, got %v", err)
	}
	if _, err := svc.Path("nested/" + info.Name); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("expected ErrInvalidName for nested path, got %v", err)
	}

	// ---- restore 往返：删掉文档再恢复 ----
	deleted, err := collection.DeleteMany(ctx, bson.M{})
	if err != nil {
		t.Fatalf("delete seeded docs: %v", err)
	}
	if deleted.DeletedCount != int64(len(seed)) {
		t.Fatalf("deleted %d docs, want %d", deleted.DeletedCount, len(seed))
	}
	if count, err := collection.CountDocuments(ctx, bson.M{}); err != nil || count != 0 {
		t.Fatalf("collection should be empty before restore, count=%d err=%v", count, err)
	}

	result, err := svc.Restore(ctx, info.Name)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if result.Restored != info.Name {
		t.Fatalf("restore reported archive %q", result.Restored)
	}
	if result.PreRestore == "" {
		t.Fatal("restore did not create a pre-restore backup")
	}
	if result.Collections < 1 || result.Documents < int64(len(seed)) {
		t.Fatalf("restore result too small: %+v", result)
	}
	if result.Files != 2 {
		t.Fatalf("restore extracted %d files, want 2", result.Files)
	}
	prePath, err := svc.Path(result.PreRestore)
	if err != nil {
		t.Fatalf("pre-restore backup path: %v", err)
	}
	if _, err := os.Stat(prePath); err != nil {
		t.Fatalf("pre-restore backup missing: %v", err)
	}
	preListed := findBackup(t, mustList(t, svc), result.PreRestore)
	if preListed.Note != "pre-restore:"+info.Name {
		t.Fatalf("unexpected pre-restore note %q", preListed.Note)
	}

	count, err := collection.CountDocuments(ctx, bson.M{})
	if err != nil {
		t.Fatalf("count after restore: %v", err)
	}
	if count != int64(len(seed)) {
		t.Fatalf("restored %d docs, want %d", count, len(seed))
	}
	for i, id := range ids {
		var doc bson.M
		if err := collection.FindOne(ctx, bson.M{"_id": id}).Decode(&doc); err != nil {
			t.Fatalf("find restored doc %s: %v", id.Hex(), err)
		}
		if got, _ := doc[roundTripMarker].(string); got != markers[i] {
			t.Fatalf("restored doc %s marker %q, want %q", id.Hex(), got, markers[i])
		}
	}
	if body := readFile(t, filepath.Join(dataRoot, "uploads", "u1.txt")); body != "hello upload" {
		t.Fatalf("uploads file not restored: %q", body)
	}

	// ---- upload ----
	archiveBytes, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	copyName := fmt.Sprintf("copy-%d-%s", time.Now().UnixNano(), info.Name)
	uploaded, err := svc.Upload(ctx, copyName, bytes.NewReader(archiveBytes), info.SHA256)
	if err != nil {
		t.Fatalf("upload backup: %v", err)
	}
	if uploaded.SHA256 != info.SHA256 || uploaded.Size != info.Size {
		t.Fatalf("uploaded backup metadata mismatch: %+v", uploaded)
	}
	if _, err := svc.Upload(ctx, copyName, bytes.NewReader(archiveBytes), info.SHA256); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists on duplicate upload, got %v", err)
	}
	findBackup(t, mustList(t, svc), copyName)

	badName := fmt.Sprintf("bad-%d-%s", time.Now().UnixNano(), info.Name)
	if _, err := svc.Upload(ctx, badName, bytes.NewReader(archiveBytes), strings.Repeat("0", 64)); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("expected ErrChecksumMismatch, got %v", err)
	}

	tampered := append([]byte(nil), archiveBytes...)
	for i := len(tampered) - 8; i < len(tampered); i++ {
		tampered[i] ^= 0xff
	}
	tamperedName := fmt.Sprintf("tampered-%d-%s", time.Now().UnixNano(), info.Name)
	if _, err := svc.Upload(ctx, tamperedName, bytes.NewReader(tampered), ""); err == nil {
		t.Fatal("tampered archive must be rejected")
	}

	// ---- delete ----
	if err := svc.Delete(info.Name); err != nil {
		t.Fatalf("delete backup: %v", err)
	}
	if _, err := os.Stat(archivePath); !os.IsNotExist(err) {
		t.Fatalf("backup file still exists after delete: %v", err)
	}
	for _, entry := range mustList(t, svc) {
		if entry.Name == info.Name {
			t.Fatalf("deleted backup %s still listed", info.Name)
		}
	}
	if err := svc.Delete(info.Name); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound on second delete, got %v", err)
	}
}

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func hashFile(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatalf("hash %s: %v", path, err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// readArchive 读取包内 manifest 与全部条目名。
func readArchive(t *testing.T, archivePath string) (models.BackupManifest, map[string]int64) {
	t.Helper()
	f, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip archive: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	hdr, err := tr.Next()
	if err != nil {
		t.Fatalf("read first archive entry: %v", err)
	}
	if hdr.Name != ManifestFileName {
		t.Fatalf("first archive entry is %q, want %q", hdr.Name, ManifestFileName)
	}
	var manifest models.BackupManifest
	if err := json.NewDecoder(tr).Decode(&manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}

	entries := map[string]int64{ManifestFileName: hdr.Size}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read archive entry: %v", err)
		}
		entries[hdr.Name] = hdr.Size
	}
	return manifest, entries
}

func findBackup(t *testing.T, list []models.BackupInfo, name string) models.BackupInfo {
	t.Helper()
	for _, info := range list {
		if info.Name == name {
			return info
		}
	}
	t.Fatalf("backup %s not found in list %v", name, keysOfBackups(list))
	return models.BackupInfo{}
}

func mustList(t *testing.T, svc Service) []models.BackupInfo {
	t.Helper()
	list, err := svc.List()
	if err != nil {
		t.Fatalf("list backups: %v", err)
	}
	return list
}

func keysOf(m map[string]int64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func keysOfBackups(list []models.BackupInfo) []string {
	out := make([]string, 0, len(list))
	for _, info := range list {
		out = append(out, info.Name)
	}
	return out
}
