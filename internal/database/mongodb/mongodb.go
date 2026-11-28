package mongodb

import (
	"context"
	"fmt"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/logger"

	"github.com/Autumn-27/ScopeSentry/internal/config"
	"github.com/Autumn-27/ScopeSentry/internal/utils/mongouri"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var Client *mongo.Client
var DB *mongo.Database

func init() {
	// 设置连接超时
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg := config.GlobalConfig.MongoDB
	_, clientOptions, err := mongouri.Build(mongouri.Options{
		Host:          cfg.IP,
		Port:          cfg.Port,
		Username:      cfg.Username,
		Password:      cfg.Password,
		AuthSource:    cfg.AuthSource,
		AuthMechanism: cfg.AuthMechanism,
		TLS:           cfg.TLS,
		TLSCAFile:     cfg.TLSCAFile,
	})
	if err != nil {
		logger.Error(fmt.Sprintf("failed to build MongoDB options for %s: %v", mongouri.URI(mongouri.Options{Host: cfg.IP, Port: cfg.Port, Username: cfg.Username}), err))
		return
	}

	// 连接到 MongoDB
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		logger.Error(fmt.Sprintf("failed to connect %v to MongoDB: %v", mongouri.URI(mongouri.Options{Host: cfg.IP, Port: cfg.Port, Username: cfg.Username}), err))
		return
	}

	// 检查连接
	if err = client.Ping(ctx, nil); err != nil {
		logger.Error(fmt.Sprintf("failed to ping MongoDB: %v", err))
		return
	}

	Client = client
	DB = client.Database(config.GlobalConfig.MongoDB.Database)

	logger.Info("MongoDB connected successfully")
	return
}

func Close() error {
	if Client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := Client.Disconnect(ctx); err != nil {
			return fmt.Errorf("failed to disconnect MongoDB: %v", err)
		}
	}
	return nil
}

// ================= 工具方法 =================

func Collection(name string) *mongo.Collection {
	return DB.Collection(name)
}

func defaultContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

// ================= CRUD 封装 =================

// 插入一条
func InsertOne(col string, document interface{}) (*mongo.InsertOneResult, error) {
	ctx, cancel := defaultContext()
	defer cancel()

	return Collection(col).InsertOne(ctx, document)
}

// 插入多条
func InsertMany(col string, documents []interface{}) (*mongo.InsertManyResult, error) {
	ctx, cancel := defaultContext()
	defer cancel()

	return Collection(col).InsertMany(ctx, documents)
}

// 查询一条并解码
func FindOne(col string, filter interface{}, result interface{}) error {
	ctx, cancel := defaultContext()
	defer cancel()

	return Collection(col).FindOne(ctx, filter).Decode(result)
}

// 查询多条
func FindMany(col string, filter interface{}, result interface{}, opts ...*options.FindOptions) error {
	ctx, cancel := defaultContext()
	defer cancel()

	cursor, err := Collection(col).Find(ctx, filter, opts...)
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	return cursor.All(ctx, result)
}

func FindAll(collectionName string, query, selector, result interface{}) error {
	collection := Collection(collectionName)
	cur, err := collection.Find(context.Background(), query, options.Find().SetProjection(selector))
	if err != nil {
		return err
	}
	defer cur.Close(context.Background())

	return cur.All(context.Background(), result)
}

// 更新一条
func UpdateOne(col string, filter interface{}, update interface{}) (*mongo.UpdateResult, error) {
	ctx, cancel := defaultContext()
	defer cancel()

	return Collection(col).UpdateOne(ctx, filter, update)
}

// 更新多条
func UpdateMany(col string, filter interface{}, update interface{}) (*mongo.UpdateResult, error) {
	ctx, cancel := defaultContext()
	defer cancel()

	return Collection(col).UpdateMany(ctx, filter, update)
}

// 删除一条
func DeleteOne(col string, filter interface{}) (*mongo.DeleteResult, error) {
	ctx, cancel := defaultContext()
	defer cancel()

	return Collection(col).DeleteOne(ctx, filter)
}

// 删除多条
func DeleteMany(col string, filter interface{}) (*mongo.DeleteResult, error) {
	ctx, cancel := defaultContext()
	defer cancel()

	return Collection(col).DeleteMany(ctx, filter)
}

// 统计数量
func Count(col string, filter interface{}) (int64, error) {
	ctx, cancel := defaultContext()
	defer cancel()

	return Collection(col).CountDocuments(ctx, filter)
}

// CollectionOrNil 返回集合句柄；DB 尚未初始化时返回 nil。
//
// 背景：连接是在本包的 init() 里做的，失败只记日志并返回，此时 DB 为 nil。
// 而不少 repository 的构造函数（甚至包级 var）会在各自的 init 阶段取集合，
// 直接调 DB.Collection 会 nil panic —— 表现为「Mongo 尚未就绪时进程启动即崩」。
// 构造函数里用本函数取句柄，把崩溃推迟到真正的请求路径（那时 DB 必然已就绪或更早暴露）。
func CollectionOrNil(name string) *mongo.Collection {
	if DB == nil {
		return nil
	}
	return DB.Collection(name)
}
