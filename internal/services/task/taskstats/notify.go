package taskstats

// 任务终态通知。
//
// 通知配置（config.NotificationApi）一直是死的：界面能加 webhook、能测，
// 但全仓没有一处调用 config.Notification —— 配了钉钉/飞书/Slack 也不会有一条消息。
// 这里把「任务完成 / 失败」接上，且只在终态写入成功之后才发。

import (
	"fmt"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/config"
	"github.com/Autumn-27/ScopeSentry/internal/constants"
	"go.mongodb.org/mongo-driver/bson"
)

// 产出维度的中文名，与 stats 快照键一一对应（顺序即展示顺序）
var statLabels = []struct{ key, label string }{
	{"subdomain", "子域"},
	{"url", "URL"},
	{"asset", "资产"},
	{"vulnerability", "漏洞"},
	{"ip", "IP"},
	{"sensitive", "敏感"},
}

// FormatTerminalMessage 生成终态通知正文；非终态或任务名为空时返回空串。
//
// 抽成纯函数是为了能单测：消息是这条链路唯一对外可见的东西，
// 数字对不上（比如把 stats 里的 count 全写成 0）比不发还糟。
func FormatTerminalMessage(taskName string, set bson.M) string {
	name := strings.TrimSpace(taskName)
	if name == "" {
		return ""
	}
	status, _ := toInt(set["status"])
	if !IsTerminal(status) {
		return ""
	}
	if status == constants.TaskStatusFailed {
		reason, _ := set["failReason"].(string)
		if strings.TrimSpace(reason) == "" {
			reason = "未提供原因"
		}
		return fmt.Sprintf("[ScopeSentry] 任务「%s」失败：%s", name, reason)
	}
	parts := make([]string, 0, len(statLabels))
	stats, _ := set["stats"].(bson.M)
	for _, item := range statLabels {
		if stats == nil {
			break
		}
		if n, ok := toInt(stats[item.key]); ok && n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", item.label, n))
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("[ScopeSentry] 任务「%s」完成（本次没有产出）", name)
	}
	return fmt.Sprintf("[ScopeSentry] 任务「%s」完成：%s", name, strings.Join(parts, " · "))
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

// NotifyTerminal 在终态写入成功之后调用：异步发送，绝不阻塞状态收敛。
// 通知接口超时/挂掉都不影响任务本身 —— 通知只是附带动作。
func NotifyTerminal(taskName string, set bson.M) {
	msg := FormatTerminalMessage(taskName, set)
	if msg == "" {
		return
	}
	go config.Notification(msg)
}
