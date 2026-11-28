package node

import (
	"math"
	"testing"
)

// 两种真实事故都要覆盖：均值被长尾拉爆、中位数被秒级任务拉低。
func TestEstTaskMinutesClampsBothEnds(t *testing.T) {
	// 0.2/0.5 分钟是「注册即完成」的空任务，600/1440 是暂停过夜的样本
	samples := []float64{0.2, 0.5, 31, 35, 33, 36, 34, 600, 1440}
	got := estTaskMinutes(samples, 3, 240)
	if math.Abs(got-34) > 0.01 {
		t.Fatalf("中位数应为 34，实际 %v", got)
	}
}

func TestEstTaskMinutesEvenCountAveragesMiddle(t *testing.T) {
	got := estTaskMinutes([]float64{30, 50}, 3, 240)
	if math.Abs(got-40) > 0.01 {
		t.Fatalf("偶数样本应取中间两值平均 40，实际 %v", got)
	}
}

func TestEstTaskMinutesNoUsableSample(t *testing.T) {
	if got := estTaskMinutes(nil, 3, 240); got != 0 {
		t.Fatalf("无样本应为 0（未知），实际 %v", got)
	}
	// 全是秒级空任务：中位数会给出 1 分钟这种误导值，这里要求返回 0
	if got := estTaskMinutes([]float64{0.1, 0.2, 0.3}, 3, 240); got != 0 {
		t.Fatalf("只有空任务时应返回 0，实际 %v", got)
	}
	if got := estTaskMinutes([]float64{500, 900}, 3, 240); got != 0 {
		t.Fatalf("全是异常长样本时应返回 0，实际 %v", got)
	}
}
