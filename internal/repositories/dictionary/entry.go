// repositories-------------------------------------
// @file      : entry.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2026/11/18 02:20
// -------------------------------------------

package dictionary

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	// entryBatchSize 单批写入行数：2000 行/批，兼顾 16MB 文档上限与往返次数
	entryBatchSize = 2000
	// entryDeleteChunk 删除时 $in 的分片大小
	entryDeleteChunk = 1000
	// dupKeyCode Mongo 唯一键冲突错误码
	dupKeyCode = 11000
)

// EntryRepository 字典行级存储。
// 唯一索引 (dict_id, value) 让「追加去重」变成一次 InsertMany + 计数，
// 不用把整份字典读进内存；排序索引 (dict_id, seq) 支撑预览/导出。
type EntryRepository interface {
	InsertBatch(ctx context.Context, dictID, category string, seqStart int64, values []string) (inserted int64, duplicated int64, err error)
	DeleteValues(ctx context.Context, dictID string, values []string) (deleted int64, err error)
	DeleteAll(ctx context.Context, dictID string) error
	Count(ctx context.Context, dictID string) (int64, error)
	Preview(ctx context.Context, dictID string, limit int) ([]models.DictionaryEntry, error)
	Search(ctx context.Context, dictID, keyword string, limit int) ([]models.DictionaryEntry, error)
	Stream(ctx context.Context, dictID string) (*mongo.Cursor, error)
}

type entryRepository struct {
	collection *mongo.Collection
}

// NewEntryRepository 创建行级字典仓储
func NewEntryRepository() EntryRepository {
	return &entryRepository{
		collection: mongodb.DB.Collection("dictionary_entry"),
	}
}

// InsertBatch 批量写入，重复行由唯一索引挡掉，计数返回。
func (r *entryRepository) InsertBatch(ctx context.Context, dictID, category string, seqStart int64, values []string) (int64, int64, error) {
	if len(values) == 0 {
		return 0, 0, nil
	}
	now := time.Now()
	docs := make([]interface{}, 0, len(values))
	for i, v := range values {
		docs = append(docs, bson.M{
			"dict_id":    dictID,
			"category":   category,
			"value":      v,
			"seq":        seqStart + int64(i),
			"created_at": now,
		})
	}

	res, err := r.collection.InsertMany(ctx, docs, options.InsertMany().SetOrdered(false))
	var inserted int64
	if res != nil {
		// driver v1.14 的 InsertManyResult 只有 InsertedIDs：长度即成功写入行数
		inserted = int64(len(res.InsertedIDs))
	}
	if err != nil {
		if !isDuplicateOnly(err) {
			return inserted, 0, fmt.Errorf("insert dictionary entries: %w", err)
		}
	}
	duplicated := int64(len(values)) - inserted
	if duplicated < 0 {
		duplicated = 0
	}
	return inserted, duplicated, nil
}

// isDuplicateOnly 判断批量写错误是否"只由唯一键冲突构成"。
// Mongo 对 ordered=false 的部分失败返回 BulkWriteException，重复键属正常情况。
func isDuplicateOnly(err error) bool {
	var bwe mongo.BulkWriteException
	if !errors.As(err, &bwe) {
		return false
	}
	if len(bwe.WriteErrors) == 0 {
		return false
	}
	for _, we := range bwe.WriteErrors {
		if we.Code != dupKeyCode {
			return false
		}
	}
	return true
}

// DeleteValues 批量删除指定行（按 dict_id + value 命中唯一索引）。
func (r *entryRepository) DeleteValues(ctx context.Context, dictID string, values []string) (int64, error) {
	var deleted int64
	for start := 0; start < len(values); start += entryDeleteChunk {
		end := start + entryDeleteChunk
		if end > len(values) {
			end = len(values)
		}
		res, err := r.collection.DeleteMany(ctx, bson.M{
			"dict_id": dictID,
			"value":   bson.M{"$in": values[start:end]},
		})
		if err != nil {
			return deleted, fmt.Errorf("delete dictionary entries: %w", err)
		}
		deleted += res.DeletedCount
	}
	return deleted, nil
}

// DeleteAll 清空一个字典的所有行（整份替换/删除字典时用）。
func (r *entryRepository) DeleteAll(ctx context.Context, dictID string) error {
	if _, err := r.collection.DeleteMany(ctx, bson.M{"dict_id": dictID}); err != nil {
		return fmt.Errorf("clear dictionary entries: %w", err)
	}
	return nil
}

// Count 字典行数（走 (dict_id, ...) 索引）。
func (r *entryRepository) Count(ctx context.Context, dictID string) (int64, error) {
	n, err := r.collection.CountDocuments(ctx, bson.M{"dict_id": dictID})
	if err != nil {
		return 0, fmt.Errorf("count dictionary entries: %w", err)
	}
	return n, nil
}

// Preview 按写入顺序取前 limit 行。
func (r *entryRepository) Preview(ctx context.Context, dictID string, limit int) ([]models.DictionaryEntry, error) {
	opts := options.Find().
		SetSort(bson.D{{Key: "seq", Value: 1}}).
		SetLimit(int64(limit))
	cursor, err := r.collection.Find(ctx, bson.M{"dict_id": dictID}, opts)
	if err != nil {
		return nil, fmt.Errorf("preview dictionary entries: %w", err)
	}
	defer cursor.Close(ctx)

	var rows []models.DictionaryEntry
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("decode dictionary entries: %w", err)
	}
	return rows, nil
}

// Search 行内容匹配（不区分大小写子串）。
// 查询天然带 dict_id 前缀，走 (dict_id, value) 索引收敛范围；不做内存过滤。
func (r *entryRepository) Search(ctx context.Context, dictID, keyword string, limit int) ([]models.DictionaryEntry, error) {
	pattern := bson.M{"$regex": regexp.QuoteMeta(keyword), "$options": "i"}
	opts := options.Find().
		SetSort(bson.D{{Key: "seq", Value: 1}}).
		SetLimit(int64(limit))
	cursor, err := r.collection.Find(ctx, bson.M{"dict_id": dictID, "value": pattern}, opts)
	if err != nil {
		return nil, fmt.Errorf("search dictionary entries: %w", err)
	}
	defer cursor.Close(ctx)

	var rows []models.DictionaryEntry
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("decode dictionary entries: %w", err)
	}
	return rows, nil
}

// Stream 按写入顺序流式拉取全部行，用于重建 GridFS 内容（O(1) 内存）。
func (r *entryRepository) Stream(ctx context.Context, dictID string) (*mongo.Cursor, error) {
	opts := options.Find().
		SetSort(bson.D{{Key: "seq", Value: 1}}).
		SetProjection(bson.M{"value": 1, "seq": 1}).
		SetBatchSize(2000)
	cursor, err := r.collection.Find(ctx, bson.M{"dict_id": dictID}, opts)
	if err != nil {
		return nil, fmt.Errorf("stream dictionary entries: %w", err)
	}
	return cursor, nil
}
