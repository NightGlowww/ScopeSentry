package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 用户管理（对标 ARL-Next backend/app/routes/user.py 的 CRUD 能力）。
//
// 存储约定：
//   - 密码只存 SHA256 十六进制摘要，与登录（user.Service.Login）同一算法；
//   - status 空值按 active 处理，兼容 initdb 建出来的历史种子用户（只有 username/password）。
const (
	// UserMaxPageSize 列表分页上限，超过直接 400（与 ICP / 扫描策略 / MCP 治理面同一口径）
	UserMaxPageSize = 200
	// UserDefaultPageSize 未传 pageSize 时的默认值
	UserDefaultPageSize = 20
	// UserMinPasswordLength 密码最短长度：创建 / 管理员重置 / 用户自助改密共用
	UserMinPasswordLength = 8
	// UserMaxPasswordLength 密码最长长度，挡住把几 MB 字符串塞进 SHA256 的无意义请求
	UserMaxPasswordLength = 256
	// UserMaxUsernameLength 用户名长度上限（字符数）
	UserMaxUsernameLength = 64
)

// 用户状态
const (
	UserStatusActive   = "active"   // 可登录
	UserStatusDisabled = "disabled" // 禁止登录（登录接口直接拒绝）
)

type User struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Username  string             `bson:"username" json:"username"`
	Password  string             `bson:"password" json:"-"`
	Email     string             `bson:"email" json:"email"`
	Role      string             `bson:"role" json:"role"`
	Status    string             `bson:"status" json:"status"`
	CreatedAt time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time          `bson:"updated_at" json:"updated_at"`
}

// UserCreate 创建用户（POST /api/user，等价于上游的注册语义）
type UserCreate struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Role     string `json:"role" binding:"required"`
	// Status 可省略，默认 active
	Status string `json:"status"`
}

// UserUpdate 更新用户（PUT /api/user/:id）
//
// 所有字段都是可选的：只改传了的字段，未传的保持原值。
// Username 仅为兼容旧请求体保留，用户管理不支持改名（username 是登录凭据，改名会让
// 已签发的 JWT 里的 username 与库里不一致），传入会被忽略。
type UserUpdate struct {
	Username string `json:"username"`
	Email    string `json:"email" binding:"omitempty,email"`
	Role     string `json:"role"`
	Status   string `json:"status"`
	// Password 可选：非空时按最短长度校验并重置密码（SHA256 存储）
	Password string `json:"password"`
}

// UserListQuery 列表查询（GET /api/user）
//
// page / size 是历史上被注释掉的分页参数别名，pageIndex / pageSize 缺省时生效。
type UserListQuery struct {
	Keyword   string `form:"keyword" json:"keyword"`
	Role      string `form:"role" json:"role"`
	Status    string `form:"status" json:"status"`
	PageIndex int    `form:"pageIndex" json:"pageIndex"`
	PageSize  int    `form:"pageSize" json:"pageSize"`
	Page      int    `form:"page" json:"page"`
	Size      int    `form:"size" json:"size"`
}

type UserLogin struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}
