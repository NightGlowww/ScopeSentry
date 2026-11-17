package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ApiKey API Key 模型，用于 MCP 等外部集成认证。
//
// MCP 治理扩展（对标 ARL-Next mcp keys）：
//   - Scope          允许的权限面，如 asset:read / task:write / *；空 = 历史 key，等同 *
//   - Tools          允许调用的 MCP 工具名白名单；空 = 全部
//   - RateLimitPerMin 每分钟请求上限；0 = 跟随全局配置（全局也为 0 则不限速）
//   - ExpiresAt      过期时间；nil = 长期有效
type ApiKey struct {
	ID              primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Name            string             `bson:"name" json:"name"`
	KeyHash         string             `bson:"keyHash" json:"-"`
	KeyPrefix       string             `bson:"keyPrefix" json:"keyPrefix"`
	Enabled         bool               `bson:"enabled" json:"enabled"`
	Scope           StringList         `bson:"scope" json:"scope"`
	Tools           StringList         `bson:"tools" json:"tools"`
	RateLimitPerMin int                `bson:"rateLimitPerMin" json:"rateLimitPerMin"`
	ExpiresAt       *time.Time         `bson:"expires_at,omitempty" json:"expiresAt,omitempty"`
	CreatedBy       string             `bson:"createdBy" json:"createdBy"`
	CreatedAt       time.Time          `bson:"createdAt" json:"createdAt"`
	LastUsedAt      *time.Time         `bson:"lastUsedAt,omitempty" json:"lastUsedAt,omitempty"`
}

// IsExpired 判断 key 是否已过期（nil 视为长期有效）
func (k *ApiKey) IsExpired(now time.Time) bool {
	if k == nil || k.ExpiresAt == nil {
		return false
	}
	return now.After(*k.ExpiresAt)
}

// CreateApiKeyRequest 创建 API Key 请求
type CreateApiKeyRequest struct {
	Name string `json:"name" binding:"required"`
}

// CreateApiKeyResponse 创建 API Key 响应（明文 key 仅返回一次）
type CreateApiKeyResponse struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Key             string     `json:"key"`
	KeyPrefix       string     `json:"keyPrefix"`
	Scope           StringList `json:"scope"`
	Tools           StringList `json:"tools"`
	RateLimitPerMin int        `json:"rateLimitPerMin"`
	ExpiresAt       *time.Time `json:"expiresAt,omitempty"`
	CreatedAt       string     `json:"createdAt"`
}

// DeleteApiKeyRequest 删除 API Key 请求
type DeleteApiKeyRequest struct {
	ID string `json:"id" binding:"required"`
}
