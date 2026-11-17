package user

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Autumn-27/ScopeSentry/internal/config"
	"github.com/Autumn-27/ScopeSentry/internal/constants"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/repositories/user"
	"github.com/Autumn-27/ScopeSentry/internal/utils/helper"
	"github.com/golang-jwt/jwt/v5"
	"go.mongodb.org/mongo-driver/bson"
	mongodriver "go.mongodb.org/mongo-driver/mongo"
)

// 用户业务层（对标 ARL-Next user.py）。
//
// 与历史实现的差别：
//   - 密码一律 SHA256（与 Login 校验同一算法）。历史 Register 用 bcrypt 写入，
//     导致经注册创建的用户永远登录不上，这里统一到 SHA256；
//   - 角色必须在 constants.RolePermissions 里，防止写进一个「不认识 = 无任何权限」的角色；
//   - 用户名 / 邮箱唯一：用户名有唯一索引兜底并发，邮箱靠预检查 + 重复键兜底
//     （历史库里可能已有共用同一邮箱的账号，故不给 email 建唯一索引）；
//   - 不能删除自己，也不能删除 / 降级 / 停用最后一个 super_admin：这三种操作都会让系统
//     失去唯一的用户管理入口。
var (
	ErrUserNotFound     = errors.New("user not found")
	ErrInvalidPassword  = errors.New("invalid password")
	ErrUsernameExists   = errors.New("username already exists")
	ErrEmailExists      = errors.New("email already exists")
	ErrDuplicateUser    = errors.New("username or email already exists")
	ErrInvalidUserData  = errors.New("invalid user data")
	ErrUserDisabled     = errors.New("user disabled")
	ErrInvalidRole      = errors.New("invalid role")
	ErrInvalidStatus    = errors.New("invalid status")
	ErrWeakPassword     = errors.New("password too short")
	ErrCannotDeleteSelf = errors.New("cannot delete yourself")
	ErrLastSuperAdmin   = errors.New("the last super admin cannot be deleted, demoted or disabled")

	// ErrInvalidID 非法 ObjectID（handler 转 400）
	ErrInvalidID = user.ErrInvalidID
)

type Service interface {
	Login(ctx context.Context, username, password string) (string, error)
	// CreateUser 创建用户（注册语义）：校验角色 / 密码长度 / 用户名邮箱唯一
	CreateUser(ctx context.Context, req *models.UserCreate) (*models.User, error)
	// ListUsers 分页列表（keyword 匹配用户名/邮箱，role/status 精确过滤）
	ListUsers(ctx context.Context, query models.UserListQuery) ([]*models.User, int64, error)
	GetUserByID(ctx context.Context, id string) (*models.User, error)
	// UpdateUser 更新 email / role / status，可选改密码
	UpdateUser(ctx context.Context, id string, req *models.UserUpdate) (*models.User, error)
	// DeleteUser 删除用户；actorID 是当前登录用户，用于「不能删除自己」
	DeleteUser(ctx context.Context, id, actorID string) error
	ChangePassword(ctx context.Context, id string, newPassword string) error
}

type service struct {
	userRepo user.Repository
}

func NewService() Service {
	return &service{
		userRepo: user.NewRepository(),
	}
}

func (s *service) Login(ctx context.Context, username, password string) (string, error) {
	user, err := s.userRepo.FindByUsername(ctx, strings.TrimSpace(username))
	if err != nil {
		return "", err
	}
	if user == nil {
		return "", ErrUserNotFound
	}
	if user.Status == models.UserStatusDisabled {
		return "", ErrUserDisabled
	}

	hashPwd := helper.Sha256Hex(password)
	if subtle.ConstantTimeCompare([]byte(hashPwd), []byte(user.Password)) != 1 {
		return "", ErrInvalidPassword
	}

	// 生成JWT token
	now := time.Now()
	expire := now.Add(config.GlobalConfig.JWT.Expire)

	claims := jwt.MapClaims{
		"userID":   user.ID.Hex(),
		"username": user.Username,
		"role":     user.Role,
		"exp":      expire.Unix(),
		"iat":      now.Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(config.GlobalConfig.JWT.Secret))
	if err != nil {
		return "", fmt.Errorf("failed to generate token: %v", err)
	}

	return tokenString, nil
}

