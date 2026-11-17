package github

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/config"
	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Hit 采集到的一条命中（service 层归一后的结构，与存储模型解耦）
type Hit struct {
	Repo     string
	Path     string
	URL      string
	Commit   string
	Title    string
	Snippet  string
	Severity string
	Keyword  string
}

// UpsertSummary 批量写入结果
type UpsertSummary struct {
	// Inserted 新增条数（= stats.newFound 的增量）
	Inserted int64
	// Matched 命中去重键的条数（已存在，仅刷新 snippet/title/updatedAt）
	Matched int64
	// Scanned 本次扫描到的命中总数（含重复）
	Scanned int64
}

// ResultRepository github_result 读写
type ResultRepository interface {
	// UpsertHits 按 (taskId,repo,path,commit) 批量 upsert，统计新增数
	UpsertHits(ctx context.Context, taskID, taskType, project, source string, hits []Hit) (UpsertSummary, error)
	List(ctx context.Context, q models.GithubResultListQuery) ([]models.GithubResult, int64, error)
	Overview(ctx context.Context, q models.GithubResultListQuery) ([]models.GithubResultOverviewRow, int64, error)
	DailyStats(ctx context.Context, q models.GithubResultListQuery, days int) ([]models.GithubDailyStat, error)
	Triage(ctx context.Context, ids []string, status, operator string) (int64, error)
	Iterate(ctx context.Context, q models.GithubResultListQuery, limit int, fn func(*models.GithubResult) error) error
	DeleteByTaskIDs(ctx context.Context, taskIDs []string) (int64, error)
	CountByTask(ctx context.Context, taskID string) (int64, error)
}

type resultRepository struct{}

// NewResultRepository 创建结果仓库
func NewResultRepository() ResultRepository { return &resultRepository{} }

func (r *resultRepository) coll() (*mongo.Collection, error) {
	if mongodb.DB == nil {
		return nil, errors.New("github_result: database not initialized")
	}
	return mongodb.DB.Collection(CollectionResult), nil
}

func (r *resultRepository) UpsertHits(ctx context.Context, taskID, taskType, project, source string, hits []Hit) (UpsertSummary, error) {
	summary := UpsertSummary{Scanned: int64(len(hits))}
	if len(hits) == 0 {
		return summary, nil
	}
	collection, err := r.coll()
	if err != nil {
		return summary, err
	}

	now := time.Now()
	writes := make([]mongo.WriteModel, 0, len(hits))
	for _, hit := range hits {
		filter := bson.M{
			"taskId": taskID,
			"repo":   hit.Repo,
			"path":   hit.Path,
			"commit": hit.Commit,
		}
		update := bson.M{
			// 命中已存在时只刷新内容，不改研判状态与创建时间
			"$set": bson.M{
				"url":       hit.URL,
				"title":     hit.Title,
				"snippet":   hit.Snippet,
				"severity":  hit.Severity,
				"updatedAt": now,
			},
			"$setOnInsert": bson.M{
				"taskId":    taskID,
				"type":      taskType,
				"repo":      hit.Repo,
				"path":      hit.Path,
				"commit":    hit.Commit,
				"status":    models.GithubResultStatusNew,
				"source":    source,
				"project":   project,
				"keyword":   hit.Keyword,
				"createdAt": now,
			},
		}
		model := mongo.NewUpdateOneModel().
			SetFilter(filter).
			SetUpdate(update).
			SetUpsert(true)
		writes = append(writes, model)
	}

	res, err := collection.BulkWrite(ctx, writes, options.BulkWrite().SetOrdered(false))
	if err != nil {
		// 并发 upsert 撞唯一索引（E11000）不算失败：说明另一路已经写入同一条命中
		if isDuplicateKey(err) {
			return summary, nil
		}
		return summary, fmt.Errorf("github_result: bulk upsert: %w", err)
	}
	summary.Inserted = res.UpsertedCount
	summary.Matched = res.MatchedCount
	return summary, nil
}

