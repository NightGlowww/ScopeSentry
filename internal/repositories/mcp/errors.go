package mcp

import "errors"

var (
	// ErrConfigUnavailable mcp_config 读写失败（集合不可用/DB 未初始化）
	ErrConfigUnavailable = errors.New("mcp config unavailable")
	// ErrInvalidLogQuery 日志查询参数非法
	ErrInvalidLogQuery = errors.New("invalid mcp log query")
)
