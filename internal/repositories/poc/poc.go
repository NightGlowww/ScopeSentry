package poc

import (
	"context"
	"fmt"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Repository 定义POC仓库接口
type Repository interface {
	FindWithPagination(ctx context.Context, query bson.M, opts *options.FindOptions) ([]models.Poc, error)
	Count(ctx context.Context, query bson.M) (int64, error)
	FindOne(ctx context.Context, query bson.M) (*models.Poc, error)
	InsertOne(ctx context.Context, doc models.Poc) (*mongo.InsertOneResult, error)
	InsertMany(ctx context.Context, docs []models.Poc) (*mongo.InsertManyResult, error)
	UpdateOne(ctx context.Context, filter bson.M, update bson.M) (*mongo.UpdateResult, error)
	DeleteMany(ctx context.Context, filter bson.M) (*mongo.DeleteResult, error)
	GetAllPocData(ctx context.Context) ([]models.Poc, error)
	GetAllTemplateId(ctx context.Context) ([]string, error)
	TemplateIdExists(ctx context.Context, templateId string) (bool, error)
	// GetTemplateIndex 返回 template id -> {_id, content_hash} 的索引，
	// 导入时用它判断「同 id 是否真的变了」，不必把全量模板内容读进内存。
	GetTemplateIndex(ctx context.Context) (map[string]models.PocIndexEntry, error)
}

type repository struct {
	collection *mongo.Collection
}

// NewRepository 创建新的POC仓库实例
func NewRepository() Repository {
	return &repository{
		collection: mongodb.CollectionOrNil("PocList"),
	}
}

func (r *repository) FindWithPagination(ctx context.Context, query bson.M, opts *options.FindOptions) ([]models.Poc, error) {
	cursor, err := r.collection.Find(ctx, query, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to find documents: %w", err)
	}
	defer cursor.Close(ctx)

	var results []models.Poc
	if err := cursor.All(ctx, &results); err != nil {
		return nil, fmt.Errorf("failed to decode documents: %w", err)
	}

	return results, nil
}

func (r *repository) Count(ctx context.Context, query bson.M) (int64, error) {
	return r.collection.CountDocuments(ctx, query)
}

func (r *repository) FindOne(ctx context.Context, query bson.M) (*models.Poc, error) {
	var result models.Poc
	err := r.collection.FindOne(ctx, query).Decode(&result)
	if err != nil {
		return nil, fmt.Errorf("failed to find document: %w", err)
	}
	return &result, nil
}

func (r *repository) InsertOne(ctx context.Context, doc models.Poc) (*mongo.InsertOneResult, error) {
	return r.collection.InsertOne(ctx, doc)
}

func (r *repository) InsertMany(ctx context.Context, docs []models.Poc) (*mongo.InsertManyResult, error) {
	interfaceDocs := make([]interface{}, len(docs))
	for i, doc := range docs {
		interfaceDocs[i] = doc
	}
	return r.collection.InsertMany(ctx, interfaceDocs)
}

func (r *repository) UpdateOne(ctx context.Context, filter bson.M, update bson.M) (*mongo.UpdateResult, error) {
	return r.collection.UpdateOne(ctx, filter, update)
}

func (r *repository) DeleteMany(ctx context.Context, filter bson.M) (*mongo.DeleteResult, error) {
	return r.collection.DeleteMany(ctx, filter)
}

func (r *repository) GetAllPocData(ctx context.Context) ([]models.Poc, error) {
	// 设置投影，只返回需要的字段
	projection := bson.M{
		"_id":         1,
		"name":        1,
		"time":        1,
		"tags":        1,
		"Template ID": 1,
	}

	opts := options.Find().SetProjection(projection)

	cursor, err := r.collection.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to find documents: %w", err)
	}
	defer cursor.Close(ctx)

	var results []models.Poc
	if err := cursor.All(ctx, &results); err != nil {
		return nil, fmt.Errorf("failed to decode documents: %w", err)
	}

	return results, nil
}

func (r *repository) GetAllTemplateId(ctx context.Context) ([]string, error) {
	// 设置投影，只返回hash字段
	projection := bson.M{
		"id":  1,
		"_id": 0,
	}

	opts := options.Find().SetProjection(projection)

	cursor, err := r.collection.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to find documents: %w", err)
	}
	defer cursor.Close(ctx)

	var results []bson.M
	if err := cursor.All(ctx, &results); err != nil {
		return nil, fmt.Errorf("failed to decode documents: %w", err)
	}

	hashes := make([]string, len(results))
	for i, result := range results {
		if hash, ok := result["id"].(string); ok {
			hashes[i] = hash
		}
	}

	return hashes, nil
}

// GetTemplateIndex 建立 template id -> 内容指纹的索引。
// 历史文档没有 content_hash 字段时返回空字符串，由调用方按需补算。
func (r *repository) GetTemplateIndex(ctx context.Context) (map[string]models.PocIndexEntry, error) {
	opts := options.Find().SetProjection(bson.M{"_id": 1, "id": 1, "content_hash": 1})
	cursor, err := r.collection.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to build template index: %w", err)
	}
	defer cursor.Close(ctx)

	type row struct {
		ID          primitive.ObjectID `bson:"_id"`
		TemplateID  string             `bson:"id"`
		ContentHash string             `bson:"content_hash"`
	}

	index := make(map[string]models.PocIndexEntry)
	for cursor.Next(ctx) {
		var item row
		if err := cursor.Decode(&item); err != nil {
			return nil, fmt.Errorf("failed to decode template index row: %w", err)
		}
		if item.TemplateID == "" {
			continue
		}
		index[item.TemplateID] = models.PocIndexEntry{ID: item.ID, ContentHash: item.ContentHash}
	}
	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate template index: %w", err)
	}
	return index, nil
}

// TemplateIdExists 高性能检查TemplateId是否存在
// 使用CountDocuments而不是FindOne，因为只需要知道是否存在，不需要返回文档
func (r *repository) TemplateIdExists(ctx context.Context, templateId string) (bool, error) {
	filter := bson.M{"id": templateId}
	count, err := r.collection.CountDocuments(ctx, filter, options.Count().SetLimit(1))
	if err != nil {
		return false, fmt.Errorf("failed to check template id existence: %w", err)
	}
	return count > 0, nil
}
