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
