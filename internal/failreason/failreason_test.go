package failreason

import (
	"testing"
)

// 历史失败原因是 Go 侧拼的中文句子；这里锁住「能还原成 code+params」这条，
// 否则英文界面上那批老记录永远显示中文（BackfillFailCodes 依赖它）。
func TestParse(t *testing.T) {
	cases := []struct {
		name   string
		reason string
		code   string
		want   map[string]string
	}{
		{
			name:   "phantom",
			reason: "任务标记为运行中，但没有任何节点在跑、也不在队列里，且 12m0s 没有进度写入；判定为失败，可 retest 重跑",
			code:   "phantomNoProgress",
			want:   map[string]string{"since": "12m0s"},
		},
		{
			name:   "node heartbeat",
			reason: "节点 node-jp-01 心跳停在 2026-11-27 21:34:22（超过 15m0s 未更新，远大于正常重启所需的 ~7 分钟），已自动释放队列；该任务判定为失败，可 retest 重跑",
			code:   "nodeHeartbeatStale",
			want:   map[string]string{"node": "node-jp-01", "at": "2026-11-27 21:34:22", "after": "15m0s"},
		},
		{
			name:   "stale with auto resume",
			reason: "超过 1h30m0s 无进展，且不在任何节点队列里（最后活动 2026-11-26 01:37:32）；多半是节点重启/更新打断了执行；已自动重投 0 次仍未跑起来",
			code:   "staleNoProgress",
			want:   map[string]string{"after": "1h30m0s", "last": "2026-11-26 01:37:32", "autoResume": "0"},
		},
		{
			name:   "stale without auto resume suffix",
			reason: "超过 30m0s 无进展，且不在任何节点队列里（最后活动 2026-11-25 10:00:00）；多半是节点重启/更新打断了执行",
			code:   "staleNoProgress",
			want:   map[string]string{"after": "30m0s", "last": "2026-11-25 10:00:00", "autoResume": "0"},
		},
		{
			name:   "reconciled stale finalize",
			reason: "reconciled: 节点已不在执行该任务，最后一次进度写入于 2026-11-26 10:00:00（进度 92%），按失败收尾",
			code:   "reconciledStale",
			want:   map[string]string{"last": "2026-11-26 10:00:00", "progress": "92"},
		},
		{
			name:   "queue expired",
			reason: "排队超过 72h0m0s 仍未开始（创建于 2026-11-22 23:26:22），已过期并移出队列；需要的话 retest 即可重新排队",
			code:   "queueExpired",
			want:   map[string]string{"after": "72h0m0s", "created": "2026-11-22 23:26:22"},
		},
		{name: "unknown stays untouched", reason: "队列积压：suffix quota exceeded", code: ""},
		{name: "empty", reason: "   ", code: ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, params := Parse(c.reason)
			if code != c.code {
				t.Fatalf("code = %q, want %q", code, c.code)
			}
			if c.code == "" {
				return
			}
			for k, v := range c.want {
				if params[k] != v {
					t.Fatalf("params[%q] = %q, want %q (all=%v)", k, params[k], v, params)
				}
			}
		})
	}
}
