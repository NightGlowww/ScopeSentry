package github

import (
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// 集合名
const (
	CollectionTask    = "github_task"
	CollectionResult  = "github_result"
	CollectionThreat  = "github_threat_config"
	CollectionHistory = "github_threat_history"
)

// EnsureIndexes 建立 GitHub 情报面所需的索引（幂等，可反复调用）。
//
//   - result(taskId,createdAt) / (type,status) / (repo)：列表默认按任务+时间倒序、总览按类型状态聚合、按仓库聚合
//   - result(taskId,repo,path,commit) 唯一：去重键，重跑同一条命中不会新增文档
//   - task(state,createdAt)：调度器扫待跑任务、列表默认排序
//   - threat_config：单例文档（_id=global），无需额外索引
//   - threat_history(kind,key) 唯一 + (kind,discoveredAt)：情报发现去重与倒序查询
func EnsureIndexes(ctx context.Context, db *mongo.Database) error {
	if db == nil {
		return errors.New("github: database not initialized")
	}

	result := db.Collection(CollectionResult)
	if _, err := result.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "taskId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("github_result_task_created"),
		},
		{
			Keys:    bson.D{{Key: "type", Value: 1}, {Key: "status", Value: 1}},
			Options: options.Index().SetName("github_result_type_status"),
		},
		{
			Keys:    bson.D{{Key: "repo", Value: 1}},
			Options: options.Index().SetName("github_result_repo"),
		},
		{
			// 去重键：同 task 下同一个 repo/path/commit 只留一条
			Keys: bson.D{
				{Key: "taskId", Value: 1},
				{Key: "repo", Value: 1},
				{Key: "path", Value: 1},
				{Key: "commit", Value: 1},
			},
			Options: options.Index().SetName("github_result_dedup").SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "source", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("github_result_source_created"),
		},
	}); err != nil {
		return fmt.Errorf("github: create %s indexes: %w", CollectionResult, err)
	}

	task := db.Collection(CollectionTask)
	if _, err := task.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "state", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("github_task_state_created"),
		},
		{
			Keys:    bson.D{{Key: "nextRunAt", Value: 1}},
			Options: options.Index().SetName("github_task_next_run").SetSparse(true),
		},
	}); err != nil {
		return fmt.Errorf("github: create %s indexes: %w", CollectionTask, err)
	}

	history := db.Collection(CollectionHistory)
	if _, err := history.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "kind", Value: 1}, {Key: "key", Value: 1}},
			Options: options.Index().SetName("github_threat_history_key").SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "kind", Value: 1}, {Key: "discoveredAt", Value: -1}},
			Options: options.Index().SetName("github_threat_history_time"),
		},
	}); err != nil {
		return fmt.Errorf("github: create %s indexes: %w", CollectionHistory, err)
	}

	return nil
}
