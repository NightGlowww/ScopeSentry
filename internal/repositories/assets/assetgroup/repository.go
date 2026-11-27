package assetgroup

import (
	"context"
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

// 集合名与 ARL-Next 保持一致，便于后续对照与数据迁移
const (
	GroupCollection = "asset_group"
	ScopeCollection = "asset_scope"
)

// Repository 资产集团 + 地契的数据访问
type Repository interface {
	ListGroups(ctx context.Context) ([]models.AssetGroup, error)
	GetGroup(ctx context.Context, id string) (*models.AssetGroup, error)
	FindGroupByName(ctx context.Context, name string) (*models.AssetGroup, error)
	CreateGroup(ctx context.Context, g *models.AssetGroup) (string, error)
	UpdateGroup(ctx context.Context, g *models.AssetGroup) error
	DeleteGroup(ctx context.Context, id string) error
	ReorderGroups(ctx context.Context, ids []string) error
	GroupScopeCounts(ctx context.Context) (map[string]int64, error)

	ListScopes(ctx context.Context, filter bson.M, pageIndex, pageSize int) ([]models.AssetScope, int64, error)
	GetScope(ctx context.Context, id string) (*models.AssetScope, error)
	CreateScope(ctx context.Context, s *models.AssetScope) (string, error)
	UpdateScope(ctx context.Context, s *models.AssetScope) error
	// DeleteScopes 批量删除，返回真实删除条数
	DeleteScopes(ctx context.Context, ids []string) (int64, error)
	MoveScopes(ctx context.Context, ids []string, groupID string) error
	// DissolveGroup 把该集团下所有地契移入未分组（删除集团时 dissolve）
	DissolveGroup(ctx context.Context, groupID string) error
	// TransferGroup 把该集团下所有地契划转到目标集团（删除集团时 transfer）
	TransferGroup(ctx context.Context, fromGroupID, toGroupID string) error
	ClearUpdateErrors(ctx context.Context, ids []string) error
	BindEnterprise(ctx context.Context, id, enterprise string) error
	// FindScopeByName 按「集团 + 名称」定位地契（ICP 同步用；groupID 为空表示未分组）
	FindScopeByName(ctx context.Context, groupID, name string) (*models.AssetScope, error)
	// MergeScopeDomains 只向 domain_array / scope_array 追加（$addToSet 去重），
	// 不删改任何已有条目，也不动 ip_array 等其它字段
	MergeScopeDomains(ctx context.Context, scopeID string, domains []string) (added int64, total int64, err error)
}

type repository struct {
	db *mongo.Database
}

// NewRepository 创建仓库实例
func NewRepository() Repository {
	return &repository{db: mongodb.DB}
}

func (r *repository) groups() *mongo.Collection { return r.db.Collection(GroupCollection) }
func (r *repository) scopes() *mongo.Collection { return r.db.Collection(ScopeCollection) }

// ListGroups 按 sort_order、创建时间排序返回全部集团（不分组时用于树形/列表展示）
func (r *repository) ListGroups(ctx context.Context) ([]models.AssetGroup, error) {
	if r.db == nil {
		return nil, fmt.Errorf("database is not initialized")
	}
	ops := options.Find().SetSort(bson.D{{Key: "sort_order", Value: 1}, {Key: "created_at", Value: 1}})
	cursor, err := r.groups().Find(ctx, bson.M{}, ops)
	if err != nil {
		return nil, fmt.Errorf("failed to list asset groups: %w", err)
	}
	defer cursor.Close(ctx)
	var result []models.AssetGroup
	if err := cursor.All(ctx, &result); err != nil {
		return nil, fmt.Errorf("failed to decode asset groups: %w", err)
	}
	return result, nil
}

// GroupScopeCounts 统计每个集团下的地契数量（列表接口用 scopeCount 展示）
func (r *repository) GroupScopeCounts(ctx context.Context) (map[string]int64, error) {
	result := make(map[string]int64)
	if r.db == nil {
		return result, nil
	}
	cursor, err := r.scopes().Aggregate(ctx, mongo.Pipeline{
		bson.D{{Key: "$group", Value: bson.D{{Key: "_id", Value: "$group_id"}, {Key: "n", Value: bson.D{{Key: "$sum", Value: 1}}}}}},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to count scopes: %w", err)
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var row struct {
			ID string `bson:"_id"`
			N  int64  `bson:"n"`
		}
		if err := cursor.Decode(&row); err != nil {
			return nil, fmt.Errorf("failed to decode scope count: %w", err)
		}
		if row.ID != "" {
			result[row.ID] = row.N
		}
	}
	return result, cursor.Err()
}

// GetGroup 按 ID 取集团
func (r *repository) GetGroup(ctx context.Context, id string) (*models.AssetGroup, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid group id", ErrInvalidID)
	}
	var g models.AssetGroup
	if err := r.groups().FindOne(ctx, bson.M{"_id": oid}).Decode(&g); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get asset group: %w", err)
	}
	return &g, nil
}

