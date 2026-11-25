package template

import (
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// 模板完整性校验：核心模块缺失必须被拦下。
// 这条规则来自 2026-11-26 的真实事故——6 个模板里 5 个缺模块，
// 任务照常跑、界面照常出资产，但 122 个 URL 里 0 个 API 端点，覆盖缺口完全不可见。
func TestMissingCoreModules(t *testing.T) {
	full := &models.ScanTemplate{
		SubdomainScan:     []string{"a"},
		PortScan:          []string{"a"},
		PortFingerprint:   []string{"a"},
		AssetMapping:      []string{"a"},
		AssetHandle:       []string{"a"},
		URLScan:           []string{"a"},
		WebCrawler:        []string{"a"},
		URLSecurity:       []string{"a"},
		DirScan:           []string{"a"},
		VulnerabilityScan: []string{"a"},
	}
	if got := missingCoreModules(full); len(got) != 0 {
		t.Fatalf("完整模板不该报缺失，得到 %v", got)
	}

	// 少了爬虫和目录扫描——正是 felix 那次的实际形态
	broken := *full
	broken.WebCrawler = nil
	broken.DirScan = []string{}
	got := missingCoreModules(&broken)
	if len(got) != 2 {
		t.Fatalf("应报 2 个缺失，得到 %v", got)
	}
	if got[0] != "WebCrawler" || got[1] != "DirScan" {
		t.Fatalf("缺失模块名不对：%v", got)
	}

	// 可选模块缺失不该被拦
	minimal := *full
	minimal.SubdomainSecurity = nil
	minimal.PortScanPreparation = nil
	minimal.TargetHandler = nil
	if got := missingCoreModules(&minimal); len(got) != 0 {
		t.Fatalf("可选模块缺失不该报错，得到 %v", got)
	}
}
