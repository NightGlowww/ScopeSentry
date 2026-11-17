package icp

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// icp_task 读写。
//
// 状态机由这里兜底：
//   - Claim 用 FindOneAndUpdate(state != 1) 抢占运行权，同一个 task 不会并发；
//   - FinishRun 只在 state=1 时写终态，所以 stop 抢先把状态改成 2 后，
//     跑完的 goroutine 不会把「已暂停」覆盖成「已完成」，只回写统计；
//   - UpdateProgress 每次查询后落 cursor + stats，resume 从 cursor 继续。

// ErrInvalidID 非法 ObjectID
var ErrInvalidID = errors.New("invalid id")

// ErrTaskNotFound 任务不存在
var ErrTaskNotFound = errors.New("icp task not found")

// ErrTaskBusy 任务正在运行（互斥失败）
var ErrTaskBusy = errors.New("icp task is running")

// TaskRepository icp_task 数据访问
type TaskRepository interface {
	Create(ctx context.Context, task *models.IcpTask) error
	Get(ctx context.Context, id string) (*models.IcpTask, error)
	List(ctx context.Context, q models.IcpTaskListQuery) ([]models.IcpTask, int64, error)
	// Claim 抢占运行权：state != 1 才能抢到；reset=true 时清零 stats/cursor（restart），
	// false 时保留进度（resume）。返回值是抢占前的任务快照。
	Claim(ctx context.Context, id string, reset bool) (*models.IcpTask, error)
	// UpdateProgress 落运行进度（cursor 指向下一个待处理查询）
	UpdateProgress(ctx context.Context, id string, cursor int, stats models.IcpTaskStats) error
	// FinishRun 写终态：仅当仍处于运行中时才改 state；被 stop 抢先时不覆盖状态，只回写统计
	FinishRun(ctx context.Context, id string, state int, stats models.IcpTaskStats, lastError string) error
	// SetState 批量改状态，from 为空表示不限制来源状态
	SetState(ctx context.Context, ids []string, state int, from []int) (int64, error)
	Delete(ctx context.Context, ids []string) (int64, error)
	// OverviewCounts 各状态任务数（聚合，allowDiskUse）
	OverviewCounts(ctx context.Context) (map[string]int64, error)
	// SumStats 全部任务统计求和（聚合，allowDiskUse）
	SumStats(ctx context.Context) (models.IcpTaskStats, error)
	Count(ctx context.Context) (int64, error)
}

type taskRepository struct{}

// NewTaskRepository 创建任务仓库（集合按调用期解析，DB 未初始化时报错而不是 panic）
func NewTaskRepository() TaskRepository { return &taskRepository{} }

func (r *taskRepository) coll() (*mongo.Collection, error) {
	if mongodb.DB == nil {
		return nil, errors.New("icp_task: database not initialized")
	}
	return mongodb.DB.Collection(CollectionTask), nil
}

func parseObjectID(id string) (primitive.ObjectID, error) {
	oid, err := primitive.ObjectIDFromHex(strings.TrimSpace(id))
	if err != nil {
		return primitive.NilObjectID, ErrInvalidID
	}
	return oid, nil
}

func (r *taskRepository) Create(ctx context.Context, task *models.IcpTask) error {
	collection, err := r.coll()
	if err != nil {
		return err
	}
	now := time.Now()
	if task.CreatedAt.IsZero() {
		task.CreatedAt = now
	}
	task.UpdatedAt = now
	res, err := collection.InsertOne(ctx, task)
	if err != nil {
		return fmt.Errorf("icp_task: insert: %w", err)
	}
	if oid, ok := res.InsertedID.(primitive.ObjectID); ok {
		task.ID = oid
	}
	return nil
}

func (r *taskRepository) Get(ctx context.Context, id string) (*models.IcpTask, error) {
	oid, err := parseObjectID(id)
	if err != nil {
		return nil, err
	}
	collection, err := r.coll()
	if err != nil {
		return nil, err
	}
	var task models.IcpTask
	if err := collection.FindOne(ctx, bson.M{"_id": oid}).Decode(&task); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrTaskNotFound
		}
		return nil, fmt.Errorf("icp_task: get: %w", err)
	}
	return &task, nil
}

