package task

import (
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// span 造一个 [start, end] 两段式切片
func span(start, end string) []string { return []string{start, end} }

func TestAggregateModuleMinutesRanksByCost(t *testing.T) {
	rows := []models.TaskProgressDetail{
		{
			Target: "a.com",
			// 目标级跨度 300 分钟
			All:               span("2026-11-29 10:00:00", "2026-11-29 15:00:00"),
			SubdomainScan:     span("2026-11-29 10:00:00", "2026-11-29 10:02:00"), // 2
			VulnerabilityScan: span("2026-11-29 10:02:00", "2026-11-29 14:58:00"), // 296
		},
		{
			Target: "b.com",
			All:               span("2026-11-29 10:00:00", "2026-11-29 11:00:00"),
			SubdomainScan:     span("2026-11-29 10:00:00", "2026-11-29 10:03:00"), // 3
			VulnerabilityScan: span("2026-11-29 10:03:00", "2026-11-29 10:58:00"), // 55
		},
	}

	got := AggregateModuleMinutes(rows)

	if got.Targets != 2 {
		t.Fatalf("目标数应为 2，实际 %d", got.Targets)
	}
	if len(got.Modules) != 2 {
		t.Fatalf("应有 2 个模块，实际 %d：%+v", len(got.Modules), got.Modules)
	}
	// 耗时降序：nuclei 必须排第一，否则「时间花在哪」这句话是假的
	if got.Modules[0].Module != "VulnerabilityScan" {
		t.Fatalf("首位应为 VulnerabilityScan，实际 %s", got.Modules[0].Module)
	}
	if got.Modules[0].Minutes != 351 { // 296 + 55
		t.Fatalf("VulnerabilityScan 应为 351 分钟，实际 %v", got.Modules[0].Minutes)
	}
	if got.Modules[1].Minutes != 5 { // 2 + 3
		t.Fatalf("SubdomainScan 应为 5 分钟，实际 %v", got.Modules[1].Minutes)
	}
	// 占比加起来要满 100（同一口径下自洽）
	if sum := got.Modules[0].Percent + got.Modules[1].Percent; sum < 99.9 || sum > 100.1 {
		t.Fatalf("占比之和应在 100 附近，实际 %v", sum)
	}
	if got.TotalMinutes != 356 {
		t.Fatalf("合计应为 356，实际 %v", got.TotalMinutes)
	}
	// 跨度：300 + 60，与模块合计分开统计，不能混用
	if got.SpanMinutes != 360 {
		t.Fatalf("目标级跨度应为 360，实际 %v", got.SpanMinutes)
	}
}

func TestAggregateModuleMinutesSkipsUnmeasured(t *testing.T) {
	rows := []models.TaskProgressDetail{
		{
			Target: "a.com",
			// 还在跑：只有 start
			PortScan: []string{"2026-11-29 10:00:00", ""},
			// 用户暂停：前端塞了第三个元素当标记
			DirScan: []string{"2026-11-29 10:00:00", "2026-11-29 10:05:00", "paused"},
			// 时钟跳变：end 早于 start
			URLScan: span("2026-11-29 12:00:00", "2026-11-29 11:00:00"),
			// 脏数据：跨度 40 天
			WebCrawler: span("2026-10-01 10:00:00", "2026-11-10 10:00:00"),
			// 唯一有效的一格
			AssetHandle: span("2026-11-29 10:00:00", "2026-11-29 10:30:00"),
			// 未开始：空切片
			PortFingerprint: []string{},
		},
	}

	got := AggregateModuleMinutes(rows)

	if len(got.Modules) != 1 {
		t.Fatalf("只有 AssetHandle 该被统计，实际 %+v", got.Modules)
	}
	if got.Modules[0].Module != "AssetHandle" || got.Modules[0].Minutes != 30 {
		t.Fatalf("应为 AssetHandle 30 分钟，实际 %+v", got.Modules[0])
	}
	// 只有一个模块时占比必须是 100，不能是 0 或 NaN
	if got.Modules[0].Percent != 100 {
		t.Fatalf("单模块占比应为 100，实际 %v", got.Modules[0].Percent)
	}
	if got.TotalMinutes != 30 {
		t.Fatalf("合计应为 30，实际 %v", got.TotalMinutes)
	}
}

func TestAggregateModuleMinutesEmpty(t *testing.T) {
	got := AggregateModuleMinutes(nil)
	if got.Targets != 0 || got.TotalMinutes != 0 || got.SpanMinutes != 0 || len(got.Modules) != 0 {
		t.Fatalf("空输入应返回零值，实际 %+v", got)
	}
}
