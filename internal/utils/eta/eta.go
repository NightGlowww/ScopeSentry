package eta

import "sort"

// EstimateMinutes 从历史样本估「一个任务大概跑多久」（分钟）。
//
// 纯函数：节点页的「队列还要等多久」和任务列表的「排队预计等待」必须用同一套口径，
// 否则同一时刻两个页面会给出互相矛盾的数字。
//
// 两次踩坑（2026-11-29 线上）：
//  1. 用均值：被「暂停过夜 / 发布打断后自动重投」的样本拉爆，5 条任务估成 36 小时；
//  2. 只用中位数：被大量「注册即完成」的秒级任务拉到 1 分钟，5 条任务估成 5 分钟。
//
// 所以两端都要截：下限 1 分钟（注册即完成也要算数），上限 24 小时（更长的多半是暂停/等待）。
// 过滤后取中位数；没有可用样本返回 0（调用方显示未知，不猜）。
func EstimateMinutes(samples []float64, minMin, maxMin float64) float64 {
	if minMin <= 0 {
		minMin = 1
	}
	if maxMin <= minMin {
		maxMin = 1440
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