// FindGroupByName 集团名称全局唯一
func (r *repository) FindGroupByName(ctx context.Context, name string) (*models.AssetGroup, error) {
	var g models.AssetGroup
	if err := r.groups().FindOne(ctx, bson.M{"name": name}).Decode(&g); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find asset group: %w", err)
	}
	return &g, nil
}

// CreateGroup 新建集团
func (r *repository) CreateGroup(ctx context.Context, g *models.AssetGroup) (string, error) {
	now := time.Now()
	g.CreatedAt = now
	g.UpdatedAt = now
	res, err := r.groups().InsertOne(ctx, g)
	if err != nil {
		return "", fmt.Errorf("failed to create asset group: %w", err)
	}
	id, _ := res.InsertedID.(primitive.ObjectID)
	return id.Hex(), nil
}

// UpdateGroup 更新名称/描述/排序
func (r *repository) UpdateGroup(ctx context.Context, g *models.AssetGroup) error {
	_, err := r.groups().UpdateOne(ctx, bson.M{"_id": g.ID}, bson.M{"$set": bson.M{
		"name":        g.Name,
		"description": g.Description,
		"sort_order":  g.SortOrder,
		"updated_at":  time.Now(),
	}})
	if err != nil {
		return fmt.Errorf("failed to update asset group: %w", err)
	}
	return nil
}

// DeleteGroup 删除集团（地契的处置由 service 先完成）
func (r *repository) DeleteGroup(ctx context.Context, id string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return fmt.Errorf("%w: invalid group id", ErrInvalidID)
	}
	if _, err := r.groups().DeleteOne(ctx, bson.M{"_id": oid}); err != nil {
		return fmt.Errorf("failed to delete asset group: %w", err)
	}
	return nil
}

// ReorderGroups 按给定顺序写 sort_order
func (r *repository) ReorderGroups(ctx context.Context, ids []string) error {
	for i, id := range ids {
		oid, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			return fmt.Errorf("%w: invalid group id %q", ErrInvalidID, id)
		}
		if _, err := r.groups().UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": bson.M{"sort_order": i, "updated_at": time.Now()}}); err != nil {
			return fmt.Errorf("failed to reorder asset groups: %w", err)
		}
	}
	return nil
}

// ListScopes 分页查询地契
func (r *repository) ListScopes(ctx context.Context, filter bson.M, pageIndex, pageSize int) ([]models.AssetScope, int64, error) {
	if pageIndex < 1 {
		pageIndex = 1
	}
	if pageSize <= 0 || pageSize > 500 {
		pageSize = 20
	}
	total, err := r.scopes().CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count asset scopes: %w", err)
	}
	ops := options.Find().
		SetSkip(int64((pageIndex - 1) * pageSize)).
		SetLimit(int64(pageSize)).
		SetSort(bson.D{{Key: "created_at", Value: -1}})
	cursor, err := r.scopes().Find(ctx, filter, ops)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list asset scopes: %w", err)
	}
	defer cursor.Close(ctx)
	var items []models.AssetScope
	if err := cursor.All(ctx, &items); err != nil {
		return nil, 0, fmt.Errorf("failed to decode asset scopes: %w", err)
	}
	return items, total, nil
}

