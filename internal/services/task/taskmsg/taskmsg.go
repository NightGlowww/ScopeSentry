// Package taskmsg 解析任务队列里的消息（停止任务时要清掉还没被节点领取的消息）。
package taskmsg

import "encoding/json"

// TaskID 从一条节点任务消息里取任务 ID。
//
// 队列里存在两种 payload：
//   - 服务端下发的扫描模板：{"id": "<taskID>", ...}（ScanTemplate.ID 的 json tag 是小写 id）
//   - 页面监控等固定任务：{"ID": "page_monitoring", ...}（大写 ID）
//
// 两种都认，省掉以后再加字段时的适配。
func TaskID(raw []byte) (string, bool) {
	var payload struct {
		Lower string `json:"id"`
		Upper string `json:"ID"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", false
	}
	if payload.Lower != "" {
		return payload.Lower, true
	}
	if payload.Upper != "" {
		return payload.Upper, true
	}
	return "", false
}

// BelongsTo 判断一条消息是否属于指定任务。
func BelongsTo(raw []byte, taskID string) bool {
	if taskID == "" {
		return false
	}
	id, ok := TaskID(raw)
	return ok && id == taskID
}
