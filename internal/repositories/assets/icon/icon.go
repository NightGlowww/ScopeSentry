// Package icon 读取扫描节点写入的网站图标（favicon）。
//
// 数据只有扫描节点（ScopeSentry-Scan）会写：它把 favicon 以 base64 存进共享 MongoDB 的
// icon 集合（fav3 → content），并把资产文档里的 iconcontent 清空。服务端与节点不共享
// 文件系统，所以图片请求必须能回源到这里，见 internal/api/handlers/images。
package icon

import (
	"context"
	"errors"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrUnavailable 表示服务端还没连上 MongoDB（启动期或配置错误）。
var ErrUnavailable = errors.New("mongodb is not connected")

// Repository 读取 favicon 内容。
type Repository interface {
	// FindContent 返回 base64 图标内容。
	// 两个集合都没有这条记录时返回 mongo.ErrNoDocuments，调用方据此回 HTTP 404。
	FindContent(ctx context.Context, hash string) (string, error)
}

type repository struct{}

// NewRepository 创建图标仓库。
func NewRepository() Repository { return &repository{} }

func (r *repository) FindContent(ctx context.Context, hash string) (string, error) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return "", mongo.ErrNoDocuments
	}
	if mongodb.DB == nil {
		return "", ErrUnavailable
	}

	content, err := r.fromIconCollection(ctx, hash)
	if err == nil {
		return content, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return "", err
	}
	// 1.8 之前未完成迁移的实例：图标还留在资产文档里。
	return r.fromLegacyAsset(ctx, hash)
}

func (r *repository) fromIconCollection(ctx context.Context, hash string) (string, error) {
	var record struct {
		Content string `bson:"content"`
	}
	err := mongodb.DB.Collection("icon").FindOne(ctx,
		bson.M{"fav3": hash},
		options.FindOne().SetProjection(bson.M{"content": 1}),
	).Decode(&record)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(record.Content) == "" {
		return "", mongo.ErrNoDocuments
	}
	return record.Content, nil
}

func (r *repository) fromLegacyAsset(ctx context.Context, hash string) (string, error) {
	var record struct {
		IconContent string `bson:"iconcontent"`
	}
	err := mongodb.DB.Collection("asset").FindOne(ctx,
		bson.M{"faviconmmh3": hash, "iconcontent": bson.M{"$exists": true, "$ne": ""}},
		options.FindOne().SetProjection(bson.M{"iconcontent": 1}),
	).Decode(&record)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(record.IconContent) == "" {
		return "", mongo.ErrNoDocuments
	}
	return record.IconContent, nil
}
