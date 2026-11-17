package apikey

import (
	"context"
	"regexp"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	mongodriver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const collectionName = "api_key"

type Repository interface {
	Create(ctx context.Context, key *models.ApiKey) error
	FindAll(ctx context.Context) ([]models.ApiKey, error)
	// ListPage 分页列表（关键字匹配 name / keyPrefix），按 createdAt 倒序
	ListPage(ctx context.Context, keyword string, pageIndex, pageSize int) ([]models.ApiKey, int64, error)
	FindByHash(ctx context.Context, keyHash string) (*models.ApiKey, error)
	FindByID(ctx context.Context, id string) (*models.ApiKey, error)
	// UpdatePolicy 只更新治理字段（name / scope / tools / 限速 / 过期）
	UpdatePolicy(ctx context.Context, id string, name string, scope, tools []string, rateLimit int, expiresAt *time.Time) error
	SetEnabled(ctx context.Context, id string, enabled bool) error
	Delete(ctx context.Context, id string) error
	UpdateLastUsed(ctx context.Context, id primitive.ObjectID) error
	EnsureIndexes(ctx context.Context) error
}

type repository struct {
	collection *mongodriver.Collection
}

func NewRepository() Repository {
	return &repository{
		collection: mongodb.DB.Collection(collectionName),
	}
}

func (r *repository) Create(ctx context.Context, key *models.ApiKey) error {
	key.ID = primitive.NewObjectID()
	key.CreatedAt = time.Now()
	_, err := r.collection.InsertOne(ctx, key)
	return err
}

func (r *repository) FindAll(ctx context.Context) ([]models.ApiKey, error) {
	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}})
	cursor, err := r.collection.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var keys []models.ApiKey
	if err := cursor.All(ctx, &keys); err != nil {
		return nil, err
	}
	return keys, nil
}

// ListPage 分页查询，走 api_key.createdAt 索引排序，避免全量拉回内存再切片
func (r *repository) ListPage(ctx context.Context, keyword string, pageIndex, pageSize int) ([]models.ApiKey, int64, error) {
	if pageIndex < 1 {
		pageIndex = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > models.McpMaxPageSize {
		pageSize = models.McpMaxPageSize
	}

	filter := bson.M{}
	if keyword != "" {
		pattern := bson.M{"$regex": regexp.QuoteMeta(keyword), "$options": "i"}
		filter = bson.M{"$or": bson.A{
			bson.M{"name": pattern},
			bson.M{"keyPrefix": pattern},
		}}
	}

	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	opts := options.Find().
		SetSkip(int64((pageIndex - 1) * pageSize)).
		SetLimit(int64(pageSize)).
		SetSort(bson.D{{Key: "createdAt", Value: -1}})
	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var keys []models.ApiKey
	if err := cursor.All(ctx, &keys); err != nil {
		return nil, 0, err
	}
	return keys, total, nil
}

// FindByHash 按 key 哈希取记录。
// 不加 enabled 过滤：禁用/过期的 key 也要能识别出来，审计日志才记得上 keyId。
func (r *repository) FindByHash(ctx context.Context, keyHash string) (*models.ApiKey, error) {
	var key models.ApiKey
	err := r.collection.FindOne(ctx, bson.M{"keyHash": keyHash}).Decode(&key)
	if err != nil {
		return nil, err
	}
	return &key, nil
}

// FindByID 按主键取记录
func (r *repository) FindByID(ctx context.Context, id string) (*models.ApiKey, error) {
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	var key models.ApiKey
	if err := r.collection.FindOne(ctx, bson.M{"_id": objID}).Decode(&key); err != nil {
		return nil, err
	}
	return &key, nil
}

// UpdatePolicy 更新治理字段
func (r *repository) UpdatePolicy(ctx context.Context, id string, name string, scope, tools []string, rateLimit int, expiresAt *time.Time) error {
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	set := bson.M{
		"scope":           models.NormalizeStringList(scope),
		"tools":           models.NormalizeStringList(tools),
		"rateLimitPerMin": rateLimit,
	}
	if name != "" {
		set["name"] = name
	}
	if expiresAt != nil {
		set["expires_at"] = expiresAt
	} else {
		set["expires_at"] = nil
	}
	_, err = r.collection.UpdateOne(ctx, bson.M{"_id": objID}, bson.M{"$set": set})
	return err
}

// SetEnabled 启用/禁用
func (r *repository) SetEnabled(ctx context.Context, id string, enabled bool) error {
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = r.collection.UpdateOne(ctx, bson.M{"_id": objID}, bson.M{"$set": bson.M{"enabled": enabled}})
	return err
}

func (r *repository) Delete(ctx context.Context, id string) error {
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = r.collection.DeleteOne(ctx, bson.M{"_id": objID})
	return err
}

func (r *repository) UpdateLastUsed(ctx context.Context, id primitive.ObjectID) error {
	now := time.Now()
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"lastUsedAt": now}})
	return err
}

func (r *repository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateOne(ctx, mongodriver.IndexModel{
		Keys:    bson.D{{Key: "keyHash", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return err
	}
	// 列表接口按 createdAt 倒序分页
	_, err = r.collection.Indexes().CreateOne(ctx, mongodriver.IndexModel{
		Keys: bson.D{{Key: "createdAt", Value: -1}},
	})
	return err
}
