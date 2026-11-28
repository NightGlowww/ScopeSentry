package poc

import (
	"context"
	"errors"
	"fmt"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrPocNotFound POC 不存在（查不到就是查不到，不要把 mongo 原始错误带到上层文案）
var ErrPocNotFound = errors.New("poc not found")

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
	// SetDisabled 批量停用/启用模板，返回实际命中的条数（0 表示 id 都不存在）
	SetDisabled(ctx context.Context, ids []primitive.ObjectID, disabled bool) (int64, error)
	// DisabledTemplateIDs 在给定 template id 里挑出被停用的（扫描载荷组装用）
	DisabledTemplateIDs(ctx context.Context, templateIDs []string) (map[string]bool, error)
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

// SetDisabled 批量停用/启用模板。
func (r *repository) SetDisabled(ctx context.Context, ids []primitive.ObjectID, disabled bool) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res, err := r.collection.UpdateMany(ctx,
		bson.M{"_id": bson.M{"$in": ids}},
		bson.M{"$set": bson.M{"disabled": disabled}})
	if err != nil {
		return 0, fmt.Errorf("poc: set disabled: %w", err)
	}
	return res.MatchedCount, nil
}

// DisabledTemplateIDs 返回给定 template id 里处于停用状态的那些。
// 只查 template id + disabled 两个字段：扫描载荷组装时每个任务都会调一次，
// 不能把模板正文（单条可达数百 KB）读进来。
func (r *repository) DisabledTemplateIDs(ctx context.Context, templateIDs []string) (map[string]bool, error) {
	out := make(map[string]bool)
	if len(templateIDs) == 0 {
		return out, nil
	}
	filter := bson.M{"id": bson.M{"$in": templateIDs}, "disabled": true}
	opts := options.Find().SetProjection(bson.M{"id": 1, "_id": 0})
	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("poc: find disabled ids: %w", err)
	}
	defer cursor.Close(ctx)
	var rows []struct {
		TemplateID string `bson:"id"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("poc: decode disabled ids: %w", err)
	}
	for _, row := range rows {
		out[row.TemplateID] = true
	}
	return out, nil
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
		// 查不到就是查不到：不要把 mongo 的原始错误一路带到 UI/MCP 文案里
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrPocNotFound
		}
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
