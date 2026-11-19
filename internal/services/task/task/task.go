// Package task -----------------------------
// @file      : task.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2025/5/4 22:04
// -------------------------------------------
package task

import (
	"context"
	"fmt"
	"github.com/Autumn-27/ScopeSentry/internal/services/plugin"
	"github.com/Autumn-27/ScopeSentry/internal/services/project"
	"github.com/Autumn-27/ScopeSentry/internal/services/task/tasksync"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/logger"

	"github.com/Autumn-27/ScopeSentry/internal/constants"
	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/repositories/task/task"
	schedulerCore "github.com/Autumn-27/ScopeSentry/internal/scheduler"
	"github.com/Autumn-27/ScopeSentry/internal/services/node"
	commonTask "github.com/Autumn-27/ScopeSentry/internal/services/task/common"
	schedulerSvc "github.com/Autumn-27/ScopeSentry/internal/services/task/scheduler"
	"github.com/Autumn-27/ScopeSentry/internal/services/task/taskstate"
	"github.com/Autumn-27/ScopeSentry/internal/utils/helper"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Service 定义任务服务接口
type Service interface {
	List(ctx *gin.Context, search string, pageIndex, pageSize int) ([]models.Task, int64, error)
	CheckTaskNameExists(ctx *gin.Context, name string) (bool, error)
	GetTaskDetail(ctx *gin.Context, taskID string) (*models.Task, error)
	GetTasksByIDs(ctx *gin.Context, ids []string) ([]models.Task, error)
	DeleteTasks(ctx *gin.Context, ids []string, delA bool) error
	RetestTask(ctx context.Context, id string) error
	StopTasks(ctx *gin.Context, ids []string) error
	// 目标级暂停（issue #277）
	PauseTarget(ctx *gin.Context, taskID, target string) error
	ResumeTarget(ctx *gin.Context, taskID, target string) error
	GetPausedTargets(ctx *gin.Context, taskID string) (map[string]bool, error)
	StartTasks(ctx *gin.Context, ids []string) error
	GetTaskProgress(ctx *gin.Context, taskID string, pageIndex, pageSize int) (map[string]interface{}, error)
	TaskProgressNumber(ctx *gin.Context) error
	TaskProgress(ctx context.Context) error
	// SweepStaleTasks 把长期无进度、无疑活动迹象的运行中任务判定为失败
	SweepStaleTasks(ctx context.Context, staleAfter time.Duration) (int, error)
	ProcessTaskProgress(ctx context.Context, task models.Task) error
	GetAllTaskNames(ctx *gin.Context) ([]models.Task, error)
}

// service 实现Service接口
type service struct {
	taskRepo         task.Repository
	commonService    commonTask.Service
	schedulerService schedulerSvc.Service
	nodeService      node.Service
	pluginService    plugin.Service
	projectService   project.Service
}

// NewService 创建任务服务实例
func NewService() Service {
	return &service{
		taskRepo:         task.NewRepository(),
		commonService:    commonTask.NewService(),
		schedulerService: schedulerSvc.NewService(),
		nodeService:      node.NewService(),
		pluginService:    plugin.NewService(),
		projectService:   project.NewService(),
	}
}

// List 获取任务列表
func (s *service) List(ctx *gin.Context, search string, pageIndex, pageSize int) ([]models.Task, int64, error) {
	filter := bson.M{}
	if search != "" {
		filter["name"] = bson.M{"$regex": search, "$options": "i"}
	}

	// 计算总数
	total, err := s.taskRepo.Count(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	projection := bson.M{
		"_id":       1,
		"status":    1,
		"name":      1,
		"taskNum":   1,
		"progress":  1,
		"creatTime": 1,
		"endTime":   1,
	}

	// 分页查询
	opts := options.Find().
		SetProjection(projection).
		SetSkip(int64((pageIndex - 1) * pageSize)).
		SetLimit(int64(pageSize)).
		SetSort(bson.D{{Key: "creatTime", Value: -1}})

	tasks, err := s.taskRepo.Find(ctx.Request.Context(), filter, opts)
	if err != nil {
		return nil, 0, err
	}

	return tasks, total, nil
}

// CheckTaskNameExists 检查任务名是否存在
func (s *service) CheckTaskNameExists(ctx *gin.Context, name string) (bool, error) {
	return s.taskRepo.CheckTaskNameExists(ctx, name)
}

// GetTaskDetail 获取任务详情
func (s *service) GetTaskDetail(ctx *gin.Context, taskID string) (*models.Task, error) {
	// 将字符串ID转换为ObjectID
	objectID, err := primitive.ObjectIDFromHex(taskID)
	if err != nil {
		return nil, err
	}

	// 查询任务详情
	task, err := s.taskRepo.FindByID(ctx, objectID)
	if err != nil {
		return nil, err
	}

	return task, nil
}

// GetTasksByIDs 按任务 ID 批量查询任务（同步到项目时使用，见 issue #317）。
func (s *service) GetTasksByIDs(ctx *gin.Context, ids []string) ([]models.Task, error) {
	objIDs := make([]primitive.ObjectID, 0, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			continue
		}
		oid, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			return nil, fmt.Errorf("invalid task id %q: %w", id, err)
		}
		objIDs = append(objIDs, oid)
	}
	if len(objIDs) == 0 {
		return nil, nil
	}
	return s.taskRepo.Find(ctx.Request.Context(), bson.M{"_id": bson.M{"$in": objIDs}}, nil)
}

