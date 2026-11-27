package models

import (
	"encoding/json"
	"strings"
	"testing"
)

// 巡检失败原因的机器可读形态：前端靠 failCode/failParams 按当前语言渲染。
// 这里锁两件事：字段能正确序列化；老数据（没有 code）不会多出空壳字段。
func TestTaskFailCodeSerialization(t *testing.T) {
	task := Task{
		FailReason: "节点 node-1 心跳停在 ...",
		FailCode:   "nodeHeartbeatStale",
		FailParams: map[string]string{"node": "node-1", "after": "15m0s"},
	}
	b, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	for _, want := range []string{"\"failCode\":\"nodeHeartbeatStale\"", "\"failParams\"", "\"node\":\"node-1\""} {
		if !strings.Contains(s, want) {
			t.Fatalf("序列化缺少 %s: %s", want, s)
		}
	}

	legacy := Task{FailReason: "超过 10m0s 无进展"}
	b2, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy: %v", err)
	}
	if strings.Contains(string(b2), "failCode") || strings.Contains(string(b2), "failParams") {
		t.Fatalf("老数据不该出现 code 字段: %s", string(b2))
	}
}