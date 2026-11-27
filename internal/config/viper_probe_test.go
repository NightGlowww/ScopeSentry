package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

// 复现：AddConfigPath(dir) + SetConfigName(config) + SetConfigType(yaml) 能否读到 config.yaml。
// 容器里出现「配置文件改了不生效、ConfigFileUsed 为空」时，先用这个确认 viper 行为本身没问题。
func TestViperReadsConfigFromExeDir(t *testing.T) {
	dir := t.TempDir()
	content := "system:\n  export_dir: /data/x\n  backup_dir: /data/b\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	v := viper.New()
	v.AddConfigPath(dir)
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	if err := v.ReadInConfig(); err != nil {
		t.Fatalf("ReadInConfig: %v", err)
	}
	if v.ConfigFileUsed() == "" {
		t.Fatal("ConfigFileUsed empty")
	}
	if got := v.GetString("system.export_dir"); got != "/data/x" {
		t.Fatalf("export_dir = %q", got)
	}
}
