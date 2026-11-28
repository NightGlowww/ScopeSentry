package taskstats

import (
	"strings"
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/constants"
	"go.mongodb.org/mongo-driver/bson"
)

// 通知正文是这条链路唯一对外可见的东西：数字对不上、状态判断错，比不发还糟。
func TestFormatTerminalMessage(t *testing.T) {
	done := bson.M{"status": constants.TaskStatusFinished, "stats": bson.M{
		"subdomain": int64(12), "url": int64(340), "asset": int64(0), "vulnerability": int64(3),
	}}
	if got := FormatTerminalMessage("chime 泄露监控", done); got != "[ScopeSentry] 任务「chime 泄露监控」完成：子域 12 · URL 340 · 漏洞 3" {
		t.Fatalf("完成消息不对: %q", got)
	}

	empty := bson.M{"status": constants.TaskStatusFinished, "stats": bson.M{"subdomain": int64(0)}}
	if got := FormatTerminalMessage("t", empty); !strings.Contains(got, "本次没有产出") {
		t.Fatalf("零产出应说明原因: %q", got)
	}

	failed := bson.M{"status": constants.TaskStatusFailed, "failReason": "节点 15 分钟没有进度写入"}
	if got := FormatTerminalMessage("t", failed); !strings.Contains(got, "失败：节点 15 分钟没有进度写入") {
		t.Fatalf("失败消息应带原因: %q", got)
	}

	if got := FormatTerminalMessage("t", bson.M{"status": constants.TaskStatusFailed}); !strings.Contains(got, "未提供原因") {
		t.Fatalf("缺原因时应有兜底文案: %q", got)
	}

	// 非终态不发：运行中/暂停/排队都不该打扰用户
	for _, st := range []int{constants.TaskStatusPending, constants.TaskStatusRunning, constants.TaskStatusPaused} {
		if got := FormatTerminalMessage("t", bson.M{"status": st}); got != "" {
			t.Fatalf("状态 %d 不应产生通知: %q", st, got)
		}
	}
	// 空任务名不发：空名字的过滤条件等于全库
	if got := FormatTerminalMessage("  ", done); got != "" {
		t.Fatalf("空任务名不应产生通知: %q", got)
	}
}
