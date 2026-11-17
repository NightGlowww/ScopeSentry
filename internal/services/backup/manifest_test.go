package backup

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

func sampleManifest() models.BackupManifest {
	return FinalizeManifest(models.BackupManifest{
		Version:   BackupVersion,
		CreatedAt: "2026-01-01T12:00:00+08:00",
		Collections: []models.BackupCollectionEntry{
			{Name: "asset", File: CollectionFileName("asset"), Documents: 2, SHA256: strings.Repeat("a", 64)},
			{Name: "RootDomain", File: CollectionFileName("RootDomain"), Documents: 0, SHA256: strings.Repeat("b", 64)},
		},
		Uploads: models.BackupDirSummary{Files: 1, Bytes: 5},
		Files:   models.BackupDirSummary{Files: 2, Bytes: 9},
	})
}

func TestManifestBuildAndVerify(t *testing.T) {
	m := sampleManifest()
	if len(m.SHA256) != 64 {
		t.Fatalf("manifest digest not set: %q", m.SHA256)
	}
	if err := VerifyManifest(m); err != nil {
		t.Fatalf("generated manifest must verify: %v", err)
	}
	// 摘要必须只由内容决定（可复现）。
	if again := FinalizeManifest(m); again.SHA256 != m.SHA256 {
		t.Fatalf("manifest digest is not stable: %s != %s", again.SHA256, m.SHA256)
	}
	if CollectionFileName("asset") != "collections/asset.bson" {
		t.Fatalf("unexpected collection file name %q", CollectionFileName("asset"))
	}

	// JSON 往返后仍应校验通过。
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	var decoded models.BackupManifest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if err := VerifyManifest(decoded); err != nil {
		t.Fatalf("round-tripped manifest must verify: %v", err)
	}
}

func TestManifestDetectsTampering(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*models.BackupManifest)
	}{
		{"document count", func(m *models.BackupManifest) { m.Collections[0].Documents++ }},
		{"collection file", func(m *models.BackupManifest) { m.Collections[0].File = "collections/evil.bson" }},
		{"uploads count", func(m *models.BackupManifest) { m.Uploads.Files++ }},
		{"files bytes", func(m *models.BackupManifest) { m.Files.Bytes++ }},
		{"stored sha", func(m *models.BackupManifest) { m.SHA256 = strings.Repeat("0", 64) }},
		{"version", func(m *models.BackupManifest) { m.Version++ }},
		{"duplicate collection", func(m *models.BackupManifest) {
			m.Collections = append(m.Collections, m.Collections[0])
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := sampleManifest()
			tc.mutate(&m)
			err := VerifyManifest(m)
			if err == nil {
				t.Fatalf("tampered manifest (%s) must be rejected", tc.name)
			}
			if !errors.Is(err, ErrManifestInvalid) {
				t.Fatalf("expected ErrManifestInvalid, got %v", err)
			}
		})
	}
}

func TestManifestRejectsUnsafeCollectionNames(t *testing.T) {
	bad := []string{"", ".", "..", "../evil", "sub/dir", "sub\\dir", "system.users", "bad\x00name"}
	for _, name := range bad {
		if err := ValidateCollectionName(name); err == nil {
			t.Fatalf("expected collection name %q to be rejected", name)
		}
	}
	good := []string{"asset", "RootDomain", "BackupRoundTripTest", "fs.files", "a-b_c.d"}
	for _, name := range good {
		if err := ValidateCollectionName(name); err != nil {
			t.Fatalf("expected collection name %q to be accepted: %v", name, err)
		}
	}
	if _, err := ParseCollectionFileName("collections/asset.bson"); err != nil {
		t.Fatalf("parse valid collection file: %v", err)
	}
	for _, file := range []string{"asset.bson", "collections/evil.bson/../x.bson", "collections/.bson"} {
		if _, err := ParseCollectionFileName(file); err == nil {
			t.Fatalf("expected collection file %q to be rejected", file)
		}
	}
}
