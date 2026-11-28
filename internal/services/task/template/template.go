package template

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/models"

	"github.com/Autumn-27/ScopeSentry/internal/repositories/task/template"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Service 定义模板服务接口
type Service interface {
	List(ctx context.Context, pageIndex, pageSize int, query string) (*models.TemplateList, error)
	Detail(ctx *gin.Context, id string) (*models.ScanTemplate, error)
	Save(ctx context.Context, id string, result *models.ScanTemplate) (string, error)
	Delete(ctx *gin.Context, ids []string) (int64, error)
}

type service struct {
	repo template.Repository
}

// NewService 创建模板服务实例
func NewService() Service {
	return &service{
		repo: template.NewRepository(),
	}
}

// List 获取模板列表
func (s *service) List(ctx context.Context, pageIndex, pageSize int, query string) (*models.TemplateList, error) {
	filter := bson.M{
		"name": bson.M{"$regex": query},
	}

	// 设置查询选项
	opts := options.Find().
		SetProjection(bson.M{
			"_id":  1,
			"name": 1,
		}).
		SetSkip(int64((pageIndex - 1) * pageSize)).
		SetLimit(int64(pageSize)).SetSort(bson.M{"_id": -1})

	// 获取总数
	total, err := s.repo.Count(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to count templates: %w", err)
	}

	// 获取列表
	list, err := s.repo.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to find templates: %w", err)
	}

	return &models.TemplateList{
		List:  list,
		Total: total,
	}, nil
}

// Detail 获取模板详情
func (s *service) Detail(ctx *gin.Context, id string) (*models.ScanTemplate, error) {
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, fmt.Errorf("invalid id format: %w", err)
	}

	filter := bson.M{"_id": objID}

	result, err := s.repo.FindOne(ctx, filter, options.FindOne())
	if err != nil {
		return nil, fmt.Errorf("failed to find template: %w", err)
	}

	if result == nil {
		return nil, fmt.Errorf("template not found")
	}

	return result, nil
}

// Save 保存模板
// coreModules 是「缺了就会静默降级覆盖」的模块。
//
// 背景（2026-11-26 实测）：6 个扫描模板里 5 个的模块是 null，其中 light/deep 连 WebCrawler、
// DirScan 都没有。结果是任务照常跑、界面照常出资产，但 122 个 URL 里 0 个 API 端点、
// 0 条目录扫描结果——**覆盖缺口完全不可见**，只能靠人去看「资产怎么这么少」才发现。
//
// 这里在保存时硬拦：核心模块没有插件就拒绝保存，并列出缺哪些。
// 可选模块（TargetHandler / SubdomainSecurity / PortScanPreparation / PassiveScan）
// 视目标类型而定，不强制。
var coreModules = []string{
	"SubdomainScan", "PortScan", "PortFingerprint", "AssetMapping", "AssetHandle",
	"URLScan", "WebCrawler", "URLSecurity", "DirScan", "VulnerabilityScan",
}

func missingCoreModules(t *models.ScanTemplate) []string {
	pairs := map[string][]string{
		"SubdomainScan":     t.SubdomainScan,
		"PortScan":          t.PortScan,
		"PortFingerprint":   t.PortFingerprint,
		"AssetMapping":      t.AssetMapping,
		"AssetHandle":       t.AssetHandle,
		"URLScan":           t.URLScan,
		"WebCrawler":        t.WebCrawler,
		"URLSecurity":       t.URLSecurity,
		"DirScan":           t.DirScan,
		"VulnerabilityScan": t.VulnerabilityScan,
	}
	var missing []string
	for _, m := range coreModules {
		if len(pairs[m]) == 0 {
			missing = append(missing, m)
		}
	}
	return missing
}

// ErrIncompleteTemplate 表示模板缺核心模块。handler 据此返回 400 而不是 500——
// 这是「你填得不对」，不是「服务端出错」。
var ErrIncompleteTemplate = errors.New("模板不完整")

func (s *service) Save(ctx context.Context, id string, result *models.ScanTemplate) (string, error) {
	// 保存前先做完整性校验：缺核心模块直接拒绝，避免又一次「跑完了才发现没测接口面」。
	if missing := missingCoreModules(result); len(missing) > 0 {
		return "", fmt.Errorf("%w：以下核心模块没有选择插件 —— %s（这些模块缺失会让扫描静默降级，且界面上看不出来）",
			ErrIncompleteTemplate, strings.Join(missing, "、"))
	}
	if id == "" {
		// 插入新模板
		insertId, err := s.repo.InsertOne(ctx, result)
		if err != nil {
			return "", fmt.Errorf("failed to insert template: %w", err)
		}
		return insertId, err
	}

	// 更新现有模板
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return "", fmt.Errorf("invalid id format: %w", err)
	}

	filter := bson.M{"_id": objID}
	update := bson.M{"$set": result}

	err = s.repo.UpdateOne(ctx, filter, update)
	if err != nil {
		return "", fmt.Errorf("failed to update template: %w", err)
	}

	return "", nil
}

// Delete 删除模板
func (s *service) Delete(ctx *gin.Context, ids []string) (int64, error) {
	var objIDs []primitive.ObjectID
	for _, id := range ids {
		objID, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			return 0, fmt.Errorf("invalid id format: %w", err)
		}
		objIDs = append(objIDs, objID)
	}

	filter := bson.M{"_id": bson.M{"$in": objIDs}}
	deleted, err := s.repo.DeleteMany(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("failed to delete templates: %w", err)
	}

	return deleted, nil
}