func isDuplicateKey(err error) bool {
	var we mongo.WriteException
	if errors.As(err, &we) {
		for _, e := range we.WriteErrors {
			if e.Code == 11000 {
				return true
			}
		}
		return false
	}
	var se mongo.BulkWriteException
	if errors.As(err, &se) {
		for _, e := range se.WriteErrors {
			if e.Code == 11000 {
				return true
			}
		}
	}
	return mongo.IsDuplicateKeyError(err)
}

// BuildResultFilter 列表 / 总览 / 导出共用同一套过滤条件，保证三者口径一致
func BuildResultFilter(q models.GithubResultListQuery) bson.M {
	filter := bson.M{}
	if q.TaskID != "" {
		filter["taskId"] = q.TaskID
	}
	if q.Type != "" {
		filter["type"] = q.Type
	}
	if q.Status != "" {
		filter["status"] = q.Status
	}
	if q.Project != "" {
		filter["project"] = q.Project
	}
	if q.Source != "" {
		filter["source"] = q.Source
	}
	if kw := escapeRegex(q.Keyword); kw != "" {
		re := primitive.Regex{Pattern: kw, Options: "i"}
		filter["$or"] = []bson.M{
			{"repo": re},
			{"path": re},
			{"title": re},
			{"snippet": re},
		}
	}
	return filter
}

func (r *resultRepository) List(ctx context.Context, q models.GithubResultListQuery) ([]models.GithubResult, int64, error) {
	collection, err := r.coll()
	if err != nil {
		return nil, 0, err
	}
	filter := BuildResultFilter(q)
	total, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("github_result: count: %w", err)
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}).
		SetSkip(int64((q.PageIndex - 1) * q.PageSize)).
		SetLimit(int64(q.PageSize))
	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("github_result: find: %w", err)
	}
	defer cursor.Close(ctx)
	items := make([]models.GithubResult, 0, q.PageSize)
	if err := cursor.All(ctx, &items); err != nil {
		return nil, 0, fmt.Errorf("github_result: decode: %w", err)
	}
	return items, total, nil
}

func (r *resultRepository) Overview(ctx context.Context, q models.GithubResultListQuery) ([]models.GithubResultOverviewRow, int64, error) {
	collection, err := r.coll()
	if err != nil {
		return nil, 0, err
	}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: BuildResultFilter(q)}},
		{{Key: "$group", Value: bson.M{
			"_id":   bson.M{"type": "$type", "status": "$status"},
			"count": bson.M{"$sum": 1},
		}}},
		{{Key: "$sort", Value: bson.M{"_id.type": 1, "_id.status": 1}}},
	}
	cursor, err := collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, 0, fmt.Errorf("github_result: overview: %w", err)
	}
	defer cursor.Close(ctx)

	rows := make([]models.GithubResultOverviewRow, 0, 8)
	var total int64
	for cursor.Next(ctx) {
		var raw struct {
			ID struct {
				Type   string `bson:"type"`
				Status string `bson:"status"`
			} `bson:"_id"`
			Count int64 `bson:"count"`
		}
		if err := cursor.Decode(&raw); err != nil {
			return nil, 0, fmt.Errorf("github_result: overview decode: %w", err)
		}
		rows = append(rows, models.GithubResultOverviewRow{
			Type:   raw.ID.Type,
			Status: raw.ID.Status,
			Count:  raw.Count,
		})
		total += raw.Count
	}
	if err := cursor.Err(); err != nil {
		return nil, 0, fmt.Errorf("github_result: overview iterate: %w", err)
	}
	return rows, total, nil
}

