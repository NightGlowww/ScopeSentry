package wih

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Repository WIH 敏感信息读模型仓库（对标 ARL-Next assetWih）。
//
// 读的是现成的 SensitiveResult 集合（敏感规则命中结果），只读，不改写入链路。
// 字段映射：type -> sid（敏感规则名）、match -> 命中片段、url -> 命中的 URL。
type Repository interface {
	// List 分页列表（按 time 倒序），返回列表与命中总数
	List(ctx context.Context, filter bson.M, pageIndex, pageSize int) ([]models.WIHRow, int64, error)
	// FindByID 单条记录；不存在时返回 (nil, nil)
	FindByID(ctx context.Context, id primitive.ObjectID) (*models.WIHDetail, error)
	// ExplainList 列表查询的 explain（queryPlanner），用于验证走索引
	ExplainList(ctx context.Context, filter bson.M, pageIndex, pageSize int) (bson.M, error)
}

type repository struct {
	db   *mongo.Database
	coll *mongo.Collection
}

// NewRepository 创建 WIH 读模型仓库实例
func NewRepository() Repository {
	return &repository{
		db:   mongodb.DB,
		coll: mongodb.DB.Collection("SensitiveResult"),
	}
}

// wihDoc SensitiveResult 集合的解码结构
type wihDoc struct {
	ID         primitive.ObjectID `bson:"_id"`
	URL        string             `bson:"url"`
	SID        string             `bson:"sid"`
	Match      []string           `bson:"match"`
	Color      string             `bson:"color"`
	Tags       []string           `bson:"tags"`
	Status     int                `bson:"status"`
	Project    string             `bson:"project"`
	RootDomain string             `bson:"rootDomain"`
	TaskName   string             `bson:"taskName"`
	Time       string             `bson:"time"`
	MD5        string             `bson:"md5"`
}

// listProjection 列表字段：正文（SensitiveBody）不在集合里，按 md5 另取
var listProjection = bson.D{
	{Key: "url", Value: 1},
	{Key: "sid", Value: 1},
	{Key: "match", Value: 1},
	{Key: "color", Value: 1},
	{Key: "tags", Value: 1},
	{Key: "status", Value: 1},
	{Key: "project", Value: 1},
	{Key: "rootDomain", Value: 1},
	{Key: "taskName", Value: 1},
	{Key: "time", Value: 1},
	{Key: "md5", Value: 1},
}

// listSort 按 time 倒序，索引 {project:1,time:-1} / {sid:1,time:-1} 可直接提供顺序
var listSort = bson.D{{Key: "time", Value: -1}}

func listFindOptions(pageIndex, pageSize int) *options.FindOptions {
	return options.Find().
		SetProjection(listProjection).
		SetSort(listSort).
		SetSkip(int64((pageIndex - 1) * pageSize)).
		SetLimit(int64(pageSize))
}

// List 分页列表。
func (r *repository) List(ctx context.Context, filter bson.M, pageIndex, pageSize int) ([]models.WIHRow, int64, error) {
	total, err := r.coll.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count wih results: %w", err)
	}
	if total == 0 {
		return []models.WIHRow{}, 0, nil
	}

	cursor, err := r.coll.Find(ctx, filter, listFindOptions(pageIndex, pageSize))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to find wih results: %w", err)
	}
	defer cursor.Close(ctx)

	var docs []wihDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, 0, fmt.Errorf("failed to decode wih results: %w", err)
	}

	rows := make([]models.WIHRow, 0, len(docs))
	for _, d := range docs {
		rows = append(rows, toRow(d))
	}
	return rows, total, nil
}

// FindByID 取单条记录。
func (r *repository) FindByID(ctx context.Context, id primitive.ObjectID) (*models.WIHDetail, error) {
	var doc wihDoc
	err := r.coll.FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find wih result %s: %w", id.Hex(), err)
	}
	return &models.WIHDetail{WIHRow: toRow(doc)}, nil
}

// ExplainList 列表查询的 explain。
func (r *repository) ExplainList(ctx context.Context, filter bson.M, pageIndex, pageSize int) (bson.M, error) {
	cmd := bson.D{
		{Key: "explain", Value: bson.D{
			{Key: "find", Value: "SensitiveResult"},
			{Key: "filter", Value: filter},
			{Key: "sort", Value: listSort},
			{Key: "skip", Value: int64((pageIndex - 1) * pageSize)},
			{Key: "limit", Value: int64(pageSize)},
			{Key: "projection", Value: listProjection},
		}},
		{Key: "verbosity", Value: "queryPlanner"},
	}
	var out bson.M
	if err := r.db.RunCommand(ctx, cmd).Decode(&out); err != nil {
		return nil, fmt.Errorf("failed to explain wih list: %w", err)
	}
	return out, nil
}

func toRow(d wihDoc) models.WIHRow {
	match := d.Match
	if match == nil {
		match = []string{}
	}
	tags := d.Tags
	if tags == nil {
		tags = []string{}
	}
	return models.WIHRow{
		ID:         d.ID.Hex(),
		Type:       d.SID,
		URL:        d.URL,
		Host:       hostFromURL(d.URL),
		Match:      match,
		Color:      d.Color,
		Tags:       tags,
		Status:     d.Status,
		Project:    d.Project,
		RootDomain: d.RootDomain,
		TaskName:   d.TaskName,
		Time:       d.Time,
		BodyID:     d.MD5,
	}
}

// hostFromURL 从 URL 里取 host 供列表展示/分组；解析失败就返回空串，不影响其它字段。
func hostFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Host
}
