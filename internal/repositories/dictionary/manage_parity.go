// repositories-------------------------------------
// @file      : manage_parity.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2026/11/18 02:30
// -------------------------------------------

package dictionary

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/gridfs"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// listSort 列表默认排序：最近更新的在前，同时间按名字升序，_id 兜底保证分页稳定。
// 对应索引 (category, updated_at) / (updated_at)。
func listSort() bson.D {
	return bson.D{
		{Key: "updated_at", Value: -1},
		{Key: "name", Value: 1},
		{Key: "_id", Value: 1},
	}
}

// ListPaged 按 filter 分页查询元数据。
func (r *manageRepository) ListPaged(ctx context.Context, filter bson.M, skip, limit int64) ([]models.DictionaryMeta, error) {
	opts := options.Find().
		SetSort(listSort()).
		SetSkip(skip).
		SetLimit(limit)
	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("list dictionaries: %w", err)
	}
	defer cursor.Close(ctx)

	var rows []models.DictionaryMeta
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("decode dictionaries: %w", err)
	}
	return rows, nil
}

// Count 统计满足条件的字典数量。
func (r *manageRepository) Count(ctx context.Context, filter bson.M) (int64, error) {
	n, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("count dictionaries: %w", err)
	}
	return n, nil
}

// CountByCategory 按 kind 聚合数量。
// 聚合显式打开 AllowDiskUse：字典数量可能很大，不能因为 100MB 内存上限直接失败。
func (r *manageRepository) CountByCategory(ctx context.Context) ([]models.DictionaryKindCount, error) {
	pipeline := bson.A{
		bson.M{"$group": bson.M{"_id": "$category", "count": bson.M{"$sum": 1}}},
		bson.M{"$sort": bson.M{"count": -1}},
	}
	opts := options.Aggregate().SetAllowDiskUse(true)
	cursor, err := r.collection.Aggregate(ctx, pipeline, opts)
	if err != nil {
		return nil, fmt.Errorf("aggregate dictionary kinds: %w", err)
	}
	defer cursor.Close(ctx)

	var rows []models.DictionaryKindCount
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("decode dictionary kinds: %w", err)
	}
	return rows, nil
}

// FindMetaByNameCategory 精确取一条；不存在返回 (nil, nil)，由 service 映射成哨兵错误。
func (r *manageRepository) FindMetaByNameCategory(ctx context.Context, name, category string) (*models.DictionaryMeta, error) {
	var meta models.DictionaryMeta
	err := r.collection.FindOne(ctx, bson.M{"name": name, "category": category}).Decode(&meta)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find dictionary %s/%s: %w", category, name, err)
	}
	return &meta, nil
}

// FindMetaByID 按 ID 取元数据；不存在返回 (nil, nil)。
func (r *manageRepository) FindMetaByID(ctx context.Context, id string) (*models.DictionaryMeta, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, fmt.Errorf("invalid dictionary id %q: %w", id, err)
	}
	var meta models.DictionaryMeta
	if err := r.collection.FindOne(ctx, bson.M{"_id": oid}).Decode(&meta); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, fmt.Errorf("find dictionary %s: %w", id, err)
	}
	return &meta, nil
}

// DeleteMetaByNameCategory 按 (name, category) 删除元数据。
func (r *manageRepository) DeleteMetaByNameCategory(ctx context.Context, name, category string) error {
	if _, err := r.collection.DeleteMany(ctx, bson.M{"name": name, "category": category}); err != nil {
		return fmt.Errorf("delete dictionary %s/%s: %w", category, name, err)
	}
	return nil
}

// UpdateMetaStats 更新行数/大小/对齐标记/更新时间。
func (r *manageRepository) UpdateMetaStats(ctx context.Context, id string, size string, lineCount int64, synced bool) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return fmt.Errorf("invalid dictionary id %q: %w", id, err)
	}
	if _, err := r.collection.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": bson.M{
		"size":           size,
		"line_count":     lineCount,
		"entries_synced": synced,
		"updated_at":     time.Now(),
	}}); err != nil {
		return fmt.Errorf("update dictionary stats %s: %w", id, err)
	}
	return nil
}