func buildTaskFilter(q models.IcpTaskListQuery) bson.M {
	filter := bson.M{}
	if q.State != nil {
		filter["state"] = *q.State
	}
	if q.Type != "" {
		filter["type"] = q.Type
	}
	if q.Provider != "" {
		filter["provider"] = q.Provider
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		re := primitive.Regex{Pattern: regexp.QuoteMeta(kw), Options: "i"}
		filter["$or"] = []bson.M{{"name": re}, {"queries": re}}
	}
	return filter
}

func (r *taskRepository) List(ctx context.Context, q models.IcpTaskListQuery) ([]models.IcpTask, int64, error) {
	collection, err := r.coll()
	if err != nil {
		return nil, 0, err
	}
	filter := buildTaskFilter(q)
	total, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("icp_task: count: %w", err)
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: -1}}).
		SetSkip(int64((q.PageIndex - 1) * q.PageSize)).
		SetLimit(int64(q.PageSize))
	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("icp_task: find: %w", err)
	}
	defer cursor.Close(ctx)

	tasks := make([]models.IcpTask, 0, q.PageSize)
	if err := cursor.All(ctx, &tasks); err != nil {
		return nil, 0, fmt.Errorf("icp_task: decode: %w", err)
	}
	return tasks, total, nil
}

func (r *taskRepository) Claim(ctx context.Context, id string, reset bool) (*models.IcpTask, error) {
	oid, err := parseObjectID(id)
	if err != nil {
		return nil, err
	}
	collection, err := r.coll()
	if err != nil {
		return nil, err
	}
	set := bson.M{
		"state":     models.IcpTaskStateRunning,
		"updatedAt": time.Now(),
	}
	if reset {
		set["stats"] = models.IcpTaskStats{}
		set["cursor"] = 0
		set["lastError"] = ""
	}
	var before models.IcpTask
	err = collection.FindOneAndUpdate(ctx,
		bson.M{"_id": oid, "state": bson.M{"$ne": models.IcpTaskStateRunning}},
		bson.M{"$set": set},
		options.FindOneAndUpdate().SetReturnDocument(options.Before),
	).Decode(&before)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			// 不存在 or 正在运行：再查一次区分（ErrTaskBusy 与 ErrTaskNotFound 对前端语义不同）
			if _, getErr := r.Get(ctx, id); getErr != nil {
				return nil, getErr
			}
			return nil, ErrTaskBusy
		}
		return nil, fmt.Errorf("icp_task: claim: %w", err)
	}
	return &before, nil
}

func (r *taskRepository) UpdateProgress(ctx context.Context, id string, cursor int, stats models.IcpTaskStats) error {
	oid, err := parseObjectID(id)
	if err != nil {
		return err
	}
	collection, err := r.coll()
	if err != nil {
		return err
	}
	_, err = collection.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": bson.M{
		"cursor":    cursor,
		"stats":     stats,
		"updatedAt": time.Now(),
	}})
	if err != nil {
		return fmt.Errorf("icp_task: update progress: %w", err)
	}
	return nil
}

func (r *taskRepository) FinishRun(ctx context.Context, id string, state int, stats models.IcpTaskStats, lastError string) error {
	oid, err := parseObjectID(id)
	if err != nil {
		return err
	}
	collection, err := r.coll()
	if err != nil {
		return err
	}
	now := time.Now()
	res, err := collection.UpdateOne(ctx,
		bson.M{"_id": oid, "state": models.IcpTaskStateRunning},
		bson.M{"$set": bson.M{
			"state":     state,
			"stats":     stats,
			"lastError": lastError,
			"updatedAt": now,
		}},
	)
	if err != nil {
		return fmt.Errorf("icp_task: finish: %w", err)
	}
	if res.MatchedCount > 0 {
		return nil
	}
	// 运行期被 stop 抢先（state 已改成 2）：只回写统计，不改状态
	if _, err := collection.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": bson.M{
		"stats":     stats,
		"updatedAt": now,
	}}); err != nil {
		return fmt.Errorf("icp_task: finish (preserve state): %w", err)
	}
	return nil
}

