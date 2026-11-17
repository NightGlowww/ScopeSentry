package httpstatus

import (
	"context"
	"fmt"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Repository HTTP 状态视图数据访问
type Repository interface {
	List(ctx context.Context, filter bson.M, pageIndex, pageSize int) ([]models.HTTPStatusRow, int64, error)
}

type repository struct{ db *mongo.Database }

// NewRepository 创建仓库
func NewRepository() Repository { return &repository{db: mongodb.DB} }

// List 分页返回资产状态码视图（按 time 倒序，走 {statuscode,time} / {project,time} 索引）
func (r *repository) List(ctx context.Context, filter bson.M, pageIndex, pageSize int) ([]models.HTTPStatusRow, int64, error) {
	if pageIndex < 1 {
		pageIndex = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}
	coll := r.db.Collection("asset")
	total, err := coll.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count http status: %w", err)
	}
	ops := options.Find().
		SetProjection(bson.M{"host": 1, "port": 1, "statuscode": 1, "title": 1, "url": 1, "service": 1, "project": 1, "time": 1}).
		SetSort(bson.D{{Key: "time", Value: -1}}).
		SetSkip(int64((pageIndex - 1) * pageSize)).
		SetLimit(int64(pageSize))
	cursor, err := coll.Find(ctx, filter, ops)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list http status: %w", err)
	}
	defer cursor.Close(ctx)
	rows := make([]models.HTTPStatusRow, 0, pageSize)
	for cursor.Next(ctx) {
		var doc struct {
			ID         primitive.ObjectID `bson:"_id"`
			Host       string             `bson:"host"`
			Port       string             `bson:"port"`
			StatusCode int                `bson:"statuscode"`
			Title      string             `bson:"title"`
			URL        string             `bson:"url"`
			Service    string             `bson:"service"`
			Project    string             `bson:"project"`
			Time       string             `bson:"time"`
		}
		if err := cursor.Decode(&doc); err != nil {
			return nil, 0, fmt.Errorf("failed to decode http status: %w", err)
		}
		rows = append(rows, models.HTTPStatusRow{
			ID: doc.ID.Hex(), Host: doc.Host, Port: doc.Port, StatusCode: doc.StatusCode,
			Title: doc.Title, URL: doc.URL, Service: doc.Service, Project: doc.Project, Time: doc.Time,
		})
	}
	return rows, total, cursor.Err()
}
