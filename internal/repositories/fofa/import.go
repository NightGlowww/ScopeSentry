package fofa

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// fofa_import 读写。
//
// 只承担两件事：
//   - LatestByQuery：同 query 的最近一次导入（host 去重基准）；
//   - List：导入历史倒序分页。
//
// 去重语义只在 service 层解释（「最近一次记录里的 host 跳过」），
// 仓库层不掺业务判断。

// ImportRepository fofa_import 数据访问
type ImportRepository interface {
	Create(ctx context.Context, record *models.FofaImport) error
	// LatestByQuery 同 query 最近一次导入；没有记录时返回 (nil, nil)
	LatestByQuery(ctx context.Context, query string) (*models.FofaImport, error)
	List(ctx context.Context, q models.FofaImportListQuery) ([]models.FofaImport, int64, error)
}

type importRepository struct{}

// NewImportRepository 创建导入记录仓库（集合按调用期解析，DB 未初始化时报错而不是 panic）
func NewImportRepository() ImportRepository { return &importRepository{} }

func (r *importRepository) coll() (*mongo.Collection, error) {
	if mongodb.DB == nil {
		return nil, errors.New("fofa_import: database not initialized")
	}
	return mongodb.DB.Collection(CollectionImport), nil
}

func (r *importRepository) Create(ctx context.Context, record *models.FofaImport) error {
	collection, err := r.coll()
	if err != nil {
		return err
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now()
	}
	result, err := collection.InsertOne(ctx, record)
	if err != nil {
		return err
	}
	if oid, ok := result.InsertedID.(primitive.ObjectID); ok {
		record.ID = oid
	}
	return nil
}

func (r *importRepository) LatestByQuery(ctx context.Context, query string) (*models.FofaImport, error) {
	collection, err := r.coll()
	if err != nil {
		return nil, err
	}
	var record models.FofaImport
	err = collection.FindOne(ctx, bson.M{"query": strings.TrimSpace(query)}, options.FindOne().
		SetSort(bson.D{{Key: "createdAt", Value: -1}})).
		Decode(&record)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

func (r *importRepository) List(ctx context.Context, q models.FofaImportListQuery) ([]models.FofaImport, int64, error) {
	collection, err := r.coll()
	if err != nil {
		return nil, 0, err
	}
	filter := bson.M{}
	if query := strings.TrimSpace(q.Query); query != "" {
		filter["query"] = bson.M{"$regex": query, "$options": "i"}
	}
	total, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	pageIndex := q.PageIndex
	if pageIndex <= 0 {
		pageIndex = 1
	}
	pageSize := q.PageSize
	if pageSize <= 0 {
		pageSize = models.FofaDefaultPageSize
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: -1}}).
		SetSkip(int64((pageIndex - 1) * pageSize)).
		SetLimit(int64(pageSize))
	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var list []models.FofaImport
	if err := cursor.All(ctx, &list); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}
