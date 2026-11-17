// repositories-------------------------------------
// @file      : upload_task.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2026/11/18 03:00
// -------------------------------------------

package dictionary

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// UploadTaskRepository 大文件上传任务的持久化。
// 内存里保存运行态（快），Mongo 里保存终态（进程重启后还能查）。
type UploadTaskRepository interface {
	Upsert(ctx context.Context, task *models.DictionaryUploadTask) error
	Get(ctx context.Context, taskID string) (*models.DictionaryUploadTask, error)
	// MarkStaleFailed 把 before 之前创建的 pending/running 任务置为 failed
	MarkStaleFailed(ctx context.Context, reason string, before time.Time) (int64, error)
}

type uploadTaskRepository struct {
	collection *mongo.Collection
}

// NewUploadTaskRepository 创建上传任务仓储
func NewUploadTaskRepository() UploadTaskRepository {
	return &uploadTaskRepository{collection: mongodb.DB.Collection("dict_upload_tasks")}
}

// Upsert 按 task_id 覆盖写。
func (r *uploadTaskRepository) Upsert(ctx context.Context, task *models.DictionaryUploadTask) error {
	_, err := r.collection.UpdateOne(ctx,
		bson.M{"task_id": task.TaskID},
		bson.M{"$set": bson.M{
			"kind":            task.Kind,
			"name":            task.Name,
			"mode":            task.Mode,
			"status":          task.Status,
			"total_lines":     task.TotalLines,
			"processed_lines": task.ProcessedLines,
			"added":           task.Added,
			"duplicated":      task.Duplicated,
			"progress":        task.Progress,
			"error":           task.Error,
			"created_at":      task.CreatedAt,
			"updated_at":      task.UpdatedAt,
			"finished_at":     task.FinishedAt,
		}},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("upsert upload task %s: %w", task.TaskID, err)
	}
	return nil
}

// Get 读取任务；不存在返回 nil。
func (r *uploadTaskRepository) Get(ctx context.Context, taskID string) (*models.DictionaryUploadTask, error) {
	var task models.DictionaryUploadTask
	if err := r.collection.FindOne(ctx, bson.M{"task_id": taskID}).Decode(&task); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, fmt.Errorf("get upload task %s: %w", taskID, err)
	}
	return &task, nil
}

// MarkStaleFailed 进程重启后把本进程启动前残留的 pending/running 置为 failed。
// before 限定只动历史任务，避免误伤本进程刚提交的任务。
func (r *uploadTaskRepository) MarkStaleFailed(ctx context.Context, reason string, before time.Time) (int64, error) {
	res, err := r.collection.UpdateMany(ctx,
		bson.M{
			"status":     bson.M{"$in": []string{models.DictUploadStatusPending, models.DictUploadStatusRunning}},
			"created_at": bson.M{"$lt": before},
		},
		bson.M{"$set": bson.M{
			"status":      models.DictUploadStatusFailed,
			"error":       reason,
			"updated_at":  time.Now(),
			"finished_at": time.Now(),
		}},
	)
	if err != nil {
		return 0, fmt.Errorf("mark stale upload tasks failed: %w", err)
	}
	return res.ModifiedCount, nil
}
