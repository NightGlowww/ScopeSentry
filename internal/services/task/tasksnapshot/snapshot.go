// Package tasksnapshot 提供任务进度快照的纯逻辑（issue #278）。
//
// 任务完成时服务端会删掉 Redis 里的 tmp/time，逐目标进度明细也可能被清理或重启丢掉。
// 完成瞬间把 detail 快照进 Mongo，接口在 Redis 读不到时回放同一份数据。
// 这里只放不碰数据库的纯函数，方便单测。
package tasksnapshot

import (
	"sort"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// KeyPrefix 是某个任务逐目标进度 key 的前缀：TaskInfo:progress:<taskID>:
func KeyPrefix(taskID string) string {
	return "TaskInfo:progress:" + taskID + ":"
}

// TargetFromKey 从 TaskInfo:progress:<taskID>:<target> 取出目标名。
//
// 只能按前缀切、不能按冒号分段：目标本身可能带冒号（IPv6、host:port）。
func TargetFromKey(prefix, key string) (string, bool) {
	if !strings.HasPrefix(key, prefix) {
		return "", false
	}
	target := strings.TrimPrefix(key, prefix)
	if target == "" {
		return "", false
	}
	return target, true
}

// BuildRows 把「progress key → 哈希内容」整理成待落库的行。
// 按 target 排序，保证同一份数据每次落库顺序一致（便于比对与测试）。
func BuildRows(taskID string, keys []string, dataByKey map[string]map[string]string) []models.TaskProgressSnapshot {
	prefix := KeyPrefix(taskID)
	rows := make([]models.TaskProgressSnapshot, 0, len(keys))
	for _, key := range keys {
		target, ok := TargetFromKey(prefix, key)
		if !ok {
			continue
		}
		data := dataByKey[key]
		if len(data) == 0 {
			continue
		}
		rows = append(rows, models.TaskProgressSnapshot{
			TaskID: taskID,
			Target: target,
			Data:   data,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Target < rows[j].Target })
	return rows
}
