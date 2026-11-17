// Package exportfilter 提供导出查询条件的纯逻辑（issue #261）。
package exportfilter

import (
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

// TimeLayout 是资产/漏洞等集合里 time 字段的格式。
// 同格式下字典序即时间序，所以可以直接用字符串比较做范围过滤。
const TimeLayout = "2006-01-02 15:04:05"

// ApplyTimeRange 把导出时间范围并进查询条件（issue #261）。
//
// start / end 允许只给一边；都为空时原样返回。base 里可能已经有 time 条件
// （用户自己按时间过滤过），这时用 $and 合并而不是覆盖。
func ApplyTimeRange(base map[string]interface{}, start, end string) (map[string]interface{}, error) {
	if start == "" && end == "" {
		return base, nil
	}
	if start != "" {
		if _, err := time.Parse(TimeLayout, start); err != nil {
			return nil, fmt.Errorf("invalid startTime %q, want %s", start, TimeLayout)
		}
	}
	if end != "" {
		if _, err := time.Parse(TimeLayout, end); err != nil {
			return nil, fmt.Errorf("invalid endTime %q, want %s", end, TimeLayout)
		}
	}
	if start != "" && end != "" && start > end {
		return nil, fmt.Errorf("startTime %s is after endTime %s", start, end)
	}

	rng := bson.M{}
	if start != "" {
		rng["$gte"] = start
	}
	if end != "" {
		rng["$lte"] = end
	}
	clause := bson.M{"time": rng}
	if len(base) == 0 {
		return clause, nil
	}
	return bson.M{"$and": []interface{}{bson.M(base), clause}}, nil
}
