package github

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrTaskNotFound 任务不存在
var ErrTaskNotFound = errors.New("github task not found")

// ErrTaskBusy 任务正在运行（互斥失败）
var ErrTaskBusy = errors.New("github task is running")

// TaskRepository github_task 读写
type TaskRepository interface {
	Create(ctx context.Context, task *models.GithubTask) error
	Get(ctx context.Context, id string) (*models.GithubTask, error)
	List(ctx context.Context, q models.GithubTaskListQuery) ([]models.GithubTask, int64, error)
	// Claim 抢占运行权：state != 1 才能抢到，成功返回抢占前的任务快照
	Claim(ctx context.Context, id string) (*models.GithubTask, error)
	// Finish 结束一次运行：写回状态、统计（累加）、lastRunAt / nextRunAt
	Finish(ctx context.Context, id string, state int, stats models.GithubTaskStats,
		lastRunAt *time.Time, nextRunAt *time.Time, lastError string) error
	// SetState 批量改状态。返回 matched（命中了几条真实记录）与 modified（真正被改动的条数）：
	// 只有 matched=0 才说明 id 根本不存在，modified=0 可能只是状态本来就一致（幂等）。
	SetState(ctx context.Context, ids []string, state int) (matched, modified int64, err error)
	UpdateSchedule(ctx context.Context, id string, patch SchedulePatch) error
	Delete(ctx context.Context, ids []string) (int64, error)
	// DueScheduled 取到点该跑的周期任务（intervalMinutes > 0 且未被暂停）
	DueScheduled(ctx context.Context, now time.Time, limit int) ([]models.GithubTask, error)
}

// SchedulePatch 周期任务的可编辑字段（指针区分「没传」与「传空」）
type SchedulePatch struct {
	Name            *string
	Query           *string
	Keywords        []string
	KeywordsSet     bool
	IntervalMinutes *int
	MaxPages        *int
	TokenRef        *string
}

type taskRepository struct{}

// NewTaskRepository 创建任务仓库（集合按调用期解析，DB 未初始化时报错而不是 panic）
func NewTaskRepository() TaskRepository { return &taskRepository{} }

func (r *taskRepository) coll() (*mongo.Collection, error) {
	if mongodb.DB == nil {
		return nil, errors.New("github_task: database not initialized")
	}
	return mongodb.DB.Collection(CollectionTask), nil
}

// ErrInvalidID 非法 ObjectID
var ErrInvalidID = errors.New("invalid id")

func parseObjectID(id string) (primitive.ObjectID, error) {
	oid, err := primitive.ObjectIDFromHex(strings.TrimSpace(id))
	if err != nil {
		return primitive.NilObjectID, ErrInvalidID
	}
	return oid, nil
}

func (r *taskRepository) Create(ctx context.Context, task *models.GithubTask) error {
	collection, err := r.coll()
	if err != nil {
		return err
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now()
	}
	res, err := collection.InsertOne(ctx, task)
	if err != nil {
		return fmt.Errorf("github_task: insert: %w", err)
	}
	if oid, ok := res.InsertedID.(primitive.ObjectID); ok {
		task.ID = oid
	}
	return nil
}

func (r *taskRepository) Get(ctx context.Context, id string) (*models.GithubTask, error) {
	oid, err := parseObjectID(id)
	if err != nil {
		return nil, err
	}
	collection, err := r.coll()
	if err != nil {
		return nil, err
	}
	var task models.GithubTask
	if err := collection.FindOne(ctx, bson.M{"_id": oid}).Decode(&task); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrTaskNotFound
		}
		return nil, fmt.Errorf("github_task: get: %w", err)
	}
	return &task, nil
}

func buildTaskFilter(q models.GithubTaskListQuery) bson.M {
	filter := bson.M{}
	if q.State != nil {
		filter["state"] = *q.State
	}
	if q.Type != "" {
		filter["type"] = q.Type
	}
	if q.ScheduledOnly {
		filter["config.intervalMinutes"] = bson.M{"$gt": 0}
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		re := primitive.Regex{Pattern: escapeRegex(kw), Options: "i"}
		filter["$or"] = []bson.M{{"name": re}, {"query": re}, {"keywords": re}}
	}
	return filter
}

func (r *taskRepository) List(ctx context.Context, q models.GithubTaskListQuery) ([]models.GithubTask, int64, error) {
	collection, err := r.coll()
	if err != nil {
		return nil, 0, err
	}
	filter := buildTaskFilter(q)
	total, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("github_task: count: %w", err)
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: -1}}).
		SetSkip(int64((q.PageIndex - 1) * q.PageSize)).
		SetLimit(int64(q.PageSize))
	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("github_task: find: %w", err)
	}
	defer cursor.Close(ctx)

	tasks := make([]models.GithubTask, 0, q.PageSize)
	if err := cursor.All(ctx, &tasks); err != nil {
		return nil, 0, fmt.Errorf("github_task: decode: %w", err)
	}
	return tasks, total, nil
}