// DeleteTasks 批量删除任务
func (s *service) DeleteTasks(ctx *gin.Context, ids []string, delA bool) error {
	// 1. 移除调度任务与配置
	// 先尝试从自定义调度器中移除任务
	for _, id := range ids {
		if id == "" {
			continue
		}
		// 移除 gocron 调度任务与 scheduled 集合记录
		_ = schedulerCore.GetGlobalScheduler().RemoveJob(id)

		// 清理 Redis 缓存
		_ = s.taskRepo.ClearTaskCache(ctx, id)
	}

	// 删除 ScheduledTasks 集合中的配置
	err := s.schedulerService.DeleteScheduledTasks(ctx, ids)
	if err != nil {
		logger.Error(fmt.Sprintf("error while deleting tasks: %v", err))
	}

	// 2. 可选：删除资产（依据 taskName）
	if delA {
		// 获取任务名称列表
		var objIDs []primitive.ObjectID
		for _, id := range ids {
			if oid, err := primitive.ObjectIDFromHex(id); err == nil {
				objIDs = append(objIDs, oid)
			}
		}
		if len(objIDs) > 0 {
			// 仅查询 name 字段
			opts := options.Find().SetProjection(bson.M{"name": 1})
			tasks, err := s.taskRepo.Find(ctx.Request.Context(), bson.M{"_id": bson.M{"$in": objIDs}}, opts)
			if err == nil {
				nameSet := make(map[string]struct{})
				for _, t := range tasks {
					if t.Name != "" {
						nameSet[t.Name] = struct{}{}
					}
				}
				if len(nameSet) > 0 {
					var names []string
					for n := range nameSet {
						names = append(names, n)
					}
					// 广播到所有节点进行后续清理（兼容原 refresh_config 行为）
					_ = s.nodeService.RefreshConfig(ctx, models.Message{Name: "all", Type: "delete_task", Content: strings.Join(ids, ",")})

					// 按 taskName 删除各资产集合数据
					// 注意：部分集合中 taskName 为数组字段，$in 也能匹配
					collections := []string{"asset", "DirScanResult", "SensitiveResult", "SubdomainTakerResult", "UrlScan", "crawler", "subdomain", "PageMonitoring", "app", "RootDomain", "mp", "IPAsset"}
					filter := bson.M{"taskName": bson.M{"$in": names}}
					db := mongodb.DB

					// vulnerabilityDetail 无 taskName 字段，只能按 vulnerability 的 hash 级联删。
					// 必须先取 hash 再删主表，否则留下永久孤儿明细（2026-11-19 实测）。
					hashCursor, herr := db.Collection("vulnerability").Find(ctx.Request.Context(), filter, options.Find().SetProjection(bson.M{"hash": 1}))
					if herr == nil {
						var hashes []interface{}
						for hashCursor.Next(ctx.Request.Context()) {
							var doc struct {
								Hash string `bson:"hash"`
							}
							if err := hashCursor.Decode(&doc); err == nil && doc.Hash != "" {
								hashes = append(hashes, doc.Hash)
							}
						}
						_ = hashCursor.Close(ctx.Request.Context())
						if len(hashes) > 0 {
							_, _ = db.Collection("vulnerabilityDetail").DeleteMany(ctx.Request.Context(), bson.M{"hash": bson.M{"$in": hashes}})
						}
					}
					_, _ = db.Collection("vulnerability").DeleteMany(ctx.Request.Context(), filter)

					for _, coll := range collections {
						_, _ = db.Collection(coll).DeleteMany(ctx.Request.Context(), filter)
					}
				}
			}
		}
	} else {
		// 仍发出节点配置刷新，保持与 Python 端一致
		_ = s.nodeService.RefreshConfig(ctx, models.Message{Name: "all", Type: "delete_task", Content: strings.Join(ids, ",")})
	}

	// 3. 删除 task 集合中的任务
	var objIDs2 []primitive.ObjectID
	for _, id := range ids {
		if oid, err := primitive.ObjectIDFromHex(id); err == nil {
			objIDs2 = append(objIDs2, oid)
		}
	}
	if len(objIDs2) == 0 {
		return nil
	}
	if _, err := s.taskRepo.DeleteMany(ctx, objIDs2); err != nil {
		return err
	}
	return nil
}

