package assetserviceview

import (
	"context"
	"fmt"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Repository 服务维度聚合
type Repository interface {
	List(ctx context.Context, match bson.M, pageIndex, pageSize int) ([]models.AssetServiceRow, int64, error)
}

type repository struct{ db *mongo.Database }

// NewRepository 创建仓库
func NewRepository() Repository { return &repository{db: mongodb.DB} }

// List 按 service 聚合：资产数/主机数/端口数/样例 host/最近时间
func (r *repository) List(ctx context.Context, match bson.M, pageIndex, pageSize int) ([]models.AssetServiceRow, int64, error) {
	if pageIndex < 1 {
		pageIndex = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}
	group := bson.M{
		"_id":        "$service",
		"assetCount": bson.M{"$sum": 1},
		"hosts":      bson.M{"$addToSet": "$host"},
		"ports":      bson.M{"$addToSet": "$port"},
		"lastSeen":   bson.M{"$max": "$time"},
	}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: match}},
		{{Key: "$group", Value: group}},
		{{Key: "$addFields", Value: bson.M{
			"hostCount": bson.M{"$size": "$hosts"},
			"portCount": bson.M{"$size": "$ports"},
			"samples":   bson.M{"$slice": []interface{}{"$hosts", 5}},
		}}},
		{{Key: "$facet", Value: bson.M{
			"total": []bson.M{{"$count": "n"}},
			"rows": []bson.M{
				{"$sort": bson.M{"assetCount": -1, "_id": 1}},
				{"$skip": int64((pageIndex - 1) * pageSize)},
				{"$limit": int64(pageSize)},
			},
		}}},
	}
	cursor, err := r.db.Collection("asset").Aggregate(ctx, pipeline, options.Aggregate().SetAllowDiskUse(true))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to aggregate asset services: %w", err)
	}
	defer cursor.Close(ctx)
	var out struct {
		Total []struct {
			N int64 `bson:"n"`
		} `bson:"total"`
		Rows []models.AssetServiceRow `bson:"rows"`
	}
	if !cursor.Next(ctx) {
		return []models.AssetServiceRow{}, 0, cursor.Err()
	}
	if err := cursor.Decode(&out); err != nil {
		return nil, 0, fmt.Errorf("failed to decode asset services: %w", err)
	}
	var total int64
	if len(out.Total) > 0 {
		total = out.Total[0].N
	}
	rows := out.Rows
	if rows == nil {
		rows = []models.AssetServiceRow{}
	}
	return rows, total, nil
}