// MarkEntriesSynced 回填完成后只更新行数与标记，不改 updated_at。
func (r *manageRepository) MarkEntriesSynced(ctx context.Context, id string, lineCount int64) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return fmt.Errorf("invalid dictionary id %q: %w", id, err)
	}
	if _, err := r.collection.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": bson.M{
		"line_count":     lineCount,
		"entries_synced": true,
	}}); err != nil {
		return fmt.Errorf("mark dictionary synced %s: %w", id, err)
	}
	return nil
}

// NextSeq 用 $inc 原子分配一段自增序号，返回区间起点。
func (r *manageRepository) NextSeq(ctx context.Context, id string, n int64) (int64, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return 0, fmt.Errorf("invalid dictionary id %q: %w", id, err)
	}
	var doc struct {
		EntrySeq int64 `bson:"entry_seq"`
	}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	err = r.collection.FindOneAndUpdate(ctx, bson.M{"_id": oid}, bson.M{"$inc": bson.M{"entry_seq": n}}, opts).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return 0, fmt.Errorf("dictionary %s disappeared: %w", id, mongo.ErrNoDocuments)
		}
		return 0, fmt.Errorf("allocate entry seq for %s: %w", id, err)
	}
	return doc.EntrySeq - n + 1, nil
}

// DeleteFileSafe 删除该名字下所有 GridFS 版本；不存在视为成功。
func (r *manageRepository) DeleteFileSafe(ctx context.Context, filename string) error {
	bucket, err := r.getBucket()
	if err != nil {
		return err
	}
	cursor, err := mongodb.DB.Collection("fs.files").Find(ctx,
		bson.M{"filename": filename},
		options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return fmt.Errorf("find gridfs file %s: %w", filename, err)
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var doc struct {
			ID primitive.ObjectID `bson:"_id"`
		}
		if err := cursor.Decode(&doc); err != nil {
			return fmt.Errorf("decode gridfs file %s: %w", filename, err)
		}
		if err := bucket.Delete(doc.ID); err != nil && !errors.Is(err, gridfs.ErrFileNotFound) {
			return fmt.Errorf("delete gridfs file %s: %w", filename, err)
		}
	}
	if err := cursor.Err(); err != nil {
		return fmt.Errorf("iterate gridfs files %s: %w", filename, err)
	}
	return nil
}

// OpenFileStream 打开 GridFS 内容流。
func (r *manageRepository) OpenFileStream(ctx context.Context, filename string) (*gridfs.DownloadStream, error) {
	bucket, err := r.getBucket()
	if err != nil {
		return nil, err
	}
	ds, err := bucket.OpenDownloadStreamByName(filename)
	if err != nil {
		return nil, fmt.Errorf("open gridfs file %s: %w", filename, err)
	}
	return ds, nil
}

// FileLength 读 fs.files 里的长度，不打开内容流。
func (r *manageRepository) FileLength(ctx context.Context, filename string) (int64, bool, error) {
	var doc struct {
		Length int64 `bson:"length"`
	}
	err := mongodb.DB.Collection("fs.files").
		FindOne(ctx, bson.M{"filename": filename}, options.FindOne().SetSort(bson.D{{Key: "uploadDate", Value: -1}})).
		Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read gridfs length %s: %w", filename, err)
	}
	return doc.Length, true, nil
}

// WriteFileStream 以流方式写入 GridFS；fill 出错时回收半成品文件。
func (r *manageRepository) WriteFileStream(ctx context.Context, filename string, fill func(io.Writer) error) error {
	bucket, err := r.getBucket()
	if err != nil {
		return err
	}
	stream, err := bucket.OpenUploadStream(filename)
	if err != nil {
		return fmt.Errorf("open gridfs upload %s: %w", filename, err)
	}
	if err := fill(stream); err != nil {
		fileID := stream.FileID
		_ = stream.Close()
		if oid, ok := fileID.(primitive.ObjectID); ok {
			_ = bucket.Delete(oid)
		}
		return err
	}
	if err := stream.Close(); err != nil {
		return fmt.Errorf("close gridfs upload %s: %w", filename, err)
	}
	return nil
}