// RetestTask 重新测试任务
func (s *service) RetestTask(ctx context.Context, id string) error {
	// 获取任务
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	t, err := s.taskRepo.FindByID(ctx, objectID)
	if err != nil {
		return err
	}
	if t == nil {
		return fmt.Errorf("task not found")
	}

	// 创建扫描任务
	if _, err := s.commonService.CreateTaskScan(ctx, *t, id, false); err != nil {
		return err
	}

	// 更新任务状态
	update := bson.M{"$set": bson.M{
		"progress":  0,
		"creatTime": helper.GetNowTimeString(),
		"endTime":   "",
		"status":    1,
	}}
	if err := s.taskRepo.UpdateOne(ctx, bson.M{"_id": objectID}, update); err != nil {
		return err
	}
	return nil
}

// StopTasks 停止任务：广播停任务并将状态置为2
func (s *service) StopTasks(ctx *gin.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	for _, id := range ids {
		// 广播停止任务
		_ = s.nodeService.RefreshConfig(ctx, models.Message{Name: "all", Type: "stop_task", Content: id})
		// 清掉还没被节点领取的队列消息：节点若在停止后才上线，不该再把这条任务跑起来
		if removed, err := s.taskRepo.RemoveQueuedNodeTasks(ctx.Request.Context(), id); err == nil && removed > 0 {
			logger.Info(fmt.Sprintf("removed %d queued node task message(s) for stopped task %s", removed, id))
		}
		// 状态置为2
		if oid, err := primitive.ObjectIDFromHex(id); err == nil {
			// 暂停时把逐目标进度快照进 Mongo：Redis 的 TaskInfo:progress:* 会过期，
			// 之后打开「任务进度」弹窗就只剩一片 "-"（2026-11-19 实测）。
			s.snapshotTaskProgress(ctx, id)
			_ = s.taskRepo.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": bson.M{"status": 2}})
		}
	}
	return nil
}

// StartTasks 开始任务：从暂停恢复继续扫描
func (s *service) StartTasks(ctx *gin.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	for _, id := range ids {
		oid, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			continue
		}
		t, err := s.taskRepo.FindByID(ctx, oid)
		if err != nil || t == nil {
			continue
		}
		if t.Progress == 100 {
			// 已完成，不再启动
			continue
		}
		// stopToStart 语义是「从暂停处继续，不重建目标缓存」。
		// 新建任务（isStart=false）必须走 false，否则目标列表永远不生成，任务以 0 目标空跑。
		resume := t.IsStart
		t.IsStart = true
		if _, err := s.commonService.CreateTaskScan(ctx, *t, id, resume); err != nil {
			logger.Error(fmt.Sprintf("StartTasks create scan failed task=%s resume=%v err=%v", id, resume, err))
			continue
		}
		_ = s.taskRepo.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": bson.M{"status": 1}})
	}
	return nil
}

