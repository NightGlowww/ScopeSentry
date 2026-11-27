package dashboard

import (
	"reflect"
	"testing"
)

// 2026-11-28 生产实测：SCAN node:* 会带回 node:<name>:currentTask（字符串键），
// 对它 HGETALL 报 WRONGTYPE，pipeline 整体失败 → 控制台「节点 0 / 0」。
func TestHeartbeatHashKeys(t *testing.T) {
	in := []string{
		"node:node-jp-01",
		"node:node-jp-01:currentTask",
		"node:node-us-02",
		"node:",
		"refresh_config:node-jp-01",
		"node:a:b:c",
	}
	want := []string{"node:node-jp-01", "node:node-us-02"}
	got := heartbeatHashKeys(in)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("heartbeatHashKeys = %v, want %v", got, want)
	}
	if len(heartbeatHashKeys(nil)) != 0 {
		t.Fatal("nil input should return empty slice")
	}
}