func (r *taskRepository) Claim(ctx context.Context, id string) (*models.GithubTask, error) {
	oid, err := parseObjectID(id)
	if err != nil {
		return nil, err
	}
	collection, err := r.coll()
	if err != nil {
		return nil, err
	}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.Before)
	var before models.GithubTask
	err = collection.FindOneAndUpdate(ctx,
		bson.M{"_id": oid, "state": bson.M{"$ne": models.GithubTaskStateRunning}},
		bson.M{"$set": bson.M{"state": models.GithubTaskStateRunning}},
		opts,
	).Decode(&before)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			// 不存在 or 正在运行，再查一次区分
			if _, getErr := r.Get(ctx, id); getErr != nil {
				return nil, getErr
			}
			return nil, ErrTaskBusy
		}
		return nil, fmt.Errorf("github_task: claim: %w", err)
	}
	return &before, nil
}

func (r *taskRepository) Finish(ctx context.Context, id string, state int, stats models.GithubTaskStats,
	lastRunAt *time.Time, nextRunAt *time.Time, lastError string) error {
	oid, err := parseObjectID(id)
	if err != nil {
		return err
	}
	collection, err := r.coll()
	if err != nil {
		return err
	}
	set := bson.M{
		"state":     state,
		"stats":     bson.M{"scanned": stats.Scanned, "newFound": stats.NewFound},
		"lastError": lastError,
	}
	if lastRunAt != nil {
		set["lastRunAt"] = *lastRunAt
	}
	if nextRunAt != nil {
		set["nextRunAt"] = *nextRunAt
	} else {
		set["nextRunAt"] = nil
	}
	_, err = collection.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": set})
	if err != nil {
		return fmt.Errorf("github_task: finish: %w", err)
	}
	return nil
}

func (r *taskRepository) SetState(ctx context.Context, ids []string, state int) (matched, modified int64, err error) {
	collection, err := r.coll()
	if err != nil {
		return 0, 0, err
	}
	oids := make([]primitive.ObjectID, 0, len(ids))
	for _, id := range ids {
		oid, err := parseObjectID(id)
		if err != nil {
			return 0, 0, err
		}
		oids = append(oids, oid)
	}
	if len(oids) == 0 {
		return 0, 0, nil
	}
	res, err := collection.UpdateMany(ctx,
		bson.M{"_id": bson.M{"$in": oids}},
		bson.M{"$set": bson.M{"state": state}},
	)
	if err != nil {
		return 0, 0, fmt.Errorf("github_task: set state: %w", err)
	}
	return res.MatchedCount, res.ModifiedCount, nil
}

func (r *taskRepository) UpdateSchedule(ctx context.Context, id string, patch SchedulePatch) error {
	oid, err := parseObjectID(id)
	if err != nil {
		return err
	}
	collection, err := r.coll()
	if err != nil {
		return err
	}
	set := bson.M{}
	if patch.Name != nil {
		set["name"] = *patch.Name
	}
	if patch.Query != nil {
		set["query"] = *patch.Query
	}
	if patch.KeywordsSet {
		set["keywords"] = patch.Keywords
	}
	if patch.IntervalMinutes != nil {
		set["config.intervalMinutes"] = *patch.IntervalMinutes
	}
	if patch.MaxPages != nil {
		set["config.maxPages"] = *patch.MaxPages
	}
	if patch.TokenRef != nil {
		set["config.tokenRef"] = *patch.TokenRef
	}
	if len(set) == 0 {
		return nil
	}
	res, err := collection.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": set})
	if err != nil {
		return fmt.Errorf("github_task: update schedule: %w", err)
	}
	if res.MatchedCount == 0 {
		return ErrTaskNotFound
	}
	return nil
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
		return 0, fmt.Errorf("github_task: delete: %w", err)
	}
	return res.DeletedCount, nil
}

func (r *taskRepository) DueScheduled(ctx context.Context, now time.Time, limit int) ([]models.GithubTask, error) {
	collection, err := r.coll()
	if err != nil {
		return nil, err
	}
	filter := bson.M{
		"config.intervalMinutes": bson.M{"$gt": 0},
		"state":                  bson.M{"$ne": models.GithubTaskStatePaused},
		"$or": []bson.M{
			{"nextRunAt": bson.M{"$exists": false}},
			{"nextRunAt": nil},
			{"nextRunAt": bson.M{"$lte": now}},
		},
	}
	opts := options.Find().SetSort(bson.D{{Key: "nextRunAt", Value: 1}}).SetLimit(int64(limit))
	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("github_task: due: %w", err)
	}
	defer cursor.Close(ctx)
	tasks := make([]models.GithubTask, 0, limit)
	if err := cursor.All(ctx, &tasks); err != nil {
		return nil, fmt.Errorf("github_task: due decode: %w", err)
	}
	return tasks, nil
}