// GetTaskProgress 获取任务进度信息
func (s *service) GetTaskProgress(ctx *gin.Context, taskID string, pageIndex, pageSize int) (map[string]interface{}, error) {
	// 1. 获取任务基本信息
	objectID, err := primitive.ObjectIDFromHex(taskID)
	if err != nil {
		return nil, fmt.Errorf("invalid task ID: %w", err)
	}

	task, err := s.taskRepo.FindByID(ctx, objectID)
	if err != nil {
		return nil, fmt.Errorf("failed to find task: %w", err)
	}
	if task == nil {
		return nil, fmt.Errorf("task not found")
	}

	// 2. 处理目标列表和分页
	targets, err := s.processTargetsWithPagination(ctx, task.Target, task.Ignore, pageIndex, pageSize)
	if err != nil {
		return nil, fmt.Errorf("failed to process targets: %w", err)
	}

	// 3. 从Redis获取进度数据
	progressData, err := s.getProgressFromRedis(ctx, taskID, targets)
	if err != nil {
		return nil, fmt.Errorf("failed to get progress data: %w", err)
	}

	// 4. 标记被暂停（跳过）的目标（issue #277）
	if paused, err := s.GetPausedTargets(ctx, taskID); err == nil && len(paused) > 0 {
		for i := range progressData {
			if paused[progressData[i].Target] {
				progressData[i].Paused = true
			}
		}
	}

	return map[string]interface{}{
		"list":  progressData,
		"total": task.TaskNum,
	}, nil
}

// processTargetsWithPagination 处理目标列表并分页
func (s *service) processTargetsWithPagination(ctx *gin.Context, targetStr, ignoreStr string, pageIndex, pageSize int) ([]string, error) {
	if targetStr == "" {
		return []string{}, nil
	}

	// 分割目标字符串
	targetLines := strings.Split(targetStr, "\n")
	var allTargets []string

	// 处理每个目标行
	for _, line := range targetLines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// 使用 helper 获取目标列表（处理忽略规则）
		targets, err := helper.GetTargetList(line, ignoreStr)
		if err != nil {
			continue
		}
		allTargets = append(allTargets, targets...)
	}

	// 分页处理
	startIndex := (pageIndex - 1) * pageSize
	endIndex := startIndex + pageSize

	if startIndex >= len(allTargets) {
		return []string{}, nil
	}

	if endIndex > len(allTargets) {
		endIndex = len(allTargets)
	}

	return allTargets[startIndex:endIndex], nil
}

// autoSyncToProject 把已完成任务的目标并入绑定项目（tasksync.AutoBindRequest 产出）。
// 失败只记日志：自动同步是附带动作，不能让任务完成状态回滚。
func (s *service) autoSyncToProject(req tasksync.Request) {
	ctx := context.Background()
	ids := make([]primitive.ObjectID, 0, len(req.IDS))
	for _, id := range req.IDS {
		if oid, err := primitive.ObjectIDFromHex(id); err == nil {
			ids = append(ids, oid)
		}
	}
	if len(ids) == 0 {
		return
	}
	tasks, err := s.taskRepo.Find(ctx, bson.M{"_id": bson.M{"$in": ids}}, nil)
	if err != nil {
		logger.Error(fmt.Sprintf("auto sync: failed to load tasks: %v", err))
		return
	}
	targets, err := tasksync.ExtractTargets(tasks)
	if err != nil {
		logger.Error(fmt.Sprintf("auto sync: %v", err))
		return
	}
	added, err := s.projectService.SyncTaskTargets(ctx, req.Project, targets)
	if err != nil {
		logger.Error(fmt.Sprintf("auto sync to project %s failed: %v", req.Project, err))
		return
	}
	logger.Info(fmt.Sprintf("auto sync to project %s: %d new target(s)", req.Project, added))
}

// snapshotTaskProgress 把完成任务的逐目标进度明细落库（issue #278）。
// 失败只记日志：快照是补充数据，不能让任务状态更新跟着失败。
func (s *service) snapshotTaskProgress(ctx context.Context, taskID string) {
	rows, err := s.taskRepo.CollectProgressSnapshot(ctx, taskID)
	if err != nil {
		logger.Error(fmt.Sprintf("failed to collect progress snapshot for task %s: %v", taskID, err))
		return
	}
	if len(rows) == 0 {
		return
	}
	if err := s.taskRepo.SaveProgressSnapshot(ctx, taskID, rows); err != nil {
		logger.Error(fmt.Sprintf("failed to save progress snapshot for task %s: %v", taskID, err))
	}
}

