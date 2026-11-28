package node

import (
	"math"
	"testing"
)

// 两种真实事故都要覆盖：均值被长尾拉爆、中位数被秒级任务拉低。
func TestEstTaskMinutesClampsBothEnds(t *testing.T) {
	// 0.2/0.5 分钟是「注册即完成」的空任务（<1 丢弃），3000 分钟是暂停过夜（>1440 丢弃）
	samples := []float64{0.2, 0.5, 31, 33, 34, 35, 36, 3000}
	got := estTaskMinutes(samples, 1, 1440)
	if math.Abs(got-34) > 0.01 {
		t.Fatalf("中位数应为 34，实际 %v", got)
	}
}

// 上限 24 小时是 prod 实测出来的：完整狩猎跑 5~21 小时是常态，
// 原来的 240 分钟上限会把「最近几条恰好都是长任务」整批丢掉，ETA 永远是未知。
func TestEstTaskMinutesKeepsLongRealTasks(t *testing.T) {
	got := estTaskMinutes([]float64{900}, 1, 1440)
	if math.Abs(got-900) > 0.01 {
		t.Fatalf("15 小时的真实任务应保留，实际 %v", got)
	}
}

func TestEstTaskMinutesEvenCountAveragesMiddle(t *testing.T) {
	got := estTaskMinutes([]float64{30, 50}, 1, 1440)
	if math.Abs(got-40) > 0.01 {
		t.Fatalf("偶数样本应取中间两值平均 40，实际 %v", got)
	}
}

func TestEstTaskMinutesNoUsableSample(t *testing.T) {
	if got := estTaskMinutes(nil, 1, 1440); got != 0 {
		t.Fatalf("无样本应为 0（未知），实际 %v", got)
	}
	// 全是秒级空任务：低于 1 分钟一律丢弃，样本为空就返回 0（别给误导性的 1 分钟）
	if got := estTaskMinutes([]float64{0.1, 0.2, 0.3}, 1, 1440); got != 0 {
		t.Fatalf("只有空任务时应返回 0，实际 %v", got)
	}
	if got := estTaskMinutes([]float64{2000, 3000}, 1, 1440); got != 0 {
		t.Fatalf("全是异常长样本时应返回 0，实际 %v", got)
	}
}