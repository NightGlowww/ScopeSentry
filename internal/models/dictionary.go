// models-------------------------------------
// @file      : dictionary.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2025/4/28 20:29
// -------------------------------------------

package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Port 仅用于基础端口键值
type Port struct {
	Name  string `json:"name" bson:"name"`
	Value string `json:"value" bson:"value"`
}

// DictionaryMeta 字典元数据
//
// 兼容旧字段：id/name/category/size 是历史契约，不能改；
// line_count / entries_synced / entry_seq / updated_at 是字典行级化后的增量字段，
// 旧文档读出来是零值，首次行操作时由 backfill 补齐。
type DictionaryMeta struct {
	ID       primitive.ObjectID `json:"id" bson:"_id"`
	Name     string             `json:"name" bson:"name"`
	Category string             `json:"category" bson:"category"`
	Size     string             `json:"size" bson:"size"`

	// LineCount 去重后的行数（行级集合的权威计数）
	LineCount int64 `json:"lineCount" bson:"line_count"`
	// EntriesSynced 行级集合是否已与 GridFS 内容对齐
	EntriesSynced bool `json:"entriesSynced" bson:"entries_synced"`
	// EntrySeq 自增序号游标，保证行序与写入顺序一致
	EntrySeq int64 `json:"-" bson:"entry_seq"`
	// UpdatedAt 最后写入时间，列表按它倒序
	UpdatedAt time.Time `json:"updatedAt" bson:"updated_at"`
}

// PortDoc 带有ID的端口字典文档
type PortDoc struct {
	ID    primitive.ObjectID `json:"id" bson:"_id"`
	Name  string             `json:"name" bson:"name"`
	Value string             `json:"value" bson:"value"`
}

type UpdatePort struct {
	ID    string `json:"id"`
	Name  string `json:"name" bson:"name"`
	Value string `json:"value" bson:"value"`
}
