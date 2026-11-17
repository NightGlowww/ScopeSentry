package icp

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

// icp_result 读写。
//
// 去重键是 (domain, company, icpNo) 唯一索引，落库走 BulkWrite + upsert：
//   - 命中已有文档 → 只刷新 siteName/type/source，不搬 taskId、不新增；
//   - 未命中 → 插入，UpsertedCount 就是 stats.found 的增量。
//
// 因此「重复跑同一个任务」found=0，而 /asset 仍能翻到第一次入库的记录。

// ResultRepository icp_result 数据访问
type ResultRepository interface {
	// UpsertRecords 批量 upsert，返回新增（真正插入）的条数
	UpsertRecords(ctx context.Context, taskID, source string, records []models.ICPRecord) (int64, error)
	List(ctx context.Context, q models.IcpAssetQuery) ([]models.IcpResult, int64, error)
	// Iterate 按 createdAt 升序流式遍历任务结果（CSV 导出用）
	Iterate(ctx context.Context, taskID string, fn func(*models.IcpResult) error) error
	// Domains 任务下的去重域名（同步到 asset_scope 用）
	Domains(ctx context.Context, taskID string) ([]string, error)
	// MissingDetails 缺 company 或 icpNo 的结果（backfill_details 用）
	MissingDetails(ctx context.Context, taskID string, limit int) ([]models.IcpResult, error)
	// UpdateDetails 只补空字段（company/icpNo/siteName）
	UpdateDetails(ctx context.Context, id primitive.ObjectID, company, icpNo, siteName string) error
	DeleteByTaskIDs(ctx context.Context, taskIDs []string) (int64, error)
	CountByTask(ctx context.Context, taskID string) (int64, error)
	// Count 全库结果条数（全局统计用）
	Count(ctx context.Context) (int64, error)
	// FindByDomain / FindByCompany 供 manual provider 回读本地记录
	FindByDomain(ctx context.Context, domain string, limit int) ([]models.ICPRecord, error)
	FindByCompany(ctx context.Context, company string, limit int) ([]models.ICPRecord, error)
}

type resultRepository struct{}

// NewResultRepository 创建结果仓库
func NewResultRepository() ResultRepository { return &resultRepository{} }

func (r *resultRepository) coll() (*mongo.Collection, error) {
	if mongodb.DB == nil {
		return nil, errors.New("icp_result: database not initialized")
	}
	return mongodb.DB.Collection(CollectionResult), nil
}

func (r *resultRepository) UpsertRecords(ctx context.Context, taskID, source string, records []models.ICPRecord) (int64, error) {
	if len(records) == 0 {
		return 0, nil
	}
	collection, err := r.coll()
	if err != nil {
		return 0, err
	}
	now := time.Now()
	writes := make([]mongo.WriteModel, 0, len(records))
	for _, rec := range records {
		rec = models.NormalizeIcpRecord(rec)
		if !rec.IsUsable() {
			continue
		}
		writes = append(writes, mongo.NewUpdateOneModel().
			SetFilter(bson.M{"domain": rec.Domain, "company": rec.Company, "icpNo": rec.ICPNo}).
			SetUpdate(bson.M{
				"$set": bson.M{
					"siteName": rec.SiteName,
					"type":     rec.Type,
					"source":   source,
				},
				"$setOnInsert": bson.M{
					"taskId":    taskID,
					"createdAt": now,
				},
			}).
			SetUpsert(true))
	}
	if len(writes) == 0 {
		return 0, nil
	}
	res, err := collection.BulkWrite(ctx, writes, options.BulkWrite().SetOrdered(false))
	var inserted int64
	if res != nil {
		inserted = res.UpsertedCount
	}
	if err != nil && !isDuplicateKeyBulkError(err) {
		return inserted, fmt.Errorf("icp_result: bulk upsert: %w", err)
	}
	return inserted, nil
}

// isDuplicateKeyBulkError 并发任务抢同一个去重键时会出现 11000，
// 这条记录本来就已经在库里（别的任务先写），对调用方等价于「未新增」。
func isDuplicateKeyBulkError(err error) bool {
	var bwe mongo.BulkWriteException
	if errors.As(err, &bwe) {
		for _, we := range bwe.WriteErrors {
			if we.Code != 11000 {
				return false
			}
		}
		return true
	}
	var we mongo.WriteException
	if errors.As(err, &we) {
		for _, item := range we.WriteErrors {
			if item.Code != 11000 {
				return false
			}
		}
		return true
	}
	return mongo.IsDuplicateKeyError(err)
}

func (r *resultRepository) List(ctx context.Context, q models.IcpAssetQuery) ([]models.IcpResult, int64, error) {
	collection, err := r.coll()
	if err != nil {
		return nil, 0, err
	}
	filter := bson.M{"taskId": strings.TrimSpace(q.TaskID)}
	total, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("icp_result: count: %w", err)
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}).
		SetSkip(int64((q.PageIndex - 1) * q.PageSize)).
		SetLimit(int64(q.PageSize))
	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("icp_result: find: %w", err)
	}
	defer cursor.Close(ctx)

	items := make([]models.IcpResult, 0, q.PageSize)
	if err := cursor.All(ctx, &items); err != nil {
		return nil, 0, fmt.Errorf("icp_result: decode: %w", err)
	}
	return items, total, nil
}

