package node

import (
	"math"
	"testing"
)

// 均值被异常值拉爆是真实事故：线上用均值算出 5 条任务要等 36 小时，
// 中位数给的是 3 小时。这里把口径钉死。
func TestEstTaskMinutesUsesMedianAndClampsOutliers(t *testing.T) {
	samples := []float64{38, 40, 42, 41, 39, 600, 1440}
	got := estTaskMinutes(samples, 240)
	if math.Abs(got-40) > 0.01 {
		t.Fatalf("中位数应为 40，实际 %v", got)
	}
}

func TestEstTaskMinutesEvenCountAveragesMiddle(t *testing.T) {
	got := estTaskMinutes([]float64{30, 50}, 240)
	if math.Abs(got-40) > 0.01 {
		t.Fatalf("偶数样本应取中间两值平均 40，实际 %v", got)
	}
}

func TestEstTaskMinutesEmptyOrAllOutliers(t *testing.T) {
	if got := estTaskMinutes(nil, 240); got != 0 {
		t.Fatalf("无样本应为 0（未知），实际 %v", got)
	}
	if got := estTaskMinutes([]float64{500, 900}, 240); got != 0 {
		t.Fatalf("全是异常值应为 0，实际 %v", got)
	}
	if got := estTaskMinutes([]float64{0, -5}, 240); got != 0 {
		t.Fatalf("非正样本应被丢掉，实际 %v", got)
	}
}