// getProgressFromRedis 从Redis获取进度数据
func (s *service) getProgressFromRedis(ctx *gin.Context, taskID string, targets []string) ([]models.TaskProgressDetail, error) {
	var progressList []models.TaskProgressDetail

	// issue #278：任务完成后 Redis 明细会被清理，这时回放 Mongo 快照。
	// 只有真在 Redis 读不到数据时才查一次库，避免每轮进度轮询都打 Mongo。
	var snapshot map[string]map[string]string
	snapshotLoaded := false
	loadSnapshot := func() map[string]map[string]string {
		if snapshotLoaded {
			return snapshot
		}
		snapshotLoaded = true
		rows, err := s.taskRepo.GetProgressSnapshot(ctx.Request.Context(), taskID)
		if err != nil {
			logger.Error(fmt.Sprintf("failed to load progress snapshot for task %s: %v", taskID, err))
			return nil
		}
		snapshot = rows
		return snapshot
	}

	for _, target := range targets {
		progress := s.createProgressResult(target)

		// 从Redis获取该目标的进度信息
		redisKey := fmt.Sprintf("TaskInfo:progress:%s:%s", taskID, target)
		progressData, err := s.taskRepo.GetProgressFromRedis(ctx, redisKey)
		if err == nil && len(progressData) > 0 {
			s.populateProgressFromRedis(progress, progressData)
		} else if data := loadSnapshot()[target]; len(data) > 0 {
			s.populateProgressFromRedis(progress, data)
		}

		progressList = append(progressList, *progress)
	}

	return progressList, nil
}

// createProgressResult 创建进度结果结构
func (s *service) createProgressResult(target string) *models.TaskProgressDetail {
	return &models.TaskProgressDetail{
		ID:                  primitive.NewObjectID(),
		TargetHandler:       []string{"", ""},
		SubdomainScan:       []string{"", ""},
		SubdomainSecurity:   []string{"", ""},
		PortScanPreparation: []string{"", ""},
		PortScan:            []string{"", ""},
		PortFingerprint:     []string{"", ""},
		AssetMapping:        []string{"", ""},
		AssetHandle:         []string{"", ""},
		URLScan:             []string{"", ""},
		WebCrawler:          []string{"", ""},
		URLSecurity:         []string{"", ""},
		DirScan:             []string{"", ""},
		VulnerabilityScan:   []string{"", ""},
		All:                 []string{"", ""},
		Target:              target,
		Node:                "",
	}
}

// populateProgressFromRedis 从Redis数据填充进度信息
func (s *service) populateProgressFromRedis(progress *models.TaskProgressDetail, redisData map[string]string) {
	// 映射Redis字段到进度结构
	fieldMappings := map[string]*[]string{
		"TargetHandler_start":       &progress.TargetHandler,
		"SubdomainScan_start":       &progress.SubdomainScan,
		"SubdomainSecurity_start":   &progress.SubdomainSecurity,
		"PortScanPreparation_start": &progress.PortScanPreparation,
		"PortScan_start":            &progress.PortScan,
		"PortFingerprint_start":     &progress.PortFingerprint,
		"AssetMapping_start":        &progress.AssetMapping,
		"AssetHandle_start":         &progress.AssetHandle,
		"URLScan_start":             &progress.URLScan,
		"WebCrawler_start":          &progress.WebCrawler,
		"URLSecurity_start":         &progress.URLSecurity,
		"DirScan_start":             &progress.DirScan,
		"VulnerabilityScan_start":   &progress.VulnerabilityScan,
		"scan_start":                &progress.All,
	}

	// 填充开始时间
	for redisField, progressField := range fieldMappings {
		if startTime, exists := redisData[redisField]; exists {
			(*progressField)[0] = startTime
		}
	}

	// 填充结束时间
	endFieldMappings := map[string]*[]string{
		"TargetHandler_end":       &progress.TargetHandler,
		"SubdomainScan_end":       &progress.SubdomainScan,
		"SubdomainSecurity_end":   &progress.SubdomainSecurity,
		"PortScanPreparation_end": &progress.PortScanPreparation,
		"PortScan_end":            &progress.PortScan,
		"PortFingerprint_end":     &progress.PortFingerprint,
		"AssetMapping_end":        &progress.AssetMapping,
		"AssetHandle_end":         &progress.AssetHandle,
		"URLScan_end":             &progress.URLScan,
		"WebCrawler_end":          &progress.WebCrawler,
		"URLSecurity_end":         &progress.URLSecurity,
		"DirScan_end":             &progress.DirScan,
		"VulnerabilityScan_end":   &progress.VulnerabilityScan,
		"scan_end":                &progress.All,
	}

	for redisField, progressField := range endFieldMappings {
		if endTime, exists := redisData[redisField]; exists {
			(*progressField)[1] = endTime
		}
	}

	// 设置节点信息
	if node, exists := redisData["node"]; exists {
		progress.Node = node
	}
}

