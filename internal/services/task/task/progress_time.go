package task

import (
	"math"
	"sort"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// 进度里的时间戳布局：节点写 Redis 时就用的这个格式（helper.GetNowTimeString）。
const progressTimeLayout = "2006-01-02 15:04:05"

// 单格耗时的上限：30 天。超过基本可以判定是脏数据（节点时钟跳变、手工改库），
// 与其把一个模块的占比算成 99%，不如当它「没测到」。
const maxModuleSpanMinutes = 30 * 24 * 60

// modulePair 模块名 + 它的 [start, end] 两段式切片。
type modulePair struct {
	name string
	pair []string
}

// modulePairs 列出进度矩阵里的模块列，顺序与流水线一致。
//
// 为什么手写而不是反射：字段名就是前端拼 i18n key（scanTemplate.<名>）的依据，
// 反射拿到的名字一旦跟着结构体标签漂移，前端会静默退化成英文 key。手写多一行，改错会编译不过。
func modulePairs(p *models.TaskProgressDetail) []modulePair {
	return []modulePair{
		{"TargetHandler", p.TargetHandler},
		{"SubdomainScan", p.SubdomainScan},
		{"SubdomainSecurity", p.SubdomainSecurity},
		{"PortScanPreparation", p.PortScanPreparation},
		{"PortScan", p.PortScan},
		{"PortFingerprint", p.PortFingerprint},
		{"AssetMapping", p.AssetMapping},
		{"AssetHandle", p.AssetHandle},
		{"URLScan", p.URLScan},
		{"WebCrawler", p.WebCrawler},
		{"URLSecurity", p.URLSecurity},
		{"DirScan", p.DirScan},
		{"VulnerabilityScan", p.VulnerabilityScan},
	}
}

// AggregateModuleMinutes 汇总「时间花在哪」：模块名 → 累计分钟 + 占比，另附目标级跨度。
//
// 纯函数，不碰 Redis/Mongo，方便单测；调用方决定传哪一页的目标。
func AggregateModuleMinutes(rows []models.TaskProgressDetail) models.TaskTimeSpend {
	acc := make(map[string]float64, len(modulePairs(&models.TaskProgressDetail{})))
	span := 0.0
	out := models.TaskTimeSpend{Targets: len(rows)}

	for i := range rows {
		if d, ok := pairMinutes(rows[i].All); ok {
			span += d
		}
		for _, m := range modulePairs(&rows[i]) {
			if d, ok := pairMinutes(m.pair); ok {
				acc[m.name] += d
			}
		}
	}

	total := 0.0
	for _, v := range acc {
		total += v
	}
	for name, v := range acc {
		pct := 0.0
		if total > 0 {
			pct = v / total * 100
		}
		out.Modules = append(out.Modules, models.ModuleTime{
			Module:  name,
			Minutes: round1(v),
			Percent: round1(pct),
		})
	}
	sort.Slice(out.Modules, func(i, j int) bool {
		if out.Modules[i].Minutes == out.Modules[j].Minutes {
			return out.Modules[i].Module < out.Modules[j].Module
		}
		return out.Modules[i].Minutes > out.Modules[j].Minutes
	})
	out.TotalMinutes = round1(total)
	out.SpanMinutes = round1(span)
	return out
}

// pairMinutes 把 [start, end] 换算成分钟。
//
// 三种情况算「没测到」：缺一端（还在跑或没跑）、时间解析不了、end <= start（时钟跳变）。
// 被用户暂停的目标前端会塞第三种元素（长度 3），这里按长度拦掉，不会把标记当时间解析。
func pairMinutes(pair []string) (float64, bool) {
	if len(pair) != 2 || pair[0] == "" || pair[1] == "" {
		return 0, false
	}
	start, err := time.ParseInLocation(progressTimeLayout, pair[0], time.Local)
	if err != nil {
		return 0, false
	}
	end, err := time.ParseInLocation(progressTimeLayout, pair[1], time.Local)
	if err != nil {
		return 0, false
	}
	d := end.Sub(start).Minutes()
	if d <= 0 || d > maxModuleSpanMinutes {
		return 0, false
	}
	return d, true
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
