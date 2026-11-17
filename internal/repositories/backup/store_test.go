package backup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

func TestValidateBackupNameRejectsTraversal(t *testing.T) {
	cases := []string{
		"",
		"../../etc/passwd.tar.gz",
		"../backup-20260101-120000.tar.gz",
		"backup-20260101-120000.tar.gz/../../evil.tar.gz",
		"/tmp/backup-20260101-120000.tar.gz",
		"dir/backup-20260101-120000.tar.gz",
		"backup-20260101-120000.tar.gz/..",
		"..\\backup-20260101-120000.tar.gz",
		"backup-20260101-120000.tar.gz\x00.txt",
		"backup-20260101-120000.zip",
		"backup.tar.gz/",
		".tar.gz",
		"backup..tar.gz",
	}
	for _, name := range cases {
		err := ValidateBackupName(name)
		if err == nil {
			t.Fatalf("expected %q to be rejected", name)
		}
		if !errors.Is(err, ErrInvalidBackupName) {
			t.Fatalf("expected ErrInvalidBackupName for %q, got %v", name, err)
		}
	}
}

func TestValidateBackupNameAcceptsGeneratedNames(t *testing.T) {
	valid := []string{
		"backup-20260101-120000.tar.gz",
		"backup-20260101-120000-2.tar.gz",
		"upl-oad_2026.01.tar.gz",
	}
	for _, name := range valid {
		if err := ValidateBackupName(name); err != nil {
			t.Fatalf("expected %q to be accepted, got %v", name, err)
		}
	}
}

func TestStorePathStaysInsideDir(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	full, err := store.Path("backup-20260101-120000.tar.gz")
	if err != nil {
		t.Fatalf("valid name rejected: %v", err)
	}
	if filepath.Dir(full) != dir {
		t.Fatalf("path escaped backup dir: %s", full)
	}

	for _, name := range []string{"../backup-20260101-120000.tar.gz", filepath.Join("nested", "backup-20260101-120000.tar.gz")} {
		if _, err := store.Path(name); err == nil {
			t.Fatalf("expected %q to be rejected by Path", name)
		}
	}
}

func TestStoreMetaListDelete(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	name := "backup-20260101-120000.tar.gz"

	if _, err := store.List(); err != nil {
		t.Fatalf("list on fresh dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("payload"), 0o644); err != nil {
		t.Fatalf("write backup fixture: %v", err)
	}
	meta := models.BackupInfo{
		Name:      name,
		Size:      7,
		CreatedAt: "2026-01-01T12:00:00+08:00",
		SHA256:    strings.Repeat("a", 64),
		Note:      "unit test",
	}
	if err := store.SaveMeta(meta); err != nil {
		t.Fatalf("save meta: %v", err)
	}

	infos, err := store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("expected 1 backup, got %d", len(infos))
	}
	got := infos[0]
	if got.Name != name || got.Note != "unit test" || got.SHA256 != meta.SHA256 || got.CreatedAt != meta.CreatedAt {
		t.Fatalf("list returned unexpected meta: %+v", got)
	}
	if got.Size != 7 {
		t.Fatalf("expected size 7 from disk, got %d", got.Size)
	}

	if err := store.Delete(name); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if infos, err := store.List(); err != nil || len(infos) != 0 {
		t.Fatalf("expected empty list after delete, got %d (err=%v)", len(infos), err)
	}
	if err := store.Delete(name); !errors.Is(err, ErrBackupNotFound) {
		t.Fatalf("expected ErrBackupNotFound on second delete, got %v", err)
	}
}

// 没有旁车 meta 的人工拷贝文件，list 也要能给出 sha256。
func TestStoreListHashesUntrackedArchive(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	name := "backup-20260101-120000.tar.gz"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("payload"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	infos, err := store.List()
	if err != nil || len(infos) != 1 {
		t.Fatalf("list: %v (n=%d)", err, len(infos))
	}
	if infos[0].SHA256 != "239f59ed55e737c77147cf55ad0c1b030b6d7ee748a7426952f9b852d5a935e5" {
		t.Fatalf("unexpected sha256 for %q: %s", "payload", infos[0].SHA256)
	}
}

func TestStoreUniqueNameAvoidsCollision(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	stamp := "20260101-120000"

	first, err := store.UniqueName(stamp)
	if err != nil {
		t.Fatalf("unique name: %v", err)
	}
	if first != "backup-"+stamp+".tar.gz" {
		t.Fatalf("unexpected first name %q", first)
	}
	if err := os.WriteFile(filepath.Join(dir, first), []byte("x"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	second, err := store.UniqueName(stamp)
	if err != nil {
		t.Fatalf("unique name: %v", err)
	}
	if second != "backup-"+stamp+"-2.tar.gz" {
		t.Fatalf("unexpected second name %q", second)
	}
}
