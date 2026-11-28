package node

import (
	"sort"
)

// estTaskMinutes 从最近任务的耗时样本里估「一个任务大概跑多久」（分钟）。
//
// 两次踩坑（都在 2026-11-29 线上）：
//  1. 用均值：被「暂停过夜 / 发布打断后自动重投」的样本拉爆，5 条任务估成 36 小时；
//  2. 只用中位数：被大量「注册即完成」「目标为空」的秒级任务拉到 1 分钟，5 条任务估成 5 分钟。
//
// 所以两端都要截：真实扫描任务至少是分钟级，最长也就几小时。
// 过滤后取中位数；没有可用样本返回 0（界面显示未知，不猜）。
func estTaskMinutes(samples []float64, minMin, maxMin float64) float64 {
	if minMin <= 0 {
		minMin = 3
	}
	if maxMin <= minMin {
		maxMin = 240
	}
	kept := make([]float64, 0, len(samples))
	for _, v := range samples {
		if v < minMin || v > maxMin {
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
