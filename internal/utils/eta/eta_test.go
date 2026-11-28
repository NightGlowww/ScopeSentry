package eta

import (
	"math"
	"testing"
)

// 同一套估算口径同时服务节点页与任务列表：这里的期望值就是两边共用的口径。
func TestEstimateMinutesClampsBothEnds(t *testing.T) {
	samples := []float64{0.2, 0.5, 31, 33, 34, 35, 36, 3000}
	if got := EstimateMinutes(samples, 1, 1440); math.Abs(got-34) > 0.01 {
		t.Fatalf("中位数应为 34，实际 %v", got)
	}
}

func TestEstimateMinutesEvenCountAveragesMiddle(t *testing.T) {
	if got := EstimateMinutes([]float64{30, 50}, 1, 1440); math.Abs(got-40) > 0.01 {
		t.Fatalf("偶数样本应取中间两值平均 40，实际 %v", got)
	}
}

func TestEstimateMinutesNoUsableSample(t *testing.T) {
	if got := EstimateMinutes(nil, 1, 1440); got != 0 {
		t.Fatalf("无样本应为 0，实际 %v", got)
	}
	if got := EstimateMinutes([]float64{0.1, 0.2, 0.3}, 1, 1440); got != 0 {
		t.Fatalf("只有秒级空任务应返回 0，实际 %v", got)
	}
	if got := EstimateMinutes([]float64{2000, 3000}, 1, 1440); got != 0 {
		t.Fatalf("全是异常长样本应返回 0，实际 %v", got)
	}
}

// prod 实测：完整狩猎 5~21 小时是常态，不能被当成异常丢掉
func TestEstimateMinutesKeepsLongRealTasks(t *testing.T) {
	if got := EstimateMinutes([]float64{900}, 1, 1440); math.Abs(got-900) > 0.01 {
		t.Fatalf("15 小时的真实任务应保留，实际 %v", got)
	}
}
