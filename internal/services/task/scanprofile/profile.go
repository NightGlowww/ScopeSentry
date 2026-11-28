// Package scanprofile 按「扫描档位」裁剪模板里的重模块。
//
// 背景（2026-11-29）：节点 maxTaskNum=1，nuclei 与目录扫描单目标各要 18~19 分钟且并行抢资源，
// 队列永远排在它们后面。nuclei 已改成模板可选；但用户还需要一个「这条任务只要资产测绘」的开关，
// 否则为了快就得手工改模板、跑完再改回来。
//
// 档位只影响**本次任务**解析出来的模板副本，不写库、不改模板本身。
package scanprofile

import (
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

const (
	// Full 按模板原样跑（默认）。
	Full = ""
	// Recon 只做资产测绘：去掉最贵的两个模块。
	Recon = "recon"
)

// heavyModules 跑起来动辄十几分钟、且会跟其他模块抢 CPU/带宽的模块。
var heavyModules = []string{"VulnerabilityScan", "DirScan"}

// Normalize 把外部传入的档位收敛成合法值，未知值按 Full 处理（不猜、不拦）。
func Normalize(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case Recon:
		return Recon
	default:
		return Full
	}
}

// Apply 按档位裁剪模板副本，返回被跳过的模块名。
// 只清插件清单与对应参数，其余字段原样保留。
func Apply(tpl *models.ScanTemplate, profile string) []string {
	if tpl == nil || Normalize(profile) != Recon {
		return nil
	}
	var dropped []string
	if len(tpl.VulnerabilityScan) > 0 {
		dropped = append(dropped, "VulnerabilityScan")
	}
	if len(tpl.DirScan) > 0 {
		dropped = append(dropped, "DirScan")
	}
	tpl.VulnerabilityScan = nil
	tpl.DirScan = nil
	if tpl.Parameters.VulnerabilityScan != nil {
		tpl.Parameters.VulnerabilityScan = map[string]string{}
	}
	if tpl.Parameters.DirScan != nil {
		tpl.Parameters.DirScan = map[string]string{}
	}
	return dropped
}

// Describe 给任务上一个可读的提示串（空 = 未启用档位）。
func Describe(dropped []string) string {
	if len(dropped) == 0 {
		return ""
	}
	return "scan_profile=recon：本次只做资产测绘，已跳过 " + strings.Join(dropped, "、")
}