func (r *resultRepository) Iterate(ctx context.Context, taskID string, fn func(*models.IcpResult) error) error {
	collection, err := r.coll()
	if err != nil {
		return err
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}}).
		SetBatchSize(500)
	cursor, err := collection.Find(ctx, bson.M{"taskId": strings.TrimSpace(taskID)}, opts)
	if err != nil {
		return fmt.Errorf("icp_result: iterate: %w", err)
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var item models.IcpResult
		if err := cursor.Decode(&item); err != nil {
			return fmt.Errorf("icp_result: iterate decode: %w", err)
		}
		if err := fn(&item); err != nil {
			return err
		}
	}
	return cursor.Err()
}

func (r *resultRepository) Domains(ctx context.Context, taskID string) ([]string, error) {
	collection, err := r.coll()
	if err != nil {
		return nil, err
	}
	pipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: bson.M{
			"taskId": strings.TrimSpace(taskID),
			"domain": bson.M{"$nin": []interface{}{"", nil}},
		}}},
		bson.D{{Key: "$group", Value: bson.M{"_id": "$domain"}}},
		bson.D{{Key: "$sort", Value: bson.M{"_id": 1}}},
	}
	cursor, err := collection.Aggregate(ctx, pipeline, options.Aggregate().SetAllowDiskUse(true))
	if err != nil {
		return nil, fmt.Errorf("icp_result: domains aggregate: %w", err)
	}
	defer cursor.Close(ctx)
	var rows []struct {
		Domain string `bson:"_id"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("icp_result: domains decode: %w", err)
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		domain := strings.TrimSpace(row.Domain)
		if domain == "" {
			continue
		}
		out = append(out, domain)
	}
	return out, nil
}

func (r *resultRepository) MissingDetails(ctx context.Context, taskID string, limit int) ([]models.IcpResult, error) {
	collection, err := r.coll()
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 500
	}
	filter := bson.M{
		"taskId": strings.TrimSpace(taskID),
		"$or": []bson.M{
			{"company": ""},
			{"company": bson.M{"$exists": false}},
			{"icpNo": ""},
			{"icpNo": bson.M{"$exists": false}},
		},
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: 1}}).
		SetLimit(int64(limit))
	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("icp_result: missing details: %w", err)
	}
	defer cursor.Close(ctx)
	var items []models.IcpResult
	if err := cursor.All(ctx, &items); err != nil {
		return nil, fmt.Errorf("icp_result: missing details decode: %w", err)
	}
	return items, nil
}

func (r *resultRepository) UpdateDetails(ctx context.Context, id primitive.ObjectID, company, icpNo, siteName string) error {
	collection, err := r.coll()
	if err != nil {
		return err
	}
	set := bson.M{}
	if company != "" {
		set["company"] = company
	}
	if icpNo != "" {
		set["icpNo"] = icpNo
	}
	if siteName != "" {
		set["siteName"] = siteName
	}
	if len(set) == 0 {
		return nil
	}
	if _, err := collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": set}); err != nil {
		return fmt.Errorf("icp_result: update details: %w", err)
	}
	return nil
}

func (r *resultRepository) DeleteByTaskIDs(ctx context.Context, taskIDs []string) (int64, error) {
	collection, err := r.coll()
	if err != nil {
		return 0, err
	}
	ids := make([]string, 0, len(taskIDs))
	for _, id := range taskIDs {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			ids = append(ids, trimmed)
		}
	}
	if len(ids) == 0 {
		return 0, nil
	}
	res, err := collection.DeleteMany(ctx, bson.M{"taskId": bson.M{"$in": ids}})
	if err != nil {
		return 0, fmt.Errorf("icp_result: delete by task: %w", err)
	}
	return res.DeletedCount, nil
}

func (r *resultRepository) CountByTask(ctx context.Context, taskID string) (int64, error) {
	collection, err := r.coll()
	if err != nil {
		return 0, err
	}
	total, err := collection.CountDocuments(ctx, bson.M{"taskId": strings.TrimSpace(taskID)})
	if err != nil {
		return 0, fmt.Errorf("icp_result: count by task: %w", err)
	}
	return total, nil
}

func (r *resultRepository) Count(ctx context.Context) (int64, error) {
	collection, err := r.coll()
	if err != nil {
		return 0, err
	}
	total, err := collection.CountDocuments(ctx, bson.M{})
	if err != nil {
		return 0, fmt.Errorf("icp_result: count all: %w", err)
	}
	return total, nil
}

func (r *resultRepository) FindByDomain(ctx context.Context, domain string, limit int) ([]models.ICPRecord, error) {
	return r.findRecords(ctx, bson.M{"domain": strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))}, limit)
}

func (r *resultRepository) FindByCompany(ctx context.Context, company string, limit int) ([]models.ICPRecord, error) {
	return r.findRecords(ctx, bson.M{"company": strings.TrimSpace(company)}, limit)
}

func (r *resultRepository) findRecords(ctx context.Context, filter bson.M, limit int) ([]models.ICPRecord, error) {
	collection, err := r.coll()
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 500
	}
	opts := options.Find().
		SetProjection(bson.M{"_id": 0, "domain": 1, "company": 1, "icpNo": 1, "siteName": 1, "type": 1}).
		SetSort(bson.D{{Key: "createdAt", Value: -1}}).
		SetLimit(int64(limit))
	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("icp_result: find records: %w", err)
	}
	defer cursor.Close(ctx)
	var records []models.ICPRecord
	if err := cursor.All(ctx, &records); err != nil {
		return nil, fmt.Errorf("icp_result: find records decode: %w", err)
	}
	return records, nil
}