// GetScope 按 ID 取地契
func (r *repository) GetScope(ctx context.Context, id string) (*models.AssetScope, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid scope id", ErrInvalidID)
	}
	var s models.AssetScope
	if err := r.scopes().FindOne(ctx, bson.M{"_id": oid}).Decode(&s); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get asset scope: %w", err)
	}
	return &s, nil
}

// CreateScope 新建地契
func (r *repository) CreateScope(ctx context.Context, s *models.AssetScope) (string, error) {
	now := time.Now()
	s.CreatedAt = now
	s.UpdatedAt = now
	res, err := r.scopes().InsertOne(ctx, s)
	if err != nil {
		return "", fmt.Errorf("failed to create asset scope: %w", err)
	}
	id, _ := res.InsertedID.(primitive.ObjectID)
	return id.Hex(), nil
}

// UpdateScope 全量更新可变字段
func (r *repository) UpdateScope(ctx context.Context, s *models.AssetScope) error {
	_, err := r.scopes().UpdateOne(ctx, bson.M{"_id": s.ID}, bson.M{"$set": bson.M{
		"name":         s.Name,
		"scope":        s.Scope,
		"scope_type":   s.ScopeType,
		"group_id":     s.GroupID,
		"scope_array":  s.ScopeArray,
		"domain_array": s.DomainArray,
		"ip_array":     s.IPArray,
		"updated_at":   time.Now(),
	}})
	if err != nil {
		return fmt.Errorf("failed to update asset scope: %w", err)
	}
	return nil
}

// DeleteScopes 批量删除
func (r *repository) DeleteScopes(ctx context.Context, ids []string) (int64, error) {
	objIDs, err := toObjectIDs(ids)
	if err != nil {
		return 0, err
	}
	res, err := r.scopes().DeleteMany(ctx, bson.M{"_id": bson.M{"$in": objIDs}})
	if err != nil {
		return 0, fmt.Errorf("failed to delete asset scopes: %w", err)
	}
	return res.DeletedCount, nil
}

// MoveScopes 批量移动到目标集团（groupID 为空 = 未分组）
func (r *repository) MoveScopes(ctx context.Context, ids []string, groupID string) error {
	objIDs, err := toObjectIDs(ids)
	if err != nil {
		return err
	}
	if _, err := r.scopes().UpdateMany(ctx, bson.M{"_id": bson.M{"$in": objIDs}}, bson.M{"$set": bson.M{
		"group_id":   groupID,
		"updated_at": time.Now(),
	}}); err != nil {
		return fmt.Errorf("failed to move asset scopes: %w", err)
	}
	return nil
}

// DissolveGroup 把集团下所有地契的 group_id 置空
func (r *repository) DissolveGroup(ctx context.Context, groupID string) error {
	if _, err := r.scopes().UpdateMany(ctx, bson.M{"group_id": groupID}, bson.M{"$set": bson.M{
		"group_id":   "",
		"updated_at": time.Now(),
	}}); err != nil {
		return fmt.Errorf("failed to dissolve asset group: %w", err)
	}
	return nil
}

// TransferGroup 把集团下所有地契划转到目标集团
func (r *repository) TransferGroup(ctx context.Context, fromGroupID, toGroupID string) error {
	if _, err := r.scopes().UpdateMany(ctx, bson.M{"group_id": fromGroupID}, bson.M{"$set": bson.M{
		"group_id":   toGroupID,
		"updated_at": time.Now(),
	}}); err != nil {
		return fmt.Errorf("failed to transfer asset group: %w", err)
	}
	return nil
}

