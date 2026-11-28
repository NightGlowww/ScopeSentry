package scanprofile

import (
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

func sample() *models.ScanTemplate {
	return &models.ScanTemplate{
		SubdomainScan:     []string{"sub"},
		DirScan:           []string{"dir"},
		VulnerabilityScan: []string{"vul"},
		Parameters: models.Parameters{
			DirScan:           map[string]string{"dir": "-d x -t 8"},
			VulnerabilityScan: map[string]string{"vul": "-s high"},
		},
	}
}

func TestApplyReconDropsHeavyModules(t *testing.T) {
	tpl := sample()
	dropped := Apply(tpl, Recon)
	if len(dropped) != 2 {
		t.Fatalf("dropped = %v, want 2 个模块", dropped)
	}
	if len(tpl.DirScan) != 0 || len(tpl.VulnerabilityScan) != 0 {
		t.Fatalf("重模块未清空: DirScan=%v VulnerabilityScan=%v", tpl.DirScan, tpl.VulnerabilityScan)
	}
	if len(tpl.Parameters.DirScan) != 0 || len(tpl.Parameters.VulnerabilityScan) != 0 {
		t.Fatalf("重模块参数未清空")
	}
	// 非重模块必须原样保留，否则「只做资产测绘」会变成「什么都没做」
	if len(tpl.SubdomainScan) != 1 {
		t.Fatalf("SubdomainScan 被误删: %v", tpl.SubdomainScan)
	}
}

func TestApplyFullKeepsTemplate(t *testing.T) {
	tpl := sample()
	if dropped := Apply(tpl, Full); len(dropped) != 0 {
		t.Fatalf("Full 档不应裁剪，got %v", dropped)
	}
	if len(tpl.DirScan) != 1 || len(tpl.VulnerabilityScan) != 1 {
		t.Fatalf("Full 档改动了模板")
	}
}

func TestNormalizeUnknownFallsBackToFull(t *testing.T) {
	for _, in := range []string{"", "  ", "full", "whatever"} {
		if got := Normalize(in); got != Full {
			t.Fatalf("Normalize(%q) = %q, want %q", in, got, Full)
		}
	}
	if got := Normalize(" RECON "); got != Recon {
		t.Fatalf("Normalize( RECON ) = %q, want recon", got)
	}
}

func TestApplyNilTemplateSafe(t *testing.T) {
	if dropped := Apply(nil, Recon); dropped != nil {
		t.Fatalf("nil 模板应安全返回 nil")
	}
}
