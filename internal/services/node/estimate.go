package node

import "github.com/Autumn-27/ScopeSentry/internal/utils/eta"

// estTaskMinutes 保留薄封装：节点页与任务列表必须用同一个估算口径（见 utils/eta）。
func estTaskMinutes(samples []float64, minMin, maxMin float64) float64 {
	return eta.EstimateMinutes(samples, minMin, maxMin)
}
