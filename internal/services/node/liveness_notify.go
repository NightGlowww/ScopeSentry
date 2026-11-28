package node

// 节点上下线通知：把「心跳过期 / 恢复」变成一条可推送的事件。
//
// 现状：节点掉线时只有巡检事后把在跑的任务判失败，人得自己盯节点页才发现。
// 这里在存活判定的同一处做「状态变化」检测，变化才通知（不去重会每次轮询都发一条）。

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Autumn-27/ScopeSentry/internal/config"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"go.uber.org/zap"
)

// livenessSeen 进程内记录每个节点上次观测到的存活状态（true=在线）。
// 重启后第一轮只做种子、不通知：否则每次发布都会给所有节点发一遍「恢复」。
var (
	livenessMu   sync.Mutex
	livenessSeen = map[string]bool{}
)

// diffLiveness 纯函数：对比新旧状态，返回刚掉线与刚恢复的节点名（排序，便于断言）。
func diffLiveness(prev, cur map[string]bool) (down []string, up []string) {
	for name, alive := range cur {
		before, seen := prev[name]
		if !seen {
			continue // 首次观测：只记录，不通知
		}
		if before && !alive {
			down = append(down, name)
		} else if !before && alive {
			up = append(up, name)
		}
	}
	sort.Strings(down)
	sort.Strings(up)
	return down, up
}

// observeLiveness 更新进程内视图并在状态变化时发通知。
// detail 给通知补上「最后心跳」这类上下文（取不到就省略）。
func observeLiveness(cur map[string]bool, detail map[string]string) {
	livenessMu.Lock()
	down, up := diffLiveness(livenessSeen, cur)
	for name, alive := range cur {
		livenessSeen[name] = alive
	}
	livenessMu.Unlock()

	for _, name := range down {
		msg := fmt.Sprintf("[ScopeSentry] 节点「%s」已离线", name)
		if d := strings.TrimSpace(detail[name]); d != "" {
			msg += "：" + d
		}
		msg += "。在跑的任务会被巡检判失败，节点恢复后可 retest。"
		logger.Warn("node liveness changed", zap.String("node", name), zap.String("state", "down"))
		go config.Notification(msg)
	}
	for _, name := range up {
		logger.Info("node liveness changed", zap.String("node", name), zap.String("state", "up"))
		go config.Notification(fmt.Sprintf("[ScopeSentry] 节点「%s」已恢复在线", name))
	}
}