func (r *resultRepository) DailyStats(ctx context.Context, q models.GithubResultListQuery, days int) ([]models.GithubDailyStat, error) {
	if days <= 0 {
		days = 7
	}
	if days > 365 {
		days = 365
	}
	collection, err := r.coll()
	if err != nil {
		return nil, err
	}
	loc := statsLocation()
	now := time.Now().In(loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(days - 1))

	filter := BuildResultFilter(q)
	filter["createdAt"] = bson.M{"$gte": start}

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: filter}},
		{{Key: "$group", Value: bson.M{
			"_id": bson.M{
				"date":   bson.M{"$dateToString": bson.M{"format": "%Y-%m-%d", "date": "$createdAt", "timezone": loc.String()}},
				"status": "$status",
			},
			"count": bson.M{"$sum": 1},
		}}},
	}
	cursor, err := collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("github_result: stats: %w", err)
	}
	defer cursor.Close(ctx)

	type key struct{ date, status string }
	counts := make(map[key]int64)
	for cursor.Next(ctx) {
		var raw struct {
			ID struct {
				Date   string `bson:"date"`
				Status string `bson:"status"`
			} `bson:"_id"`
			Count int64 `bson:"count"`
		}
		if err := cursor.Decode(&raw); err != nil {
			return nil, fmt.Errorf("github_result: stats decode: %w", err)
		}
		counts[key{raw.ID.Date, raw.ID.Status}] += raw.Count
	}
	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("github_result: stats iterate: %w", err)
	}

	out := make([]models.GithubDailyStat, 0, days)
	for i := 0; i < days; i++ {
		day := start.AddDate(0, 0, i).Format("2006-01-02")
		stat := models.GithubDailyStat{
			Date:    day,
			New:     counts[key{day, models.GithubResultStatusNew}],
			Triaged: counts[key{day, models.GithubResultStatusTriaged}],
			Ignored: counts[key{day, models.GithubResultStatusIgnored}],
		}
		stat.Total = stat.New + stat.Triaged + stat.Ignored
		out = append(out, stat)
	}
	return out, nil
}

// statsLocation 趋势聚合使用的时区。
//
// Mongo 的 $dateToString.timezone 只认 IANA 名（"Asia/Shanghai" 这种），
// time.Local.String() 在 macOS 上是 "Local" 会直接报错，因此这里做归一：
// 先取系统配置时区，再退回 UTC。
func statsLocation() *time.Location {
	name := config.GlobalConfig.System.Timezone
	if name != "" {
		if loc, err := time.LoadLocation(name); err == nil {
			return loc
		}
	}
	if loc := time.Local; loc != nil && loc.String() != "Local" && loc.String() != "" {
		return loc
	}
	return time.UTC
}

func (r *resultRepository) Triage(ctx context.Context, ids []string, status, operator string) (int64, error) {
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
	now := time.Now()
	res, err := collection.UpdateMany(ctx,
		bson.M{"_id": bson.M{"$in": oids}},
		bson.M{"$set": bson.M{
			"status":    status,
			"triagedBy": operator,
			"triagedAt": now,
			"updatedAt": now,
		}},
	)
	if err != nil {
		return 0, fmt.Errorf("github_result: triage: %w", err)
	}
	return res.ModifiedCount, nil
}

func (r *resultRepository) Iterate(ctx context.Context, q models.GithubResultListQuery, limit int, fn func(*models.GithubResult) error) error {
	collection, err := r.coll()
	if err != nil {
		return err
	}
	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}})
	if limit > 0 {
		opts.SetLimit(int64(limit))
	}
	cursor, err := collection.Find(ctx, BuildResultFilter(q), opts)
	if err != nil {
		return fmt.Errorf("github_result: iterate: %w", err)
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var item models.GithubResult
		if err := cursor.Decode(&item); err != nil {
			return fmt.Errorf("github_result: iterate decode: %w", err)
		}
		if err := fn(&item); err != nil {
			return err
		}
	}
	if err := cursor.Err(); err != nil {
		return fmt.Errorf("github_result: iterate err: %w", err)
	}
	return nil
}

func (r *resultRepository) DeleteByTaskIDs(ctx context.Context, taskIDs []string) (int64, error) {
	collection, err := r.coll()
	if err != nil {
		return 0, err
	}
	if len(taskIDs) == 0 {
		return 0, nil
	}
	res, err := collection.DeleteMany(ctx, bson.M{"taskId": bson.M{"$in": taskIDs}})
	if err != nil {
		return 0, fmt.Errorf("github_result: delete by task: %w", err)
	}
	return res.DeletedCount, nil
}

func (r *resultRepository) CountByTask(ctx context.Context, taskID string) (int64, error) {
	collection, err := r.coll()
	if err != nil {
		return 0, err
	}
	count, err := collection.CountDocuments(ctx, bson.M{"taskId": taskID})
	if err != nil {
		return 0, fmt.Errorf("github_result: count by task: %w", err)
	}
	return count, nil
}