func (s *service) TaskProgressNumber(ctx *gin.Context) error {
	filter := bson.M{
		"progress": bson.M{"$ne": 100},
		"status":   1,
	}
	// 调用 repository
	tasks, err := s.taskRepo.Find(ctx.Request.Context(), filter, nil)
	if err != nil {
		return err
	}

	for _, task := range tasks {
		fmt.Printf("任务ID: %s, 进度: %d, 状态: %d\n", task.ID.Hex(), task.Progress, task.Status)
		id := task.ID.Hex()
		key := fmt.Sprintf("TaskInfo:tmp:%s", id)
		fmt.Printf("<UNK>ID: %s\n", key)
	}
	return nil
}

// TaskProgress 更新任务进度
func (s *service) TaskProgress(ctx context.Context) error {
	// 查询所有进度不为100的任务：既包含运行中（1），也包含待开始（0）。
	// 待开始的任务必须一起轮询，否则节点产出进度后无法从待开始转为运行中。
	filter := bson.M{
		"progress": bson.M{"$ne": 100},
		"status":   bson.M{"$in": []int{constants.TaskStatusPending, constants.TaskStatusRunning}},
	}

	tasks, err := s.taskRepo.Find(ctx, filter, nil)
	if err != nil {
		return fmt.Errorf("failed to find running tasks: %w", err)
	}

	// 如果没有运行中的任务，直接返回
	if len(tasks) == 0 {
		return nil
	}

	// 处理每个任务
	for _, task := range tasks {
		if err := s.ProcessTaskProgress(ctx, task); err != nil {
			logger.Error(fmt.Sprintf("failed to process task progress for task %s: %v", task.ID.Hex(), err))
			continue
		}
	}

	return nil
}

