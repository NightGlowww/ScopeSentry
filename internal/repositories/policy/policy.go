package policy

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// scan_policy 读写 + 校验所需的两个只读引用（ScanTemplates / plugins）。
//
// 名称唯一由 initdb 的 {name:1} unique 索引兜底，Create/Update 里同时做
// 「先查一次给友好错误 + 撞索引兜底」两层，避免并发注册出重名。

var (
	// ErrInvalidID 非法 ObjectID
	ErrInvalidID = errors.New("invalid id")
	// ErrNotFound 策略不存在
	ErrNotFound = errors.New("scan policy not found")
	// ErrNameExists 策略名已存在
	ErrNameExists = errors.New("scan policy name already exists")
)

// Update 部分更新：nil 字段不落库
type Update struct {
	Name        *string
	Description *string
	Config      *models.ScanPolicyConfig
	Enabled     *bool
}

// Repository scan_policy 数据访问
type Repository interface {
	Create(ctx context.Context, p *models.ScanPolicy) error
	Get(ctx context.Context, id string) (*models.ScanPolicy, error)
	GetByName(ctx context.Context, name string) (*models.ScanPolicy, error)
	List(ctx context.Context, q models.ScanPolicyListQuery) ([]models.ScanPolicy, int64, error)
	Update(ctx context.Context, id string, u Update) (*models.ScanPolicy, error)
	Delete(ctx context.Context, ids []string) (int64, error)

	// MissingPlugins 返回在 plugins 集合里既查不到 hash 也查不到 name 的引用（保序）
	MissingPlugins(ctx context.Context, plugins []string) ([]string, error)
	// TemplateExists 扫描模板是否存在
	TemplateExists(ctx context.Context, id string) (bool, error)
	// LoadTemplatePhases 读模板的分阶段插件清单（apply 传模板 ID 时生成 baseline）
	LoadTemplatePhases(ctx context.Context, id string) (models.ScanTemplatePhases, error)
}

type repository struct{}

// NewRepository 创建仓库（集合按调用期解析，DB 未初始化时报错而不是 panic）
func NewRepository() Repository { return &repository{} }

func (r *repository) coll() (*mongo.Collection, error) {
	if mongodb.DB == nil {
		return nil, errors.New("scan_policy: database not initialized")
	}
	return mongodb.DB.Collection(CollectionPolicy), nil
}

func parseObjectID(id string) (primitive.ObjectID, error) {
	oid, err := primitive.ObjectIDFromHex(strings.TrimSpace(id))
	if err != nil {
		return primitive.NilObjectID, ErrInvalidID
	}
	return oid, nil
}

func (r *repository) Create(ctx context.Context, p *models.ScanPolicy) error {
	collection, err := r.coll()
	if err != nil {
		return err
	}
	now := time.Now()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	res, err := collection.InsertOne(ctx, p)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrNameExists
		}
		return fmt.Errorf("scan_policy: insert: %w", err)
	}
	if oid, ok := res.InsertedID.(primitive.ObjectID); ok {
		p.ID = oid
	}
	return nil
}

func (r *repository) Get(ctx context.Context, id string) (*models.ScanPolicy, error) {
	oid, err := parseObjectID(id)
	if err != nil {
		return nil, err
	}
	collection, err := r.coll()
	if err != nil {
		return nil, err
	}
	var p models.ScanPolicy
	if err := collection.FindOne(ctx, bson.M{"_id": oid}).Decode(&p); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan_policy: get: %w", err)
	}
	return &p, nil
}

func (r *repository) GetByName(ctx context.Context, name string) (*models.ScanPolicy, error) {
	collection, err := r.coll()
	if err != nil {
		return nil, err
	}
	var p models.ScanPolicy
	if err := collection.FindOne(ctx, bson.M{"name": name}).Decode(&p); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan_policy: get by name: %w", err)
	}
	return &p, nil
}

func buildListFilter(q models.ScanPolicyListQuery) bson.M {
	filter := bson.M{}
	if q.Enabled != nil {
		filter["enabled"] = *q.Enabled
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		re := primitive.Regex{Pattern: regexp.QuoteMeta(kw), Options: "i"}
		filter["$or"] = []bson.M{{"name": re}, {"description": re}}
	}
	return filter
}

func (r *repository) List(ctx context.Context, q models.ScanPolicyListQuery) ([]models.ScanPolicy, int64, error) {
	collection, err := r.coll()
	if err != nil {
		return nil, 0, err
	}
	filter := buildListFilter(q)
	total, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("scan_policy: count: %w", err)
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "updatedAt", Value: -1}}).
		SetSkip(int64((q.PageIndex - 1) * q.PageSize)).
		SetLimit(int64(q.PageSize))
	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("scan_policy: find: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()
	list := make([]models.ScanPolicy, 0, q.PageSize)
	if err := cursor.All(ctx, &list); err != nil {
		return nil, 0, fmt.Errorf("scan_policy: decode list: %w", err)
	}
	return list, total, nil
}

