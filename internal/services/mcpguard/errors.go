package mcpguard

import "errors"

var (
	// ErrMCPDisabled 全局开关关闭
	ErrMCPDisabled = errors.New("mcp is disabled")
	// ErrInvalidKey key 不存在/格式非法
	ErrInvalidKey = errors.New("invalid api key")
	// ErrKeyDisabled key 已被禁用
	ErrKeyDisabled = errors.New("api key disabled")
	// ErrKeyExpired key 已过期
	ErrKeyExpired = errors.New("api key expired")
	// ErrToolNotAllowed 工具不在白名单（或被全局白名单挡住/未登记）
	ErrToolNotAllowed = errors.New("tool not allowed")
	// ErrScopeDenied scope 不足
	ErrScopeDenied = errors.New("insufficient scope")
	// ErrRateLimited 触发限速
	ErrRateLimited = errors.New("rate limit exceeded")
	// ErrInvalidConfig 配置入参非法
	ErrInvalidConfig = errors.New("invalid mcp config")
)