// CreateUser 创建用户
func (s *service) CreateUser(ctx context.Context, req *models.UserCreate) (*models.User, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: empty request", ErrInvalidUserData)
	}
	username := strings.TrimSpace(req.Username)
	email := strings.TrimSpace(req.Email)
	role := strings.TrimSpace(req.Role)
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = models.UserStatusActive
	}

	if username == "" || email == "" {
		return nil, fmt.Errorf("%w: username and email are required", ErrInvalidUserData)
	}
	if utf8.RuneCountInString(username) > models.UserMaxUsernameLength {
		return nil, fmt.Errorf("%w: username too long", ErrInvalidUserData)
	}
	if !ValidEmail(email) {
		return nil, fmt.Errorf("%w: invalid email", ErrInvalidUserData)
	}
	if err := ValidateRole(role); err != nil {
		return nil, err
	}
	if err := ValidateStatus(status); err != nil {
		return nil, err
	}
	if err := ValidatePassword(req.Password); err != nil {
		return nil, err
	}

	existing, err := s.userRepo.FindByUsername(ctx, username)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrUsernameExists
	}
	existing, err = s.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrEmailExists
	}

	record := &models.User{
		Username: username,
		Password: helper.Sha256Hex(req.Password),
		Email:    email,
		Role:     role,
		Status:   status,
	}
	if err := s.userRepo.Create(ctx, record); err != nil {
		// 并发创建时预检查会同时通过，真正兜底的是唯一索引
		if user.IsDuplicateKey(err) {
			return nil, ErrDuplicateUser
		}
		return nil, err
	}
	return record, nil
}

// ListUsers 分页列表
func (s *service) ListUsers(ctx context.Context, query models.UserListQuery) ([]*models.User, int64, error) {
	pageIndex := query.PageIndex
	if pageIndex <= 0 {
		pageIndex = query.Page
	}
	pageSize := query.PageSize
	if pageSize <= 0 {
		pageSize = query.Size
	}
	if pageIndex <= 0 {
		pageIndex = 1
	}
	if pageSize <= 0 {
		pageSize = models.UserDefaultPageSize
	}
	return s.userRepo.ListPage(ctx,
		strings.TrimSpace(query.Keyword),
		strings.TrimSpace(query.Role),
		strings.TrimSpace(query.Status),
		pageIndex, pageSize,
	)
}

func (s *service) GetUserByID(ctx context.Context, id string) (*models.User, error) {
	return s.userRepo.FindByID(ctx, id)
}

// UpdateUser 更新用户
func (s *service) UpdateUser(ctx context.Context, id string, req *models.UserUpdate) (*models.User, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: empty request", ErrInvalidUserData)
	}
	target, err := s.userRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, ErrUserNotFound
	}

	fields := bson.M{"updated_at": time.Now()}

	if email := strings.TrimSpace(req.Email); email != "" && email != target.Email {
		if !ValidEmail(email) {
			return nil, fmt.Errorf("%w: invalid email", ErrInvalidUserData)
		}
		other, err := s.userRepo.FindByEmail(ctx, email)
		if err != nil {
			return nil, err
		}
		if other != nil && other.ID != target.ID {
			return nil, ErrEmailExists
		}
		fields["email"] = email
	}

	if role := strings.TrimSpace(req.Role); role != "" && role != target.Role {
		if err := ValidateRole(role); err != nil {
			return nil, err
		}
		// 降级最后一个超管：系统会失去唯一的用户管理入口
		if target.Role == constants.RoleSuperAdmin && role != constants.RoleSuperAdmin {
			count, err := s.userRepo.CountByRole(ctx, constants.RoleSuperAdmin)
			if err != nil {
				return nil, err
			}
			if count <= 1 {
				return nil, ErrLastSuperAdmin
			}
		}
		fields["role"] = role
	}

	if status := strings.TrimSpace(req.Status); status != "" && status != target.Status {
		if err := ValidateStatus(status); err != nil {
			return nil, err
		}
		// 停用唯一超管等于把系统锁死（再没有人能管理用户），与降级同等对待
		if target.Role == constants.RoleSuperAdmin && status == models.UserStatusDisabled {
			count, err := s.userRepo.CountByRole(ctx, constants.RoleSuperAdmin)
			if err != nil {
				return nil, err
			}
			if count <= 1 {
				return nil, ErrLastSuperAdmin
			}
		}
		fields["status"] = status
	}

	if req.Password != "" {
		if err := ValidatePassword(req.Password); err != nil {
			return nil, err
		}
		fields["password"] = helper.Sha256Hex(req.Password)
	}

	// 只有 updated_at 说明没有任何字段要改，直接返回现状，不打库
	if len(fields) == 1 {
		return target, nil
	}

	if err := s.userRepo.UpdateFields(ctx, id, fields); err != nil {
		switch {
		case errors.Is(err, mongodriver.ErrNoDocuments):
			return nil, ErrUserNotFound
		case user.IsDuplicateKey(err):
			return nil, ErrDuplicateUser
		default:
			return nil, err
		}
	}
	return s.userRepo.FindByID(ctx, id)
}

