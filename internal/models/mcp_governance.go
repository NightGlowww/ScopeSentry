package models

import (
	"encoding/json"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// MCP 治理面（对标 ARL-Next backend/app/routes/mcp.py 的 keys / logs / config 三段）。
//
// 设计要点：
//  1. 认证主体仍是 api_key 集合（不新建 key 集合），只在原文档上扩治理字段；
//  2. 调用审计落 mcp_call_logs，异步批量写，主请求不等 Mongo；
//  3. 全局开关与工具白名单落 mcp_config 单例文档（_id = "global"）。

const (
	// McpConfigGlobalID mcp_config 单例文档主键
	McpConfigGlobalID = "global"
	// McpDefaultLogRetention 日志保留条数默认上限（10 万条）
	McpDefaultLogRetention int64 = 100000
	// McpMaxPageSize 列表接口分页上限，超过直接 400
	McpMaxPageSize = 200
)

// MCP 调用状态（写进 mcp_call_logs.status，也是日志筛选的取值）
const (
	McpLogStatusSuccess = "success"
	McpLogStatusDenied  = "denied"
	McpLogStatusError   = "error"
)

// StringList 字符串列表，接受 "a" 与 ["a","b"] 两种 JSON 写法。
// ARL-Next 的 scope 字段就是这种「单值或数组」形态，前端两种都会传。
type StringList []string

// UnmarshalJSON 兼容字符串与字符串数组
func (s *StringList) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*s = nil
		return nil
	}
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*s = NormalizeStringList([]string{single})
		return nil
	}
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	*s = NormalizeStringList(list)
	return nil
}

// MarshalJSON 空列表输出 []，避免前端拿到 null
func (s StringList) MarshalJSON() ([]byte, error) {
	if s == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]string(s))
}

// NormalizeStringList 去空白、去空项、保序去重
func NormalizeStringList(in []string) StringList {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return StringList(out)
}

// McpCallLog 一次 MCP 调用审计记录
type McpCallLog struct {
	ID         primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	KeyID      string             `bson:"keyId" json:"keyId"`
	KeyPrefix  string             `bson:"keyPrefix" json:"keyPrefix"`
	Tool       string             `bson:"tool" json:"tool"`
	Status     string             `bson:"status" json:"status"`
	DurationMS int64              `bson:"durationMs" json:"durationMs"`
	ClientIP   string             `bson:"clientIp" json:"clientIp"`
	Error      string             `bson:"error,omitempty" json:"error,omitempty"`
	CreatedAt  time.Time          `bson:"createdAt" json:"createdAt"`
}

// McpConfig MCP 全局开关（单例文档）
type McpConfig struct {
	ID              string     `bson:"_id" json:"-"`
	Enabled         bool       `bson:"enabled" json:"enabled"`
	AllowedTools    StringList `bson:"allowedTools" json:"allowedTools"`
	RateLimitPerMin int        `bson:"rateLimitPerMin" json:"rateLimitPerMin"`
	LogRetention    int64      `bson:"logRetention" json:"logRetention"`
	UpdatedAt       time.Time  `bson:"updatedAt" json:"updatedAt"`
	UpdatedBy       string     `bson:"updatedBy" json:"updatedBy"`
}

// DefaultMcpConfig 默认配置：开启、不限制工具、不限速、保留 10 万条日志
func DefaultMcpConfig() *McpConfig {
	return &McpConfig{
		ID:              McpConfigGlobalID,
		Enabled:         true,
		AllowedTools:    nil,
		RateLimitPerMin: 0,
		LogRetention:    McpDefaultLogRetention,
	}
}

// Normalize 补齐零值：老文档没有 logRetention 时回落到默认值
func (c *McpConfig) Normalize() {
	if c == nil {
		return
	}
	c.ID = McpConfigGlobalID
	if c.LogRetention <= 0 {
		c.LogRetention = McpDefaultLogRetention
	}
	if c.RateLimitPerMin < 0 {
		c.RateLimitPerMin = 0
	}
	c.AllowedTools = NormalizeStringList(c.AllowedTools)
}

// McpKeyCreateRequest 新建 MCP Key（明文 key 只在响应里返回一次）
type McpKeyCreateRequest struct {
	Name            string     `json:"name" binding:"required"`
	Scope           StringList `json:"scope"`
	Tools           StringList `json:"tools"`
	RateLimitPerMin int        `json:"rateLimitPerMin"`
	// ExpiresAt RFC3339 或 "2006-01-02 15:04:05"，留空表示长期有效
	ExpiresAt string `json:"expiresAt"`
}

// McpKeyUpdateRequest 编辑 MCP Key 的治理字段（name / scope / tools / 限速 / 过期）
type McpKeyUpdateRequest struct {
	Name            string     `json:"name"`
	Scope           StringList `json:"scope"`
	Tools           StringList `json:"tools"`
	RateLimitPerMin int        `json:"rateLimitPerMin"`
	ExpiresAt       string     `json:"expiresAt"`
}

// McpKeyStatusRequest 启用/禁用
type McpKeyStatusRequest struct {
	Enabled *bool `json:"enabled" binding:"required"`
}

// McpKeyListQuery Key 列表查询
type McpKeyListQuery struct {
	Keyword   string `form:"keyword"`
	PageIndex int    `form:"pageIndex"`
	PageSize  int    `form:"pageSize"`
}

// McpLogQuery 调用日志查询
type McpLogQuery struct {
	KeyID     string     `form:"keyId"`
	Tool      string     `form:"tool"`
	Status    string     `form:"status"`
	From      *time.Time `form:"-"`
	To        *time.Time `form:"-"`
	PageIndex int        `form:"pageIndex"`
	PageSize  int        `form:"pageSize"`
}

// McpConfigUpdateRequest 更新全局开关；指针字段用于区分「没传」与「传了零值」
// （AllowedTools 用指针：[] = 清空白名单 = 允许全部）
type McpConfigUpdateRequest struct {
	Enabled         *bool       `json:"enabled"`
	AllowedTools    *StringList `json:"allowedTools"`
	RateLimitPerMin *int        `json:"rateLimitPerMin"`
	LogRetention    *int64      `json:"logRetention"`
}
