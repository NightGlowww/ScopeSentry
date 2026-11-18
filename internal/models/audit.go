package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// AuditLog 服务端操作日志：只记录「写」请求（POST/PUT/DELETE/PATCH），
// 用于回答「谁在什么时候改了什么」，与节点日志、MCP 调用日志分开。
type AuditLog struct {
	ID         primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	User       string             `bson:"user" json:"user"`
	Method     string             `bson:"method" json:"method"`
	Path       string             `bson:"path" json:"path"`
	Status     int                `bson:"status" json:"status"`
	DurationMS int64              `bson:"durationMs" json:"durationMs"`
	IP         string             `bson:"ip" json:"ip"`
	UA         string             `bson:"ua,omitempty" json:"ua,omitempty"`
	At         time.Time          `bson:"at" json:"at"`
}

// AuditLogQuery 操作日志查询条件
type AuditLogQuery struct {
	User      string `json:"user"`
	Path      string `json:"path"`
	Method    string `json:"method"`
	StatusMin int    `json:"statusMin"`
	PageIndex int    `json:"pageIndex"`
	PageSize  int    `json:"pageSize"`
}
