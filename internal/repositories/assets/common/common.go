package common

import (
	"context"
	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/mongo/options"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// Repository 定义通用数据访问接口
type Repository interface {
	DeleteMany(ctx context.Context, collection string, filter bson.M) (int64, error)
	FindOne(ctx context.Context, collection string, filter bson.M) (*models.Document, error)
	Find(ctx context.Context, collection string, filter bson.M, limit int64, fields []string, result interface{}) error
	UpdateOne(ctx context.Context, collection string, filter bson.M, update bson.M) error
	CountDocuments(ctx context.Context, collection string, filter bson.M) (int64, error)
	EstimatedDocumentCount(ctx context.Context, collection string) (int64, error)
	// Count 精确计数（带 filter）
	Count(ctx context.Context, collection string, filter bson.M) (int64, error)
	DeleteManyByFilter(ctx context.Context, collection string, filter bson.M) (int64, error)
}

type repository struct {
	db *mongo.Database
}

// NewRepository 创建新的repository实例
func NewRepository() Repository {
	return &repository{
		db: mongodb.DB,
	}
}

func (r *repository) Find(ctx context.Context, collection string, filter bson.M, limit int64, fields []string, result interface{}) error {
	findOpts := options.Find()
	if limit > 0 {
		findOpts.SetLimit(limit)
	}
	if len(fields) > 0 {
		projection := bson.M{}
		for _, f := range fields {
			projection[f] = 1
		}
		findOpts.SetProjection(projection)
	}
	cur, err := r.db.Collection(collection).Find(ctx, filter, findOpts)
	if err != nil {
		return err
	}
	defer cur.Close(ctx)

	return cur.All(ctx, result)
}

// DeleteMany 批量删除文档
func (r *repository) DeleteMany(ctx context.Context, collection string, filter bson.M) (int64, error) {
	result, err := r.db.Collection(collection).DeleteMany(ctx, filter)
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}

// FindOne 查找单个文档
func (r *repository) FindOne(ctx context.Context, collection string, filter bson.M) (*models.Document, error) {
	var doc models.Document
	err := r.db.Collection(collection).FindOne(ctx, filter).Decode(&doc)
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

// UpdateOne 更新单个文档
func (r *repository) UpdateOne(ctx context.Context, collection string, filter bson.M, update bson.M) error {
	res, err := r.db.Collection(collection).UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	// 匹配 0 条不是「成功」：驱动的 UpdateOne 对「没有文档命中」不报错，
	// 直接把 nil 往上抛，调用方就会回 200 + 什么也没改（假成功）。统一成 ErrNoDocuments，
	// 由上层映射成 404（2026-11-28 集成测试发现：改状态传不存在的 id 会回 200）。
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// CountDocuments 统计文档数量
func (r *repository) CountDocuments(ctx context.Context, collection string, filter bson.M) (int64, error) {
	return r.db.Collection(collection).CountDocuments(ctx, filter)
}

// EstimatedDocumentCount 获取集合的估计文档数量
func (r *repository) EstimatedDocumentCount(ctx context.Context, collection string) (int64, error) {
	return r.db.Collection(collection).EstimatedDocumentCount(ctx)
}

// Count 精确计数（带 filter 时走 CountDocuments；空 filter 也应使用 EstimatedDocumentCount）
func (r *repository) Count(ctx context.Context, collection string, filter bson.M) (int64, error) {
	return r.db.Collection(collection).CountDocuments(ctx, filter)
}

// DeleteManyByFilter 根据过滤条件批量删除
func (r *repository) DeleteManyByFilter(ctx context.Context, collection string, filter bson.M) (int64, error) {
	result, err := r.db.Collection(collection).DeleteMany(ctx, filter)
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}
