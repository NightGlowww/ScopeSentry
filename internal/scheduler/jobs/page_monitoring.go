// Package jobs -----------------------------
// @file      : page_monitoring.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2025/4/30 15:18
// -------------------------------------------
package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func CreatePageMonitoringTask() error {
	logger.Info("create_page_monitoring_task")
	filter := bson.M{"id": "page_monitoring"}
	opts := options.FindOne()
	var task models.Task
	err := ScanSvc.collection.FindOne(context.Background(), filter, opts).Decode(&task)
	if err != nil {
		return fmt.Errorf("failed to get scheduled task: %w", err)
	}
	if task.AllNode {
		// 选择自动加入后 获取所有节点
		// 只取在线节点：NodeTask:<node> 没有 TTL，推给离线节点会留下一条永远没人消费的消息。
		allNode, err := NodeService.GetNodeData(context.Background(), true)
		if err != nil {
			return err
		}
		var nameMap = make(map[string]struct{})
		for _, n := range task.Node {
			nameMap[n] = struct{}{}
		}
		for _, n := range allNode {
			if _, exists := nameMap[n.Name]; !exists {
				nameMap[n.Name] = struct{}{}
				task.Node = append(task.Node, n.Name)
			}
		}
	}
	data, err := getPageMonitoringData()
	if err != nil {
		return err
	}
	if len(data) == 0 {
		// 之前直接静默 return，排查时看不到「有调度但没数据」。
		logger.Warn("page_monitoring: PageMonitoring 集合为空，跳过本次调度")
		return nil
	}
	if len(task.Node) == 0 {
		logger.Warn("page_monitoring: 未取到任何节点，跳过本次调度")
		return nil
	}
	err = CommonService.Repo.Del(context.Background(), "TaskInfo:page_monitoring")
	if err != nil {
		return err
	}

	err = CommonService.Repo.LPush(context.Background(), "TaskInfo:page_monitoring", data)
	if err != nil {
		return err
	}
	addRedisTaskData := map[string]interface{}{
		"ID":   "page_monitoring",
		"type": "page_monitoring",
	}
	jsonData, err := json.Marshal(addRedisTaskData)
	if err != nil {
		return err
	}
	// 注意：队列 key 用「节点名」而不是 URL。旧实现遍历 data（当时是 url 列表），
	// allNode 场景下会把消息推进 NodeTask:https://xxx 这种无人消费的野 key，
	// 真正的节点反而一条都收不到。
	for _, nodeName := range task.Node {
		if nodeName == "" {
			continue
		}
		if err := CommonService.Repo.RPush(context.Background(), fmt.Sprintf("NodeTask:%v", nodeName), jsonData); err != nil {
			logger.Error(fmt.Sprintf("page_monitoring push NodeTask:%v error: %v", nodeName, err))
		}
	}
	return nil
}

// pageMonitPayload 是推给节点的 TaskInfo:page_monitoring 列表元素格式。
//
// 节点侧（ScopeSentry-Scan internal/task/task.go:114 + internal/runner/pagemonitoring.go）
// 会把列表里的每个元素 json.Unmarshal 成 types.PageMonit（url/md5/hash/statusCode/...），
// 再按 md5 回写 PageMonitoring（hash/time/statusCode/rootDomain）并 Upsert PageMonitoringBody。
// 历史实现只推裸 url 字符串：节点 Unmarshal 失败后 warn 一行就 continue，
// 表现为「Task begin/end 同一秒、PageMonitoringBody 永远为空」。
// 另外节点在 hash 非空而 statusCode 为空时会取 StatusCode[len-1] 触发 index -1 panic，
// 所以这里必须把 statusCode 一起带上（缺失时退化成首轮基线，见下）。
type pageMonitPayload struct {
	URL        string   `bson:"url" json:"url"`
	Hash       []string `bson:"hash" json:"hash"`
	MD5        string   `bson:"md5" json:"md5"`
	StatusCode []int    `bson:"statusCode" json:"statusCode"`
	Similarity float64  `bson:"similarity" json:"similarity"`
	State      int      `bson:"state" json:"state"`
	Project    string   `bson:"project" json:"project"`
	Time       string   `bson:"time" json:"time"`
	RootDomain string   `bson:"rootDomain" json:"rootDomain"`
	Tags       []string `bson:"tags" json:"tags"`
}

// pageMonitProjection 只取节点回写需要的字段，避免把大文档整篇读出来。
var pageMonitProjection = bson.M{
	"url":        1,
	"hash":       1,
	"md5":        1,
	"statusCode": 1,
	"similarity": 1,
	"state":      1,
	"project":    1,
	"time":       1,
	"rootDomain": 1,
	"tags":       1,
}

// getPageMonitoringData 获取页面监控数据，返回节点可直接消费的 JSON 列表。
func getPageMonitoringData() ([]string, error) {
	cursor, err := ScanSvc.pageMonitoringCollection.Find(context.Background(), bson.M{}, options.Find().SetProjection(pageMonitProjection))
	if err != nil {
		return nil, fmt.Errorf("failed to find documents: %w", err)
	}
	defer cursor.Close(context.Background())
	var payloads []string
	for cursor.Next(context.Background()) {
		var payload pageMonitPayload
		if err := cursor.Decode(&payload); err != nil {
			return nil, fmt.Errorf("failed to decode document: %w", err)
		}
		if payload.URL == "" {
			continue
		}
		// 节点分支：len(Hash)==0 -> 首轮基线；否则要求 StatusCode 非空。
		// 历史脏数据（有 hash 没 statusCode）会把节点打成 panic，这里退化成基线。
		if len(payload.Hash) > 0 && len(payload.StatusCode) == 0 {
			payload.Hash = []string{}
			payload.Similarity = 0
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal page monitoring payload: %w", err)
		}
		payloads = append(payloads, string(raw))
	}
	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("getPageMonitoringData cursor error: %w", err)
	}
	return payloads, nil
}
