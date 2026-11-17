package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// AssetScope 资产组「地契」：一个集团下的一段资产范围（对标 ARL-Next asset_scope）。
//
// Scope 是用户填写的原始文本（换行/逗号分隔），解析后落成 ScopeArray / DomainArray / IPArray，
// 后续资产匹配、ICP 回填、覆盖度统计都读解析后的数组。
type AssetScope struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Name         string             `bson:"name" json:"name"`
	Scope        string             `bson:"scope" json:"scope"`
	ScopeType    string             `bson:"scope_type" json:"scopeType"` // domain | ip | mixed
	GroupID      string             `bson:"group_id" json:"groupId"`
	ScopeArray   []string           `bson:"scope_array" json:"scopeArray"`
	DomainArray  []string           `bson:"domain_array" json:"domainArray"`
	IPArray      []string           `bson:"ip_array" json:"ipArray"`
	Enterprise   string             `bson:"enterprise" json:"enterprise"`
	UpdateError  string             `bson:"update_error" json:"updateError"`
	ICPUpdatedAt *time.Time         `bson:"icp_updated_at,omitempty" json:"icpUpdatedAt,omitempty"`
	CreatedAt    time.Time          `bson:"created_at" json:"createdAt"`
	UpdatedAt    time.Time          `bson:"updated_at" json:"updatedAt"`
}

// AssetScopeAddRequest 新建地契
type AssetScopeAddRequest struct {
	Name      string `json:"name" binding:"required"`
	Scope     string `json:"scope" binding:"required"`
	ScopeType string `json:"scopeType"`
	GroupID   string `json:"groupId"`
}

// AssetScopeUpdateRequest 编辑地契
type AssetScopeUpdateRequest struct {
	ID        string `json:"id" binding:"required"`
	Name      string `json:"name"`
	Scope     string `json:"scope"`
	ScopeType string `json:"scopeType"`
	GroupID   string `json:"groupId"`
}

// AssetScopeIDsRequest 批量操作（删除/移动/清理错误）
type AssetScopeIDsRequest struct {
	IDs []string `json:"ids" binding:"required"`
}

// AssetScopeMoveRequest 批量移动地契到目标集团（groupId 为空表示移入未分组）
type AssetScopeMoveRequest struct {
	IDs     []string `json:"ids" binding:"required"`
	GroupID string   `json:"groupId"`
}

// AssetScopeBindEnterpriseRequest 绑定企业主体
type AssetScopeBindEnterpriseRequest struct {
	ID         string `json:"id" binding:"required"`
	Enterprise string `json:"enterprise"`
}
