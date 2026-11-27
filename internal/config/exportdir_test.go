package config

import (
	"path/filepath"
	"testing"
)

// 导出目录：没配置时回退到 <ExeDir>/files/export（老行为不变），
// 配置了就严格用它——容器部署必须能指到挂载卷上，否则重建即丢文件。
func TestExportPath(t *testing.T) {
	if got := (SystemConfig{ExeDir: "/opt/app"}).ExportPath(); got != filepath.Join("/opt/app", "files", "export") {
		t.Fatalf("fallback = %q", got)
	}
	if got := (SystemConfig{ExeDir: "/opt/app", ExportDir: "/data/exports"}).ExportPath(); got != "/data/exports" {
		t.Fatalf("configured = %q", got)
	}
	if got := (SystemConfig{ExeDir: "/opt/app", ExportDir: "   "}).ExportPath(); got != filepath.Join("/opt/app", "files", "export") {
		t.Fatalf("blank should fall back, got %q", got)
	}
}

// 备份目录与导出一致：没配置回退老路径，配置了严格用它（容器部署必须能指到挂载卷）。
func TestBackupPath(t *testing.T) {
	if got := (SystemConfig{ExeDir: "/opt/app"}).BackupPath(); got != filepath.Join("/opt/app", "files", "backups") {
		t.Fatalf("fallback = %q", got)
	}
	if got := (SystemConfig{ExeDir: "/opt/app", BackupDir: "/data/backups"}).BackupPath(); got != "/data/backups" {
		t.Fatalf("configured = %q", got)
	}
	if got := (SystemConfig{ExeDir: "/opt/app", BackupDir: "  "}).BackupPath(); got != filepath.Join("/opt/app", "files", "backups") {
		t.Fatalf("blank should fall back, got %q", got)
	}
}

// 图片/上传目录与导出一致：可配置到挂载卷，未配置回退老路径。
func TestImageAndUploadPath(t *testing.T) {
	base := SystemConfig{ExeDir: "/opt/app"}
	if got := base.ImagePath(); got != filepath.Join("/opt/app", "images") {
		t.Fatalf("image fallback = %q", got)
	}
	if got := base.UploadPath(); got != filepath.Join("/opt/app", "uploads") {
		t.Fatalf("upload fallback = %q", got)
	}
	cfg := SystemConfig{ExeDir: "/opt/app", ImageDir: "/data/images", UploadDir: "/data/uploads"}
	if got := cfg.ImagePath(); got != "/data/images" {
		t.Fatalf("image configured = %q", got)
	}
	if got := cfg.UploadPath(); got != "/data/uploads" {
		t.Fatalf("upload configured = %q", got)
	}
}