func (r *repository) Update(ctx context.Context, id string, u Update) (*models.ScanPolicy, error) {
	oid, err := parseObjectID(id)
	if err != nil {
		return nil, err
	}
	collection, err := r.coll()
	if err != nil {
		return nil, err
	}
	set := bson.M{"updatedAt": time.Now()}
	if u.Name != nil {
		set["name"] = *u.Name
	}
	if u.Description != nil {
		set["description"] = *u.Description
	}
	if u.Enabled != nil {
		set["enabled"] = *u.Enabled
	}
	if u.Config != nil {
		set["config"] = *u.Config
	}

	var out models.ScanPolicy
	err = collection.FindOneAndUpdate(ctx,
		bson.M{"_id": oid},
		bson.M{"$set": set},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&out)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		if mongo.IsDuplicateKeyError(err) {
			return nil, ErrNameExists
		}
		return nil, fmt.Errorf("scan_policy: update: %w", err)
	}
	return &out, nil
}

func (r *repository) Delete(ctx context.Context, ids []string) (int64, error) {
	oids := make([]primitive.ObjectID, 0, len(ids))
	for _, id := range ids {
		oid, err := parseObjectID(id)
		if err != nil {
			return 0, err
		}
		oids = append(oids, oid)
	}
	if len(oids) == 0 {
		return 0, ErrInvalidID
	}
	collection, err := r.coll()
	if err != nil {
		return 0, err
	}
	res, err := collection.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": oids}})
	if err != nil {
		return 0, fmt.Errorf("scan_policy: delete: %w", err)
	}
	return res.DeletedCount, nil
}

// pluginRefChunk 批量查插件时的单批上限（避免 $in 过长）
const pluginRefChunk = 500

func (r *repository) MissingPlugins(ctx context.Context, plugins []string) ([]string, error) {
	if len(plugins) == 0 {
		return nil, nil
	}
	if mongodb.DB == nil {
		return nil, errors.New("scan_policy: database not initialized")
	}
	collection := mongodb.DB.Collection(CollectionPlugins)
	found := make(map[string]struct{}, len(plugins))

	for start := 0; start < len(plugins); start += pluginRefChunk {
		end := start + pluginRefChunk
		if end > len(plugins) {
			end = len(plugins)
		}
		chunk := plugins[start:end]
		in := make([]interface{}, 0, len(chunk))
		for _, v := range chunk {
			in = append(in, v)
		}
		cursor, err := collection.Find(ctx, bson.M{"$or": []bson.M{
			{"hash": bson.M{"$in": in}},
			{"name": bson.M{"$in": in}},
		}}, options.Find().SetProjection(bson.M{"hash": 1, "name": 1, "_id": 0}))
		if err != nil {
			return nil, fmt.Errorf("scan_policy: find plugins: %w", err)
		}
		var docs []struct {
			Hash string `bson:"hash"`
			Name string `bson:"name"`
		}
		if err := cursor.All(ctx, &docs); err != nil {
			_ = cursor.Close(ctx)
			return nil, fmt.Errorf("scan_policy: decode plugins: %w", err)
		}
		_ = cursor.Close(ctx)
		for _, d := range docs {
			if d.Hash != "" {
				found[d.Hash] = struct{}{}
			}
			if d.Name != "" {
				found[d.Name] = struct{}{}
			}
		}
	}

	missing := make([]string, 0, len(plugins))
	for _, p := range plugins {
		if _, ok := found[p]; !ok {
			missing = append(missing, p)
		}
	}
	return missing, nil
}

func (r *repository) TemplateExists(ctx context.Context, id string) (bool, error) {
	oid, err := parseObjectID(id)
	if err != nil {
		return false, err
	}
	if mongodb.DB == nil {
		return false, errors.New("scan_policy: database not initialized")
	}
	count, err := mongodb.DB.Collection(CollectionTemplates).CountDocuments(ctx, bson.M{"_id": oid})
	if err != nil {
		return false, fmt.Errorf("scan_policy: count templates: %w", err)
	}
	return count > 0, nil
}

func (r *repository) LoadTemplatePhases(ctx context.Context, id string) (models.ScanTemplatePhases, error) {
	oid, err := parseObjectID(id)
	if err != nil {
		return models.ScanTemplatePhases{}, err
	}
	if mongodb.DB == nil {
		return models.ScanTemplatePhases{}, errors.New("scan_policy: database not initialized")
	}
	var phases models.ScanTemplatePhases
	err = mongodb.DB.Collection(CollectionTemplates).FindOne(ctx, bson.M{"_id": oid}).Decode(&phases)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return models.ScanTemplatePhases{}, ErrNotFound
		}
		return models.ScanTemplatePhases{}, fmt.Errorf("scan_policy: load template: %w", err)
	}
	return phases, nil
}