// DeleteUser 删除用户
func (s *service) DeleteUser(ctx context.Context, id, actorID string) error {
	target, err := s.userRepo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if target == nil {
		return ErrUserNotFound
	}
	if actorID != "" && target.ID.Hex() == actorID {
		return ErrCannotDeleteSelf
	}
	if target.Role == constants.RoleSuperAdmin {
		count, err := s.userRepo.CountByRole(ctx, constants.RoleSuperAdmin)
		if err != nil {
			return err
		}
		if count <= 1 {
			return ErrLastSuperAdmin
		}
	}

	if err := s.userRepo.Delete(ctx, id); err != nil {
		if errors.Is(err, mongodriver.ErrNoDocuments) {
			return ErrUserNotFound
		}
		return err
	}
	return nil
}

// ChangePassword 仅更新密码字段，使用与登录一致的 SHA256 存储
func (s *service) ChangePassword(ctx context.Context, id string, newPassword string) error {
	if newPassword == "" {
		return ErrInvalidUserData
	}
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	fields := bson.M{
		"password":   helper.Sha256Hex(newPassword),
		"updated_at": time.Now(),
	}
	if err := s.userRepo.UpdateFields(ctx, id, fields); err != nil {
		if errors.Is(err, mongodriver.ErrNoDocuments) {
			return ErrUserNotFound
		}
		return err
	}
	return nil
}

// ValidateRole 角色必须存在于 constants.RolePermissions
func ValidateRole(role string) error {
	if role == "" {
		return fmt.Errorf("%w: role is required", ErrInvalidRole)
	}
	if _, ok := constants.RolePermissions[role]; !ok {
		return fmt.Errorf("%w: %q", ErrInvalidRole, role)
	}
	return nil
}

// ValidateStatus 状态只允许 active / disabled
func ValidateStatus(status string) error {
	switch status {
	case models.UserStatusActive, models.UserStatusDisabled:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrInvalidStatus, status)
	}
}

// ValidatePassword 密码长度校验（创建 / 重置 / 自助改密共用同一口径）
func ValidatePassword(password string) error {
	length := utf8.RuneCountInString(password)
	if length < models.UserMinPasswordLength {
		return fmt.Errorf("%w: at least %d characters", ErrWeakPassword, models.UserMinPasswordLength)
	}
	if length > models.UserMaxPasswordLength {
		return fmt.Errorf("%w: at most %d characters", ErrInvalidUserData, models.UserMaxPasswordLength)
	}
	return nil
}

// ValidEmail 邮箱格式校验（与 gin binding 的 email 校验同向，服务层直接被调用时也生效）
func ValidEmail(email string) bool {
	email = strings.TrimSpace(email)
	if email == "" || len(email) > 254 || !strings.Contains(email, "@") {
		return false
	}
	addr, err := mail.ParseAddress(email)
	return err == nil && addr.Address == email
}