// ClearUpdateErrors 清空 ICP/同步失败信息
func (r *repository) ClearUpdateErrors(ctx context.Context, ids []string) error {
	objIDs, err := toObjectIDs(ids)
	if err != nil {
		return err
	}
	if _, err := r.scopes().UpdateMany(ctx, bson.M{"_id": bson.M{"$in": objIDs}}, bson.M{"$set": bson.M{
		"update_error": "",
		"updated_at":   time.Now(),
	}}); err != nil {
		return fmt.Errorf("failed to clear update errors: %w", err)
	}
	return nil
}

// BindEnterprise 绑定企业主体
func (r *repository) BindEnterprise(ctx context.Context, id, enterprise string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return fmt.Errorf("%w: invalid scope id", ErrInvalidID)
	}
	if _, err := r.scopes().UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": bson.M{
		"enterprise": enterprise,
		"updated_at": time.Now(),
	}}); err != nil {
		return fmt.Errorf("failed to bind enterprise: %w", err)
	}
	return nil
}

// FindScopeByName 按集团 + 名称定位地契；不存在返回 (nil, nil)。
// ICP 同步只关心「同名地契在不在」，因此同一集团下有重名时取最早创建的一条。
func (r *repository) FindScopeByName(ctx context.Context, groupID, name string) (*models.AssetScope, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	filter := bson.M{"name": name, "group_id": strings.TrimSpace(groupID)}
	opts := options.FindOne().SetSort(bson.D{{Key: "created_at", Value: 1}})
	var scope models.AssetScope
	if err := r.scopes().FindOne(ctx, filter, opts).Decode(&scope); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find asset scope by name: %w", err)
	}
	return &scope, nil
}

// MergeScopeDomains 把域名并进地契的 domain_array / scope_array。
//
// $addToSet + $each 的语义正好是「去重追加」：已有条目顺序不变、不重复，新条目补在末尾。
// 返回 (新增条数, 合并后域名总数)，统计基于更新前的快照计算。
func (r *repository) MergeScopeDomains(ctx context.Context, scopeID string, domains []string) (int64, int64, error) {
	oid, err := primitive.ObjectIDFromHex(scopeID)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: invalid scope id", ErrInvalidID)
	}
	cleaned := make([]string, 0, len(domains))
	seen := make(map[string]struct{}, len(domains))
	for _, d := range domains {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if _, ok := seen[d]; ok {
			continue
		}
		seen[d] = struct{}{}
		cleaned = append(cleaned, d)
	}
	if len(cleaned) == 0 {
		return 0, 0, nil
	}

	var current models.AssetScope
	if err := r.scopes().FindOne(ctx, bson.M{"_id": oid}).Decode(&current); err != nil {
		if err == mongo.ErrNoDocuments {
			return 0, 0, fmt.Errorf("asset scope not found: %s", scopeID)
		}
		return 0, 0, fmt.Errorf("failed to load asset scope: %w", err)
	}
	existing := make(map[string]struct{}, len(current.DomainArray)+len(cleaned))
	for _, d := range current.DomainArray {
		existing[d] = struct{}{}
	}
	var added int64
	for _, d := range cleaned {
		if _, ok := existing[d]; !ok {
			added++
			existing[d] = struct{}{}
		}
	}

	if _, err := r.scopes().UpdateOne(ctx, bson.M{"_id": oid}, bson.M{
		"$addToSet": bson.M{
			"domain_array": bson.M{"$each": cleaned},
			"scope_array":  bson.M{"$each": cleaned},
		},
		"$set": bson.M{"updated_at": time.Now()},
	}); err != nil {
		return 0, 0, fmt.Errorf("failed to merge scope domains: %w", err)
	}
	return added, int64(len(existing)), nil
}

func toObjectIDs(ids []string) ([]primitive.ObjectID, error) {
	result := make([]primitive.ObjectID, 0, len(ids))
	for _, id := range ids {
		oid, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			return nil, fmt.Errorf("%w: %q", ErrInvalidID, id)
		}
		result = append(result, oid)
	}
	return result, nil
}
