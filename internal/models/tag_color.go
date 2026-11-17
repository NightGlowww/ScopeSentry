package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// TagColor 标签的固定颜色覆盖（issue #217）。
//
// 前端默认按标签名稳定取色；用户给某个标签指定固定颜色后写在这里，
// 前端加载一次即可让所有页面统一。color 为空表示删除覆盖（落库时直接删行）。
type TagColor struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"_id"`
	Tag       string             `bson:"tag" json:"tag"`
	Color     string             `bson:"color" json:"color"`
	UpdatedAt time.Time          `bson:"updatedAt" json:"updatedAt"`
}

// TagColorRequest 设置/清除标签颜色
type TagColorRequest struct {
	Tag   string `json:"tag" binding:"required"`
	Color string `json:"color"`
}
