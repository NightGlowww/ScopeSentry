package dataview

import (
	"context"
	"fmt"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Repository 通用只读列表 + 按 id 删除（fileleak / npoc_service 共用）。
type Repository interface {
	List(ctx context.Context, collection string, filter bson.M, sort bson.D, pageIndex, pageSize int) ([]bson.M, int64, error)
	DeleteByIDs(ctx context.Context, collection string, ids []string) (int64, error)
}

type repository struct{ db *mongo.Database }

// NewRepository 创建仓库
func NewRepository() Repository { return &repository{db: mongodb.DB} }

// List 分页查询（投影交给调用方在 filter 之外处理；这里返回原始文档，_id 转成 hex）。
func (r *repository) List(ctx context.Context, collection string, filter bson.M, sort bson.D, pageIndex, pageSize int) ([]bson.M, int64, error) {
	if pageIndex < 1 {
		pageIndex = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}
	coll := r.db.Collection(collection)
	total, err := coll.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count %s: %w", collection, err)
	}
	ops := options.Find().SetSkip(int64((pageIndex - 1) * pageSize)).SetLimit(int64(pageSize))
	if len(sort) > 0 {
		ops = ops.SetSort(sort)
	}
	cursor, err := coll.Find(ctx, filter, ops)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list %s: %w", collection, err)
	}
	defer cursor.Close(ctx)
	rows := make([]bson.M, 0, pageSize)
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return nil, 0, fmt.Errorf("failed to decode %s: %w", collection, err)
		}
		if oid, ok := doc["_id"].(primitive.ObjectID); ok {
			doc["_id"] = oid.Hex()
		}
		rows = append(rows, doc)
	}
	return rows, total, cursor.Err()
}

// DeleteByIDs 按 _id 批量删除。
func (r *repository) DeleteByIDs(ctx context.Context, collection string, ids []string) (int64, error) {
	objIDs := make([]primitive.ObjectID, 0, len(ids))
	for _, id := range ids {
		oid, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			return 0, fmt.Errorf("invalid id %q: %w", id, err)
		}
		objIDs = append(objIDs, oid)
	}
	res, err := r.db.Collection(collection).DeleteMany(ctx, bson.M{"_id": bson.M{"$in": objIDs}})
	if err != nil {
		return 0, fmt.Errorf("failed to delete from %s: %w", collection, err)
	}
	return res.DeletedCount, nil
}
