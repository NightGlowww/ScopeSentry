package tagcolor

import (
	"context"
	"fmt"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// CollectionName 标签颜色覆盖集合
const CollectionName = "TagColor"

// Repository 标签颜色覆盖的数据访问
type Repository interface {
	List(ctx context.Context) (map[string]string, error)
	Set(ctx context.Context, tag, color string) error
	Delete(ctx context.Context, tag string) error
}

type repository struct {
	db *mongo.Database
}

// NewRepository 创建仓库实例
func NewRepository() Repository {
	return &repository{db: mongodb.DB}
}

// List 返回 tag → color；没有覆盖的标签不会出现在结果里。
func (r *repository) List(ctx context.Context) (map[string]string, error) {
	result := make(map[string]string)
	if r.db == nil {
		return result, nil
	}
	cursor, err := r.db.Collection(CollectionName).Find(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("failed to list tag colors: %w", err)
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var doc struct {
			Tag   string `bson:"tag"`
			Color string `bson:"color"`
		}
		if err := cursor.Decode(&doc); err != nil {
			return nil, fmt.Errorf("failed to decode tag color: %w", err)
		}
		if doc.Tag == "" || doc.Color == "" {
			continue
		}
		result[doc.Tag] = doc.Color
	}
	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate tag colors: %w", err)
	}
	return result, nil
}

// Set 写入/覆盖某个标签的颜色（按 tag 唯一）。
func (r *repository) Set(ctx context.Context, tag, color string) error {
	if r.db == nil {
		return fmt.Errorf("database is not initialized")
	}
	_, err := r.db.Collection(CollectionName).UpdateOne(
		ctx,
		bson.M{"tag": tag},
		bson.M{"$set": bson.M{"tag": tag, "color": color, "updatedAt": time.Now()}},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("failed to set tag color: %w", err)
	}
	return nil
}

// Delete 删除某个标签的颜色覆盖。
func (r *repository) Delete(ctx context.Context, tag string) error {
	if r.db == nil {
		return fmt.Errorf("database is not initialized")
	}
	if _, err := r.db.Collection(CollectionName).DeleteMany(ctx, bson.M{"tag": tag}); err != nil {
		return fmt.Errorf("failed to delete tag color: %w", err)
	}
	return nil
}
