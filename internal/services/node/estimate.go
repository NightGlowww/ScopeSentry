package node

import (
	"sort"
)

// estTaskMinutes 从最近任务的耗时样本里估「一个任务大概跑多久」（分钟）。
//
// 为什么不是均值：线上实测均值 430 分钟/任务，中位数约 40 —— 差距全来自
// 「暂停过夜」「发布打断后自动重投」这类样本。用均值算出来的排队时长
// 会把 5 条任务估成 36 小时，比不说更误导（2026-11-29 实测）。
//
// 口径：先丢掉超过 clampMin 的样本（那不是正常执行时长），再取中位数。
// 样本为空返回 0，调用方据此显示「未知」而不是瞎猜。
func estTaskMinutes(samples []float64, clampMin float64) float64 {
	if clampMin <= 0 {
		clampMin = 240
	}
	kept := make([]float64, 0, len(samples))
	for _, v := range samples {
		if v <= 0 || v > clampMin {
			continue
		}
		kept = append(kept, v)
	}
	if len(kept) == 0 {
		return 0
	}
	sort.Float64s(kept)
	mid := len(kept) / 2
	if len(kept)%2 == 1 {
		return kept[mid]
	}
	return (kept[mid-1] + kept[mid]) / 2
}
