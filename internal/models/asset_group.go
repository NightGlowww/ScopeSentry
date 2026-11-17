package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// AssetGroup 资产集团分组（对标 ARL-Next asset_group 集合）。
//
// 与项目（project）的区别：project 是扫描/资产归属的容器，asset_group 是「企业/集团」维度，
// 一个集团下挂多个 asset_scope（地契），用来描述这个集团覆盖了哪些域名/IP。
type AssetGroup struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Name        string             `bson:"name" json:"name"`
	Description string             `bson:"description" json:"description"`
	SortOrder   int                `bson:"sort_order" json:"sortOrder"`
	CreatedAt   time.Time          `bson:"created_at" json:"createdAt"`
	UpdatedAt   time.Time          `bson:"updated_at" json:"updatedAt"`
	// ScopeCount 只在列表接口里填充：该集团下资产组数量
	ScopeCount int64 `bson:"-" json:"scopeCount"`
}

// AssetGroupAddRequest 新建集团
type AssetGroupAddRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	SortOrder   int    `json:"sortOrder"`
}

// AssetGroupEditRequest 编辑集团
type AssetGroupEditRequest struct {
	ID          string `json:"id" binding:"required"`
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	SortOrder   int    `json:"sortOrder"`
}

// AssetGroupDeleteRequest 删除集团：dissolve 把地契移入未分组，transfer 划转到目标集团
type AssetGroupDeleteRequest struct {
	ID            string `json:"id" binding:"required"`
	Action        string `json:"action"` // dissolve | transfer
	TargetGroupID string `json:"targetGroupId"`
}

// AssetGroupReorderRequest 按给定顺序重排集团
type AssetGroupReorderRequest struct {
	OrderIDs []string `json:"orderIds" binding:"required"`
}
