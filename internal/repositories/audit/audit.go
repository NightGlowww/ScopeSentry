package audit

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const auditCollection = "audit_logs"

// Repository 操作日志读写。
type Repository interface {
	InsertMany(ctx context.Context, logs []models.AuditLog) error
	List(ctx context.Context, q models.AuditLogQuery) ([]models.AuditLog, int64, error)
	EnsureIndexes(ctx context.Context) error
}

type repository struct{}

// NewRepository 创建操作日志仓库。
func NewRepository() Repository { return &repository{} }

func coll() *mongo.Collection { return mongodb.DB.Collection(auditCollection) }

func (r *repository) InsertMany(ctx context.Context, logs []models.AuditLog) error {
	if len(logs) == 0 {
		return nil
	}
	docs := make([]interface{}, 0, len(logs))
	for i := range logs {
		docs = append(docs, logs[i])
	}
	_, err := coll().InsertMany(ctx, docs)
	return err
}

func (r *repository) List(ctx context.Context, q models.AuditLogQuery) ([]models.AuditLog, int64, error) {
	filter := bson.M{}
	if strings.TrimSpace(q.User) != "" {
		filter["user"] = bson.M{"$regex": regexp.QuoteMeta(strings.TrimSpace(q.User)), "$options": "i"}
	}
	if strings.TrimSpace(q.Path) != "" {
		filter["path"] = bson.M{"$regex": regexp.QuoteMeta(strings.TrimSpace(q.Path)), "$options": "i"}
	}
	if strings.TrimSpace(q.Method) != "" {
		filter["method"] = strings.ToUpper(strings.TrimSpace(q.Method))
	}
	if q.StatusMin > 0 {
		filter["status"] = bson.M{"$gte": q.StatusMin}
	}

	total, err := coll().CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "at", Value: -1}}).
		SetSkip(int64((q.PageIndex - 1) * q.PageSize)).
		SetLimit(int64(q.PageSize))
	cursor, err := coll().Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)
	var out []models.AuditLog
	if err := cursor.All(ctx, &out); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (r *repository) EnsureIndexes(ctx context.Context) error {
	_, err := coll().Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "at", Value: -1}}},
		{Keys: bson.D{{Key: "user", Value: 1}, {Key: "at", Value: -1}}},
	})
	return err
}

// 保证 time 包被使用（At 字段的默认值由中间件设置）
var _ = time.Now