// processTaskProgress 处理单个任务的进度更新
func (s *service) ProcessTaskProgress(ctx context.Context, task models.Task) error {
	taskID := task.ID.Hex()
	tmpKey := fmt.Sprintf("TaskInfo:tmp:%s", taskID)
	timeKey := fmt.Sprintf("TaskInfo:time:%s", taskID)

	// 检查Redis中是否存在临时统计键
	exists, err := s.taskRepo.Exists(ctx, tmpKey)
	if err != nil {
		return fmt.Errorf("failed to check tmp key existence: %w", err)
	}

	if exists {
		// 获取集合中元素的数量
		count, err := s.taskRepo.SCard(ctx, tmpKey)
		if err != nil {
			return fmt.Errorf("failed to get set cardinality: %w", err)
		}

		// 计算进度与下一个状态：出现进度后待开始的任务转为运行中，
		// 进度达到 100 时转为运行完成（状态3）。见 issue #288。
		progress, nextStatus := taskstate.Derive(task.Status, count, int64(task.TaskNum))

		// 如果进度达到100，更新任务状态为完成
		if nextStatus == constants.TaskStatusFinished {
			// 获取结束时间
			endTime, err := s.taskRepo.Get(ctx, timeKey)
			if err != nil {
				return fmt.Errorf("failed to get end time: %w", err)
			}

			// 更新任务状态为完成（状态3）并设置结束时间
			update := bson.M{
				"$set": bson.M{
					"endTime":  endTime,
					"status":   constants.TaskStatusFinished,
					"progress": progress,
				},
			}
			if err := s.taskRepo.UpdateOne(ctx, bson.M{"_id": task.ID}, update); err != nil {
				return fmt.Errorf("failed to update task status: %w", err)
			}

			// issue #278：完成即删 tmp/time，逐目标明细随后也可能被清理。
			// 先把 Redis 里的明细快照进 Mongo，接口在 Redis 读不到时回放这份快照。
			s.snapshotTaskProgress(ctx, taskID)

			// 删除Redis统计信息
			if err := s.taskRepo.Del(ctx, tmpKey, timeKey); err != nil {
				logger.Error(fmt.Sprintf("failed to delete redis keys for completed task %s: %v", taskID, err))
			}

			// 任务创建时如果绑定了项目（BindProject），完成后自动把目标并入该项目。
			// 之前这个字段只有 MCP 在建任务时写，没有任何代码消费，等于“填了也没用”。
			if req, ok := tasksync.AutoBindRequest(task); ok {
				go s.autoSyncToProject(req)
			}

			// 调用服务端插件
			go func() {
				err := s.pluginService.RunTaskEnd(task)
				if err != nil {
					return
				}
			}()

		} else {
			// 更新任务进度与状态（待开始 → 运行中）
			update := bson.M{
				"$set": bson.M{
					"progress": progress,
					"status":   nextStatus,
				},
			}
			if err := s.taskRepo.UpdateOne(ctx, bson.M{"_id": task.ID}, update); err != nil {
				return fmt.Errorf("failed to update task progress: %w", err)
			}
		}
	} else {
		// 没有 tmp 集合时不要直接把进度归零：节点实际把进度写在
		// TaskInfo:progress:<taskId>:<target> 的 hash 里（各模块 _start/_end 时间戳）。
		// 之前只认 tmp 集合，导致真实扫描跑完进度仍显示 0%。
		targets, completedTargets, partial, lastEnd, perr := s.taskRepo.ProgressSummary(ctx, taskID)
		if perr != nil {
			logger.Error(fmt.Sprintf("failed to summarise progress for task %s: %v", taskID, perr))
		}
		if targets > 0 {
			// 进度 = (已完成目标数 + 进行中目标的完成比例) / 任务目标总数。
			// 必须按「目标」而非「模块总数」算：模块总数会随新目标出现而变大，
			// 导致进度回退（实测 19% → 8%）。这个公式只增不减。
			// 分母只认任务声明的目标数。多节点并行时，子目标（www.x.com 这类）
			// 也会各自写一个 hash，用「hash 数量」做分母会让进度回退（实测 61%→33%）。
			expectedTargets := task.TaskNum
			if expectedTargets <= 0 {
				expectedTargets = targets
			}
			done := float64(completedTargets) + partial
			if done > float64(expectedTargets) {
				done = float64(expectedTargets)
			}
			progress := int(done * 100 / float64(expectedTargets))
			if progress > 100 {
				progress = 100
			}
			// 未全部完成时不超过 99%，避免「看着 100% 但其实还在跑」
			if completedTargets < expectedTargets && progress >= 100 {
				progress = 99
			}
			status := constants.TaskStatusRunning
			update := bson.M{"$set": bson.M{"progress": progress, "status": status}}
			if completedTargets >= expectedTargets {
				status = constants.TaskStatusFinished
				if lastEnd == "" {
					lastEnd = helper.GetNowTimeString()
				}
				update = bson.M{"$set": bson.M{
					"progress": 100,
					"status":   status,
					"endTime":  lastEnd,
				}}
				s.snapshotTaskProgress(ctx, taskID)
				go func() { _ = s.pluginService.RunTaskEnd(task) }()
			}
			if err := s.taskRepo.UpdateOne(ctx, bson.M{"_id": task.ID}, update); err != nil {
				return fmt.Errorf("failed to update task progress from redis hashes: %w", err)
			}
		} else {
			// 完全没有进度数据：保持 0，交给 SweepStaleTasks 处理僵尸任务
			update := bson.M{"$set": bson.M{"progress": 0}}
			if err := s.taskRepo.UpdateOne(ctx, bson.M{"_id": task.ID}, update); err != nil {
				return fmt.Errorf("failed to reset task progress: %w", err)
			}
		}
	}

	return nil
}

