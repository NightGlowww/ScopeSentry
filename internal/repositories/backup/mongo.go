// repositories/backup-------------------------------------
// @file      : mongo.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2026/11/18 01:15
// -------------------------------------------

package backup

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrMongoUnavailable MongoDB 未初始化（连接失败）
var ErrMongoUnavailable = errors.New("mongodb is not available")

const (
	// maxBSONDocumentSize BSON 文档硬上限 16MB
	maxBSONDocumentSize = 16 * 1024 * 1024
	restoreBatchSize    = 500
)

// MongoRepository 集合枚举与 BSON 流导入导出。
type MongoRepository interface {
	ListCollections(ctx context.Context) ([]string, error)
	ExportCollection(ctx context.Context, name string, w io.Writer) (documents int64, sha256Hex string, err error)
	RestoreCollection(ctx context.Context, name string, r io.Reader) (documents int64, err error)
}

type mongoRepository struct {
	db *mongo.Database
}

// NewMongoRepository 使用全局连接
func NewMongoRepository() MongoRepository {
	return &mongoRepository{db: mongodb.DB}
}

// NewMongoRepositoryWithDB 指定 database（测试用）
func NewMongoRepositoryWithDB(db *mongo.Database) MongoRepository {
	return &mongoRepository{db: db}
}

func (r *mongoRepository) collection(name string) (*mongo.Collection, error) {
	if r.db == nil {
		return nil, ErrMongoUnavailable
	}
	return r.db.Collection(name), nil
}

// ListCollections 列出全部业务集合，跳过 system.*，按名称排序保证备份可复现。
func (r *mongoRepository) ListCollections(ctx context.Context) ([]string, error) {
	if r.db == nil {
		return nil, ErrMongoUnavailable
	}
	names, err := r.db.ListCollectionNames(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("list collections: %w", err)
	}
	result := make([]string, 0, len(names))
	for _, name := range names {
		if strings.HasPrefix(name, "system.") {
			continue
		}
		result = append(result, name)
	}
	sort.Strings(result)
	return result, nil
}

// ExportCollection 把集合逐文档编码为 BSON 连续流写入 w，返回文档数与流 sha256。
func (r *mongoRepository) ExportCollection(ctx context.Context, name string, w io.Writer) (int64, string, error) {
	coll, err := r.collection(name)
	if err != nil {
		return 0, "", err
	}
	cursor, err := coll.Find(ctx, bson.D{})
	if err != nil {
		return 0, "", fmt.Errorf("find %s: %w", name, err)
	}
	defer cursor.Close(ctx)

	hasher := sha256.New()
	sink := io.MultiWriter(w, hasher)
	var documents int64
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return documents, "", fmt.Errorf("decode %s: %w", name, err)
		}
		raw, err := bson.Marshal(doc)
		if err != nil {
			return documents, "", fmt.Errorf("marshal %s: %w", name, err)
		}
		if _, err := sink.Write(raw); err != nil {
			return documents, "", fmt.Errorf("write %s: %w", name, err)
		}
		documents++
	}
	if err := cursor.Err(); err != nil {
		return documents, "", fmt.Errorf("iterate %s: %w", name, err)
	}
	return documents, hex.EncodeToString(hasher.Sum(nil)), nil
}

// RestoreCollection 读取 BSON 流并逐文档 ReplaceOne(upsert)。
func (r *mongoRepository) RestoreCollection(ctx context.Context, name string, reader io.Reader) (int64, error) {
	coll, err := r.collection(name)
	if err != nil {
		return 0, err
	}

	var documents int64
	batch := make([]mongo.WriteModel, 0, restoreBatchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if _, err := coll.BulkWrite(ctx, batch, options.BulkWrite().SetOrdered(false)); err != nil {
			return fmt.Errorf("bulk write %s: %w", name, err)
		}
		batch = batch[:0]
		return nil
	}

	for {
		var lengthBuf [4]byte
		if _, err := io.ReadFull(reader, lengthBuf[:]); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return documents, fmt.Errorf("read %s document length: %w", name, err)
		}
		length := int64(binary.LittleEndian.Uint32(lengthBuf[:]))
		if length < 5 || length > maxBSONDocumentSize {
			return documents, fmt.Errorf("invalid bson document length %d in %s", length, name)
		}
		raw := make([]byte, length)
		copy(raw[:4], lengthBuf[:])
		if _, err := io.ReadFull(reader, raw[4:]); err != nil {
			return documents, fmt.Errorf("read %s document body: %w", name, err)
		}
		var doc bson.M
		if err := bson.Unmarshal(raw, &doc); err != nil {
			return documents, fmt.Errorf("unmarshal %s document: %w", name, err)
		}
		id, ok := doc["_id"]
		if !ok {
			id = primitive.NewObjectID()
			doc["_id"] = id
		}
		model := mongo.NewReplaceOneModel().
			SetFilter(bson.M{"_id": id}).
			SetReplacement(doc).
			SetUpsert(true)
		batch = append(batch, model)
		documents++
		if len(batch) >= restoreBatchSize {
			if err := flush(); err != nil {
				return documents, err
			}
		}
	}
	if err := flush(); err != nil {
		return documents, err
	}
	return documents, nil
}
