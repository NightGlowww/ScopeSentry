// configuration-------------------------------------
// @file      : service.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2025/10/29 21:20
// -------------------------------------------

package configuration

import (
	"context"
	"errors"
	"fmt"

	"github.com/Autumn-27/ScopeSentry/internal/config"
	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	schedulerCore "github.com/Autumn-27/ScopeSentry/internal/scheduler"
	assetCommon "github.com/Autumn-27/ScopeSentry/internal/services/assets/common"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	nservice "github.com/Autumn-27/ScopeSentry/internal/services/node"

	"github.com/Autumn-27/ScopeSentry/internal/repositories/common"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

// ErrNotificationNotFound 更新一条不存在的通知（否则 UpdateOne 匹配不到也回 nil）
var ErrNotificationNotFound = errors.New("notification: not found")

type Service struct {
	repo         common.Repository
	nodeService  nservice.Service
	dedupService assetCommon.DedupService
}

func NewService() *Service {
	return &Service{
		repo:         common.NewRepository(),
		nodeService:  nservice.NewService(),
		dedupService: assetCommon.NewDedupService(),
	}
}

const (
	collConfig       = "config"
	collNotification = "notification"
)

// GetSubfinderContent 读取 SubfinderApiConfig
func (s *Service) GetSubfinderContent(ctx *gin.Context) (string, error) {
	doc, err := s.repo.FindOne(ctx.Request.Context(), collConfig, bson.M{"name": "SubfinderApiConfig"}, bson.M{"_id": 0})
	if err != nil {
		return "", err
	}
	if v, ok := doc["value"].(string); ok {
		return v, nil
	}
	return "", nil
}

// SaveSubfinderContent 保存 SubfinderApiConfig 并通知节点刷新
func (s *Service) SaveSubfinderContent(ctx *gin.Context, content string) error {
	if err := s.repo.Upsert(ctx.Request.Context(), collConfig, bson.M{"name": "SubfinderApiConfig"}, bson.M{"name": "SubfinderApiConfig", "value": content}); err != nil {
		return err
	}
	// 通知所有节点刷新 subfinder 配置
	_ = s.nodeService.RefreshConfig(ctx, models.Message{Name: "all", Type: "subfinder"})
	return nil
}

// GetRadContent 读取 RadConfig
func (s *Service) GetRadContent(ctx *gin.Context) (string, error) {
	doc, err := s.repo.FindOne(ctx.Request.Context(), collConfig, bson.M{"name": "RadConfig"}, bson.M{"_id": 0})
	if err != nil {
		return "", err
	}
	if v, ok := doc["value"].(string); ok {
		return v, nil
	}
	return "", nil
}

// SaveRadContent 保存 RadConfig 并通知节点刷新
func (s *Service) SaveRadContent(ctx *gin.Context, content string) error {
	if err := s.repo.Upsert(ctx.Request.Context(), collConfig, bson.M{"name": "RadConfig"}, bson.M{"name": "RadConfig", "value": content}); err != nil {
		return err
	}
	_ = s.nodeService.RefreshConfig(ctx, models.Message{Name: "all", Type: "rad"})
	return nil
}

// GetSystemData 返回 type=system 的所有键值
func (s *Service) GetSystemData(ctx *gin.Context) (map[string]interface{}, error) {
	list, err := s.repo.FindMany(ctx.Request.Context(), collConfig, bson.M{"type": "system"}, nil)
	if err != nil {
		return nil, err
	}
	out := make(map[string]interface{})
	for _, m := range list {
		name, _ := m["name"].(string)
		if name == "" {
			continue
		}
		out[name] = m["value"]
	}
	return out, nil
}

// SaveSystemData 保存 system 配置，并通知节点
func (s *Service) SaveSystemData(ctx *gin.Context, kv map[string]interface{}) error {
	for k, v := range kv {
		doc := bson.M{"type": "system", "name": k, "value": v}
		if err := s.repo.Upsert(ctx.Request.Context(), collConfig, bson.M{"type": "system", "name": k}, doc); err != nil {
			return err
		}
	}
	timezone, _ := kv["timezone"].(string)
	modulesConfig := fmt.Sprintf("%v", kv["ModulesConfig"]) // 兼容字符串或其他类型
	msg := timezone + "[*]" + modulesConfig
	_ = s.nodeService.RefreshConfig(ctx, models.Message{Name: "all", Type: "system", Content: msg})
	return nil
}

// GetDeduplicationConfig 查询去重配置与下一次运行时间（若有）
func (s *Service) GetDeduplicationConfig(ctx *gin.Context) (map[string]interface{}, error) {
	doc, err := s.repo.FindOne(ctx.Request.Context(), collConfig, bson.M{"name": "deduplication"}, bson.M{"_id": 0})
	if err != nil {
		// 若不存在，返回空 map
		return map[string]interface{}{}, nil
	}
	return doc, nil
}

// dedupJobID 去重定时任务的固定 id（和配置文档同名，便于识别与幂等替换）
const dedupJobID = "deduplication"

// dedupCron 把「每 N 小时」翻译成 cron 表达式。
// N<=0 视为配置无效 → 退回默认 3 小时；N>=24 视为每天一次（0 点）。
// 分钟固定 0：去重是重活，别落在整分的其他任务堆里抢资源。
func dedupCron(hours int) string {
	if hours <= 0 {
		hours = 3
	}
	if hours >= 24 {
		return "0 0 * * *"
	}
	return fmt.Sprintf("0 */%d * * *", hours)
}

// EnsureDeduplicationJob 按配置幂等注册/移除去重定时任务。
//
// flag=true → 每 hour 小时跑一次；flag=false → 移除。启动期与每次保存都调，
// 这样「去重开关 + 去重周期」才真的对应一个在跑的定时任务（以前完全没有）。
func (s *Service) EnsureDeduplicationJob(ctx context.Context) error {
	doc, err := s.repo.FindOne(ctx, collConfig, bson.M{"name": "deduplication"}, bson.M{"_id": 0})
	if err != nil || doc == nil {
		return nil
	}
	enabled, _ := doc["flag"].(bool)
	hours := 3
	if v, ok := doc["hour"].(int32); ok && v > 0 {
		hours = int(v)
	} else if v, ok := doc["hour"].(int64); ok && v > 0 {
		hours = int(v)
	} else if v, ok := doc["hour"].(int); ok && v > 0 {
		hours = v
	}
	// 调度器里的 "interval" 其实是「每 N 天在 HH:MM 跑」，不是「每 N 小时」，
	// 用它表达「每 3 小时」会被拼成 Every(0).Days() 直接报错。这里用 cron 表达间隔。
	job := &schedulerCore.Job{
		ID:        dedupJobID,
		Name:      "deduplication",
		Handler:   "deduplication",
		CycleType: "cron",
		Schedule:  dedupCron(hours),
	}
	sched := schedulerCore.GetGlobalScheduler()
	if !enabled {
		// 关闭时移除：留着空转的 job 会让「下次运行时间」一直往前走，看起来像还在跑
		return sched.RemoveJob(dedupJobID)
	}
	if err := sched.RemoveJob(dedupJobID); err != nil {
		return err
	}
	if err := sched.AddJob(job); err != nil {
		return fmt.Errorf("register deduplication job: %w", err)
	}
	return nil
}

// SaveDeduplicationConfig 保存去重配置；如需要立即运行，透传到节点或调度器（此处仅保存与刷新）
func (s *Service) SaveDeduplicationConfig(ctx *gin.Context, cfg models.DepConfig) error {
	cfg.Name = "deduplication"
	runNow := cfg.RunNow
	cfg.RunNow = false
	if err := s.repo.Upsert(ctx.Request.Context(), collConfig, bson.M{"name": "deduplication"}, cfg); err != nil {
		return err
	}
	// 保存即生效：开关/周期直接对应一个在跑的定时任务（以前只存不排，开关等于摆设）。
	if err := s.EnsureDeduplicationJob(ctx.Request.Context()); err != nil {
		// 注册失败不该让保存回滚：配置已落库，下次启动还会按配置重建
		logger.Error("deduplication: register job failed: " + err.Error())
	}
	if runNow {
		go func() {
			err := s.dedupService.DoAssetDeduplication()
			if err != nil {
				logger.Error(err.Error())
				return
			}
		}()
	}
	return nil
}

// GetNotificationList 获取通知列表
func (s *Service) GetNotificationList(ctx *gin.Context) ([]models.Notification, error) {
	var notifications []models.Notification
	err := s.repo.Find(ctx.Request.Context(), collNotification, bson.M{}, nil, &notifications)
	if err != nil {
		return nil, err
	}
	return notifications, nil
}

// AddNotification 新增通知并刷新节点
func (s *Service) AddNotification(ctx *gin.Context, data models.Notification) error {
	_, err := s.repo.InsertOne(ctx.Request.Context(), collNotification, data)
	if err != nil {
		return err
	}
	_ = s.nodeService.RefreshConfig(ctx, models.Message{Name: "all", Type: "notification"})
	return nil
}

// UpdateNotification 更新通知并刷新节点
func (s *Service) UpdateNotification(ctx *gin.Context, data models.UpdateNotification) error {
	if data.ID == "" {
		return fmt.Errorf("missing id")
	}
	objID, err := primitive.ObjectIDFromHex(data.ID)
	if err != nil {
		return fmt.Errorf("invalid ObjectID: %v", err)
	}
	// 先确认文档存在：UpdateOne 匹配不到时返回 nil，
	// 于是「改一条不存在的通知」也回 200，界面上看起来保存成功（2026-11-29 实测）。
	if _, err := s.repo.FindOne(ctx.Request.Context(), collNotification, bson.M{"_id": objID}, bson.M{"_id": 1}); err != nil {
		return fmt.Errorf("%w: %s", ErrNotificationNotFound, data.ID)
	}
	if err := s.repo.UpdateOne(ctx.Request.Context(), collNotification, bson.M{"_id": objID}, bson.M{"$set": data}); err != nil {
		return err
	}
	_ = s.nodeService.RefreshConfig(ctx, models.Message{Name: "all", Type: "notification"})
	if err := mongodb.FindAll("notification", bson.M{"state": true}, bson.M{"_id": 0, "method": 1, "url": 1, "contentType": 1, "data": 1, "state": 1}, &config.NotificationApi); err != nil {
		return err
	}
	return nil
}

// DeleteNotifications 批量删除通知并刷新节点，返回真实删除条数
func (s *Service) DeleteNotifications(ctx *gin.Context, ids []string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	// 将 string 转换为 ObjectID
	objectIDs := make([]primitive.ObjectID, 0, len(ids))
	for _, id := range ids {
		objID, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			return 0, fmt.Errorf("invalid ObjectID: %s, error: %w", id, err)
		}
		objectIDs = append(objectIDs, objID)
	}
	filter := bson.M{"_id": bson.M{"$in": objectIDs}}
	deleted, err := s.repo.DeleteMany(ctx.Request.Context(), collNotification, filter)
	if err != nil {
		return 0, err
	}
	_ = s.nodeService.RefreshConfig(ctx, models.Message{Name: "all", Type: "notification"})
	return deleted, nil
}

// GetNotificationConfig 读取通知配置
func (s *Service) GetNotificationConfig(ctx *gin.Context) (map[string]interface{}, error) {
	doc, err := s.repo.FindOne(ctx.Request.Context(), collConfig, bson.M{"name": "notification"}, bson.M{})
	if err != nil {
		return map[string]interface{}{}, nil
	}
	delete(doc, "_id")
	delete(doc, "type")
	delete(doc, "name")
	return doc, nil
}

// UpdateNotificationConfig 更新通知配置并刷新节点
func (s *Service) UpdateNotificationConfig(ctx *gin.Context, data map[string]interface{}) error {
	if data == nil {
		return nil
	}
	// 必须 upsert：新装环境（或被清理过）里没有 name=notification 这条文档，
	// UpdateOne 匹配不到就静默什么都不做——接口回 200、GET 回来还是空，
	// 配置永远存不下来，且没有任何报错（2026-11-29 集成用例实测）。
	if err := s.repo.Upsert(ctx.Request.Context(), collConfig, bson.M{"name": "notification"}, data); err != nil {
		return err
	}
	_ = s.nodeService.RefreshConfig(ctx, models.Message{Name: "all", Type: "notification"})
	return nil
}
