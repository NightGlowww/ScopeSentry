package jobs

// 资产去重定时任务。
//
// 以前配置页的「去重开关 + 去重周期」完全没人读：调度器没有 deduplication handler，
// 全仓也没有任何定时注册，只有「立即运行一次」真的会跑。用户以为每 N 小时自动去重，实际从不运行。

import (
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/common"
)

// Deduplication 按配置执行一轮资产去重（哪些集合由配置决定）。
func Deduplication() error {
	return common.NewDedupService().DoAssetDeduplication()
}
