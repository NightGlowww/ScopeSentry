package user

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	mongodriver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const collectionName = "user"

// ErrInvalidID 主键不是合法的 ObjectID 十六进制串（handler 转 400）。
//
// 历史实现直接把字符串塞进 bson.M{"_id": id} 去比对 ObjectID 字段，永远匹配不上，
// 删除/详情永远「查不到」。这里统一在仓库层做转换，并把非法 id 显式暴露出来。
var ErrInvalidID = errors.New("invalid user id")

type Repository interface {
	// FindByUsername 按用户名取用户；不存在返回 (nil, nil)，让业务层区分「没这个人」和「查询失败」
	FindByUsername(ctx context.Context, username string) (*models.User, error)
	// FindByEmail 按邮箱取用户；不存在返回 (nil, nil)
	FindByEmail(ctx context.Context, email string) (*models.User, error)
	Create(ctx context.Context, user *models.User) error
	FindAll(ctx context.Context) ([]*models.User, error)
	// ListPage 分页列表：keyword 模糊匹配 username / email，role、status 精确过滤
	ListPage(ctx context.Context, keyword, role, status string, pageIndex, pageSize int) ([]*models.User, int64, error)
	// FindByID 按主键取用户；不存在返回 (nil, nil)，非法 id 返回 ErrInvalidID
	FindByID(ctx context.Context, id string) (*models.User, error)
	Update(ctx context.Context, id string, user *models.User) error
	UpdateFields(ctx context.Context, id string, fields bson.M) error
	Delete(ctx context.Context, id string) error
	// CountByRole 统计某角色的人数（最后一个 super_admin 保护用）
	CountByRole(ctx context.Context, role string) (int64, error)
}

type repository struct {
	collection *mongodriver.Collection
}

func NewRepository() Repository {
	return &repository{
		collection: mongodb.CollectionOrNil(collectionName),
	}
}

// parseID 把路径里的十六进制串转成 ObjectID；非法值返回 ErrInvalidID
func parseID(id string) (primitive.ObjectID, error) {
	objectID, err := primitive.ObjectIDFromHex(strings.TrimSpace(id))
	if err != nil {
		return primitive.NilObjectID, ErrInvalidID
	}
	return objectID, nil
}

// IsDuplicateKey 判断写入是否撞了唯一索引（username 有唯一索引；邮箱重复通常由业务层
// 预检查先拦下，并发写入时这里作为最后一道兜底）
func IsDuplicateKey(err error) bool {
	return mongodriver.IsDuplicateKeyError(err)
}

// Create 创建用户
func (r *repository) Create(ctx context.Context, user *models.User) error {
	if user.ID.IsZero() {
		user.ID = primitive.NewObjectID()
	}
	now := time.Now()
	if user.CreatedAt.IsZero() {
		user.CreatedAt = now
	}
	user.UpdatedAt = now

	_, err := r.collection.InsertOne(ctx, user)
	return err
}

// FindByID 根据ID获取用户
func (r *repository) FindByID(ctx context.Context, id string) (*models.User, error) {
	objectID, err := parseID(id)
	if err != nil {
		return nil, err
	}
	var user models.User
	if err := r.collection.FindOne(ctx, bson.M{"_id": objectID}).Decode(&user); err != nil {
		if errors.Is(err, mongodriver.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

// FindByUsername 根据用户名获取用户
func (r *repository) FindByUsername(ctx context.Context, username string) (*models.User, error) {
	var user models.User
	if err := r.collection.FindOne(ctx, bson.M{"username": username}).Decode(&user); err != nil {
		if errors.Is(err, mongodriver.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

// FindByEmail 根据邮箱获取用户（创建 / 改邮箱时的唯一性检查）
func (r *repository) FindByEmail(ctx context.Context, email string) (*models.User, error) {
	if strings.TrimSpace(email) == "" {
		return nil, nil
	}
	var user models.User
	if err := r.collection.FindOne(ctx, bson.M{"email": email}).Decode(&user); err != nil {
		if errors.Is(err, mongodriver.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

// FindAll 获取用户列表（全量，保留给内部/脚本使用；分页请用 ListPage）
func (r *repository) FindAll(ctx context.Context) ([]*models.User, error) {
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}})

	cursor, err := r.collection.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var users []*models.User
	if err = cursor.All(ctx, &users); err != nil {
		return nil, err
	}

	return users, nil
}

// ListPage 分页查询，走 user.created_at 索引排序，避免全量拉回内存再切片
func (r *repository) ListPage(ctx context.Context, keyword, role, status string, pageIndex, pageSize int) ([]*models.User, int64, error) {
	if pageIndex < 1 {
		pageIndex = 1
	}
	if pageSize <= 0 {
		pageSize = models.UserDefaultPageSize
	}
	// handler 已经对 >200 返回 400；这里再夹一次，防止内部调用绕过校验
	if pageSize > models.UserMaxPageSize {
		pageSize = models.UserMaxPageSize
	}

	filter := bson.M{}
	if role != "" {
		filter["role"] = role
	}
	if status != "" {
		filter["status"] = status
	}
	if keyword != "" {
		// QuoteMeta：关键字里的正则元字符按字面量处理，避免 ( 之类直接 500
		pattern := bson.M{"$regex": regexp.QuoteMeta(keyword), "$options": "i"}
		filter["$and"] = bson.A{
			bson.M{"$or": bson.A{
				bson.M{"username": pattern},
				bson.M{"email": pattern},
			}},
		}
	}

	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	opts := options.Find().
		SetSkip(int64((pageIndex - 1) * pageSize)).
		SetLimit(int64(pageSize)).
		// _id 兜底：同一毫秒创建的多条记录排序仍然稳定
		SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}})
	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var users []*models.User
	if err := cursor.All(ctx, &users); err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

// Update 全字段更新（只写明确列出的字段，避免把 _id 塞进 $set 触发 immutable field 错误）
func (r *repository) Update(ctx context.Context, id string, user *models.User) error {
	objectID, err := parseID(id)
	if err != nil {
		return err
	}
	set := bson.M{
		"username":   user.Username,
		"email":      user.Email,
		"role":       user.Role,
		"status":     user.Status,
		"updated_at": time.Now(),
	}
	if user.Password != "" {
		set["password"] = user.Password
	}
	res, err := r.collection.UpdateOne(ctx, bson.M{"_id": objectID}, bson.M{"$set": set})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongodriver.ErrNoDocuments
	}
	return nil
}

// UpdateFields 按字段更新（仅更新传入字段，避免零值覆盖）
func (r *repository) UpdateFields(ctx context.Context, id string, fields bson.M) error {
	objectID, err := parseID(id)
	if err != nil {
		return err
	}
	res, err := r.collection.UpdateOne(ctx, bson.M{"_id": objectID}, bson.M{"$set": fields})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongodriver.ErrNoDocuments
	}
	return nil
}

// Delete 删除用户
func (r *repository) Delete(ctx context.Context, id string) error {
	objectID, err := parseID(id)
	if err != nil {
		return err
	}
	res, err := r.collection.DeleteOne(ctx, bson.M{"_id": objectID})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return mongodriver.ErrNoDocuments
	}
	return nil
}

// CountByRole 统计角色人数
func (r *repository) CountByRole(ctx context.Context, role string) (int64, error) {
	return r.collection.CountDocuments(ctx, bson.M{"role": role})
}
