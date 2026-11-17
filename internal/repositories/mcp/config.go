package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	mongodriver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const configCollection = "mcp_config"

// ConfigRepository MCP 全局配置（单例文档 _id = "global"）
type ConfigRepository interface {
	GetConfig(ctx context.Context) (*models.McpConfig, error)
	SaveConfig(ctx context.Context, cfg *models.McpConfig) error
}

type configRepository struct{}

// NewConfigRepository 创建配置仓库。
// 集合按调用期从 mongodb.DB 解析：DB 未初始化（单测 / 启动早期）时不在构造期 panic，
// 而是在真正读写时返回 ErrConfigUnavailable。
func NewConfigRepository() ConfigRepository {
	return &configRepository{}
}

func (r *configRepository) coll() (*mongodriver.Collection, error) {
	if mongodb.DB == nil {
		return nil, fmt.Errorf("%w: database not initialized", ErrConfigUnavailable)
	}
	return mongodb.DB.Collection(configCollection), nil
}

// GetConfig 读配置；文档不存在时返回内存默认值（不写库，避免读路径产生写放大）
func (r *configRepository) GetConfig(ctx context.Context) (*models.McpConfig, error) {
	collection, err := r.coll()
	if err != nil {
		return nil, err
	}
	var cfg models.McpConfig
	err = collection.FindOne(ctx, bson.M{"_id": models.McpConfigGlobalID}).Decode(&cfg)
	if err != nil {
		if err == mongodriver.ErrNoDocuments {
			return models.DefaultMcpConfig(), nil
		}
		return nil, fmt.Errorf("%w: %v", ErrConfigUnavailable, err)
	}
	cfg.Normalize()
	return &cfg, nil
}

// SaveConfig upsert 单例文档
func (r *configRepository) SaveConfig(ctx context.Context, cfg *models.McpConfig) error {
	collection, err := r.coll()
	if err != nil {
		return err
	}
	cfg.Normalize()
	cfg.UpdatedAt = time.Now()
	_, err = collection.UpdateOne(ctx,
		bson.M{"_id": models.McpConfigGlobalID},
		bson.M{"$set": bson.M{
			"enabled":         cfg.Enabled,
			"allowedTools":    cfg.AllowedTools,
			"rateLimitPerMin": cfg.RateLimitPerMin,
			"logRetention":    cfg.LogRetention,
			"updatedAt":       cfg.UpdatedAt,
			"updatedBy":       cfg.UpdatedBy,
		}},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrConfigUnavailable, err)
	}
	return nil
}