// GetAllTaskNames 获取所有任务名称，按 id 倒序
func (s *service) GetAllTaskNames(ctx *gin.Context) ([]models.Task, error) {
	filter := bson.M{}

	// 只查询 id 和 name 字段
	projection := bson.M{
		"_id":  1,
		"name": 1,
	}

	// 按 id 倒序排序
	opts := options.Find().
		SetProjection(projection).
		SetSort(bson.D{{Key: "_id", Value: -1}})

	tasks, err := s.taskRepo.Find(ctx.Request.Context(), filter, opts)
	if err != nil {
		return nil, err
	}

	return tasks, nil
}

// SweepStaleTasks 巡检僵尸任务：状态仍是运行中、进度未满，但创建时间已超过 staleAfter，
// 且 Redis 里既没有待处理目标队列（TaskInfo:<id>）也没有进度集合（TaskInfo:tmp:<id>），
// 判定为 failed。此前没有 failed 态，节点挂掉后任务会永久停在「运行中」。
func (s *service) SweepStaleTasks(ctx context.Context, staleAfter time.Duration) (int, error) {
	filter := bson.M{
		"status":   constants.TaskStatusRunning,
		"progress": bson.M{"$ne": 100},
	}
	tasks, err := s.taskRepo.Find(ctx, filter, nil)
	if err != nil {
		return 0, fmt.Errorf("sweep stale tasks: %w", err)
	}
	now := time.Now()
	// 节点仍在线时不要轻易判死：长任务可能几小时才产出第一份进度。
	// 只有「任务节点全部离线」或「超过硬上限（6 小时）」才进入失败判定。
	onlineNodes := map[string]struct{}{}
	if nodeData, err := s.nodeService.GetNodeData(ctx, true); err == nil {
		for _, n := range nodeData {
			onlineNodes[n.Name] = struct{}{}
		}
	}
	const hardLimit = 6 * time.Hour
	swept := 0
	for _, task := range tasks {
		created, perr := time.ParseInLocation("2006-01-02 15:04:05", task.CreatTime, time.Local)
		if perr != nil || now.Sub(created) < staleAfter {
			continue
		}
		nodeAlive := false
		for _, n := range task.Node {
			if _, ok := onlineNodes[n]; ok {
				nodeAlive = true
				break
			}
		}
		// 「有节点在线」不等于「这个任务在跑」：任务可能早就没被领取。
		// 只要既没有进度 hash、又不在任何 NodeTask 队列里，就没有节点会再执行它，
		// 此时继续显示「运行中 + 0%」是假状态（2026-11-19 实测 16 小时未变）。
		if nodeAlive && now.Sub(created) < hardLimit {
			id := task.ID.Hex()
			hasProgress := false
			if n, _, _, _, err := s.taskRepo.ProgressSummary(ctx, id); err == nil && n > 0 {
				hasProgress = true
			}
			if !hasProgress {
				if queued, err := s.taskRepo.HasQueuedTask(ctx, id); err == nil && !queued {
					task.Status = constants.TaskStatusFailed
					s.taskRepo.UpdateOne(ctx, bson.M{"_id": task.ID}, bson.M{"$set": bson.M{
						"status":  constants.TaskStatusFailed,
						"endTime": helper.GetNowTimeString(),
					}})
					swept++
					continue
				}
			}
			continue
		}
		id := task.ID.Hex()
		if ok, _ := s.taskRepo.Exists(ctx, "TaskInfo:"+id); ok {
			continue // 还有目标没被节点领走
		}
		if ok, _ := s.taskRepo.Exists(ctx, "TaskInfo:tmp:"+id); ok {
			continue // 节点仍在写进度
		}
		update := bson.M{"$set": bson.M{
			"status":   constants.TaskStatusFailed,
			"endTime":  helper.GetNowTimeString(),
			"progress": task.Progress,
		}}
		if err := s.taskRepo.UpdateOne(ctx, bson.M{"_id": task.ID}, update); err != nil {
			logger.Error(fmt.Sprintf("sweep stale tasks: update %s failed: %v", id, err))
			continue
		}
		logger.Warn(fmt.Sprintf("task %s marked failed: no progress after %s", id, staleAfter))
		swept++
	}
	return swept, nil
}
