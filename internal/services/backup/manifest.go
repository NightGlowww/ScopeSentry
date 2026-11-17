// services/backup-------------------------------------
// @file      : manifest.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2026/11/18 01:18
// -------------------------------------------

package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

const (
	// BackupVersion 备份包格式版本
	BackupVersion = 1
	// ManifestFileName 清单文件名
	ManifestFileName = "manifest.json"
	// CollectionsDir 集合 BSON 流在包内的目录
	CollectionsDir = "collections"
)

// ErrManifestInvalid manifest 结构或内容校验失败
var ErrManifestInvalid = errors.New("invalid backup manifest")

// ComputeManifestSHA256 计算 manifest 的规范化摘要（不含 sha256 字段本身）。
// 因为集合按名称排序、目录计数固定，同一份数据生成的摘要稳定可复现。
func ComputeManifestSHA256(m models.BackupManifest) string {
	canonical := m
	canonical.SHA256 = ""
	data, err := json.Marshal(canonical)
	if err != nil {
		// 结构体只含基础类型，正常不会失败；失败时返回空串让校验侧拒绝。
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// FinalizeManifest 写入最终摘要
func FinalizeManifest(m models.BackupManifest) models.BackupManifest {
	m.SHA256 = ComputeManifestSHA256(m)
	return m
}

// VerifyManifest 校验版本、摘要、集合清单与目录计数。
func VerifyManifest(m models.BackupManifest) error {
	if m.Version != BackupVersion {
		return fmt.Errorf("%w: unsupported version %d", ErrManifestInvalid, m.Version)
	}
	if len(m.SHA256) != 64 {
		return fmt.Errorf("%w: missing sha256", ErrManifestInvalid)
	}
	if expected := ComputeManifestSHA256(m); expected != m.SHA256 {
		return fmt.Errorf("%w: sha256 mismatch (want %s got %s)", ErrManifestInvalid, expected, m.SHA256)
	}
	if m.Uploads.Files < 0 || m.Uploads.Bytes < 0 || m.Files.Files < 0 || m.Files.Bytes < 0 {
		return fmt.Errorf("%w: negative directory counts", ErrManifestInvalid)
	}

	seen := make(map[string]struct{}, len(m.Collections))
	for _, entry := range m.Collections {
		if err := ValidateCollectionName(entry.Name); err != nil {
			return fmt.Errorf("%w: %v", ErrManifestInvalid, err)
		}
		if _, dup := seen[entry.Name]; dup {
			return fmt.Errorf("%w: duplicate collection %q", ErrManifestInvalid, entry.Name)
		}
		seen[entry.Name] = struct{}{}
		if entry.File != CollectionFileName(entry.Name) {
			return fmt.Errorf("%w: collection %q has unexpected file %q", ErrManifestInvalid, entry.Name, entry.File)
		}
		if entry.Documents < 0 {
			return fmt.Errorf("%w: collection %q has negative document count", ErrManifestInvalid, entry.Name)
		}
		if len(entry.SHA256) != 64 {
			return fmt.Errorf("%w: collection %q has invalid stream sha256", ErrManifestInvalid, entry.Name)
		}
	}
	return nil
}

// CollectionFileName 集合在包内的相对路径
func CollectionFileName(name string) string {
	return CollectionsDir + "/" + name + ".bson"
}

// ParseCollectionFileName 解析 collections/<name>.bson，返回集合名。
func ParseCollectionFileName(file string) (string, error) {
	if !strings.HasPrefix(file, CollectionsDir+"/") || !strings.HasSuffix(file, ".bson") {
		return "", fmt.Errorf("%w: %q is not a collection stream", ErrManifestInvalid, file)
	}
	name := strings.TrimSuffix(strings.TrimPrefix(file, CollectionsDir+"/"), ".bson")
	if err := ValidateCollectionName(name); err != nil {
		return "", err
	}
	return name, nil
}

// ValidateCollectionName 集合名必须能安全地作为文件名的一部分。
func ValidateCollectionName(name string) error {
	if name == "" {
		return fmt.Errorf("empty collection name")
	}
	if len(name) > 200 {
		return fmt.Errorf("collection name %q is too long", name)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("collection name %q is not allowed", name)
	}
	if strings.HasPrefix(name, "system.") {
		return fmt.Errorf("collection name %q is a system collection", name)
	}
	if strings.ContainsAny(name, "/\\") || strings.ContainsRune(name, 0) {
		return fmt.Errorf("collection name %q contains a path separator", name)
	}
	if !utf8.ValidString(name) {
		return fmt.Errorf("collection name %q is not valid utf-8", name)
	}
	return nil
}