func (r *taskRepository) SetState(ctx context.Context, ids []string, state int, from []int) (int64, error) {
	collection, err := r.coll()
	if err != nil {
		return 0, err
	}
	oids := make([]primitive.ObjectID, 0, len(ids))
	for _, id := range ids {
		oid, err := parseObjectID(id)
		if err != nil {
			return 0, err
		}
		oids = append(oids, oid)
	}
	if len(oids) == 0 {
		return 0, nil
	}
	filter := bson.M{"_id": bson.M{"$in": oids}}
	if len(from) > 0 {
		filter["state"] = bson.M{"$in": from}
	}
	res, err := collection.UpdateMany(ctx, filter, bson.M{"$set": bson.M{
		"state":     state,
		"updatedAt": time.Now(),
	}})
	if err != nil {
		return 0, fmt.Errorf("icp_task: set state: %w", err)
	}
	return res.ModifiedCount, nil
}

func (r *taskRepository) Delete(ctx context.Context, ids []string) (int64, error) {
	collection, err := r.coll()
	if err != nil {
		return 0, err
	}
	oids := make([]primitive.ObjectID, 0, len(ids))
	for _, id := range ids {
		oid, err := parseObjectID(id)
		if err != nil {
			return 0, err
		}
		oids = append(oids, oid)
	}
	if len(oids) == 0 {
		return 0, nil
	}
	res, err := collection.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": oids}})
	if err != nil {
		return 0, fmt.Errorf("icp_task: delete: %w", err)
	}
	return res.DeletedCount, nil
}

func (r *taskRepository) Count(ctx context.Context) (int64, error) {
	collection, err := r.coll()
	if err != nil {
		return 0, err
	}
	total, err := collection.CountDocuments(ctx, bson.M{})
	if err != nil {
		return 0, fmt.Errorf("icp_task: count all: %w", err)
	}
	return total, nil
}

func (r *taskRepository) OverviewCounts(ctx context.Context) (map[string]int64, error) {
	collection, err := r.coll()
	if err != nil {
		return nil, err
	}
	pipeline := mongo.Pipeline{
		bson.D{{Key: "$group", Value: bson.M{
			"_id":   "$state",
			"count": bson.M{"$sum": 1},
		}}},
	}
	cursor, err := collection.Aggregate(ctx, pipeline, options.Aggregate().SetAllowDiskUse(true))
	if err != nil {
		return nil, fmt.Errorf("icp_task: overview aggregate: %w", err)
	}
	defer cursor.Close(ctx)

	var rows []struct {
		ID    int   `bson:"_id"`
		Count int64 `bson:"count"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("icp_task: overview decode: %w", err)
	}
	out := map[string]int64{
		"waiting": 0,
		"running": 0,
		"paused":  0,
		"done":    0,
		"total":   0,
	}
	for _, row := range rows {
		out[models.IcpStateName(row.ID)] = row.Count
		out["total"] += row.Count
	}
	return out, nil
}

func (r *taskRepository) SumStats(ctx context.Context) (models.IcpTaskStats, error) {
	collection, err := r.coll()
	if err != nil {
		return models.IcpTaskStats{}, err
	}
	pipeline := mongo.Pipeline{
		bson.D{{Key: "$group", Value: bson.M{
			"_id":    nil,
			"total":  bson.M{"$sum": "$stats.total"},
			"done":   bson.M{"$sum": "$stats.done"},
			"found":  bson.M{"$sum": "$stats.found"},
			"failed": bson.M{"$sum": "$stats.failed"},
		}}},
	}
	cursor, err := collection.Aggregate(ctx, pipeline, options.Aggregate().SetAllowDiskUse(true))
	if err != nil {
		return models.IcpTaskStats{}, fmt.Errorf("icp_task: stats aggregate: %w", err)
	}
	defer cursor.Close(ctx)

	var rows []struct {
		Total  int64 `bson:"total"`
		Done   int64 `bson:"done"`
		Found  int64 `bson:"found"`
		Failed int64 `bson:"failed"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return models.IcpTaskStats{}, fmt.Errorf("icp_task: stats decode: %w", err)
	}
	if len(rows) == 0 {
		return models.IcpTaskStats{}, nil
	}
	return models.IcpTaskStats{
		Total:  rows[0].Total,
		Done:   rows[0].Done,
		Found:  rows[0].Found,
		Failed: rows[0].Failed,
	}, nil
}
