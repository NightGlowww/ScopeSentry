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
	List(ctx *gin.Context, search string, pageIndex, pageSize int, status *int, node string) ([]models.Task, int64, error)
	// DeleteByFilter 按筛选批量删除，返回条数
	DeleteByFilter(ctx *gin.Context, search string, status *int) (int, error)
	// CountTasksCreatedSince 统计某时间之后创建的任务数（防刷）
	CountTasksCreatedSince(ctx context.Context, since string) (int64, error)
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
	// StartTasksReport 批量开始并按 id 返回失败原因
	StartTasksReport(ctx *gin.Context, ids []string) (int, map[string]string)
	GetTaskProgress(ctx *gin.Context, taskID string, pageIndex, pageSize int) (map[string]interface{}, error)
	TaskProgressNumber(ctx *gin.Context) error
	TaskProgress(ctx context.Context) error
	// SweepStaleTasks 把长期无进度、无疑活动迹象的运行中任务判定为失败
	SweepStaleTasks(ctx context.Context, staleAfter time.Duration) (int, error)
	// ResumeRunningTasks 启动自愈：把没人消费的「运行中」任务重新投递给节点
	ResumeRunningTasks(ctx context.Context) (int, error)
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
func (s *service) List(ctx *gin.Context, search string, pageIndex, pageSize int, status *int, node string) ([]models.Task, int64, error) {
	filter := bson.M{}
	if search != "" {
		filter["name"] = bson.M{"$regex": search, "$options": "i"}
	}
	// 按节点筛选：node 是数组字段，直接等值匹配表示「包含该元素」
	if strings.TrimSpace(node) != "" {
		filter["node"] = strings.TrimSpace(node)
	}
	// 状态过滤：任务堆积时（几十个暂停任务）只靠名字搜很难找
	if status != nil && *status >= 0 {
		filter["status"] = *status
	}

	// 计算总数
	total, err := s.taskRepo.Count(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	projection := bson.M{
		"_id":        1,
		"status":     1,
		"name":       1,
		"taskNum":    1,
		"progress":   1,
		"creatTime":  1,
		"endTime":    1,
		"failReason": 1,
		"node":       1,
		"allNode":    1,
		"startedAt":  1,
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

	// 只给运行中的任务补「当前正在做什么」：长任务（目录扫描 7 分钟/目标）
	// 只显示百分比会像卡死。运行中任务通常 1-2 个，N+1 查询可接受。
	// 排队位置：任务在节点待领取队列里排第几（只对排队中的任务有意义）
	queuePositions := s.taskRepo.TaskQueuePositions(ctx)

	for i := range tasks {
		// 工具失败数对所有任务都填：它是任务「结束后」才写回的，
		// 只在运行中填等于永远看不到（2026-11-20 实测坑）。
		tasks[i].CmdFail = s.taskRepo.TaskCmdFail(ctx, tasks[i].ID.Hex())
		if tasks[i].Status != constants.TaskStatusRunning {
			continue
		}
		id := tasks[i].ID.Hex()
		mod, tgt, since := s.taskRepo.CurrentActivity(ctx, id)
		tasks[i].CurrentModule = mod
		tasks[i].CurrentTarget = tgt
		tasks[i].CurrentSince = since
		// 没有任何活动但还在队列里 = 排队中（节点一次只跑一个任务）。
		// 这类任务此前一直显示「运行中 0%」，让人误以为卡死。
		if pos, ok := queuePositions[tasks[i].ID.Hex()]; ok {
			tasks[i].QueuePos = pos
		}
		if mod == "" {
			if queued, err := s.taskRepo.HasQueuedTask(ctx, id); err == nil && queued {
				tasks[i].Queued = true
			}
		}
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

// StartTasksReport 批量开始，返回每个 id 的结果，便于界面说清「哪几个没起来、为什么」。
// 之前的 StartTasks 只返回最后一个错误：批量 4 个里失败 1 个，用户看到的是整批失败，
// 也不知道是哪一个（2026-11-20）。
func (s *service) StartTasksReport(ctx *gin.Context, ids []string) (int, map[string]string) {
	failed := map[string]string{}
	started := 0
	for _, id := range ids {
		if err := s.startOne(ctx, id); err != nil {
			failed[id] = err.Error()
			continue
		}
		started++
	}
	return started, failed
}

// startOne 启动单个任务；返回 error 时调用方负责把原因带回界面。
func (s *service) startOne(ctx *gin.Context, id string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return fmt.Errorf("任务 ID 非法")
	}
	t, err := s.taskRepo.FindByID(ctx, oid)
	if err != nil || t == nil {
		return fmt.Errorf("任务不存在")
	}
	if t.Progress == 100 {
		return fmt.Errorf("任务已完成，无需重新开始")
	}
	// stopToStart 语义是「从暂停处继续，不重建目标缓存」。
	// 新建任务（isStart=false）必须走 false，否则目标列表永远不生成，任务以 0 目标空跑。
	resume := t.IsStart
	t.IsStart = true
	if _, err := s.commonService.CreateTaskScan(ctx, *t, id, resume); err != nil {
		logger.Error(fmt.Sprintf("start task failed task=%s resume=%v err=%v", id, resume, err))
		return err
	}
	// startedAt 记录「本次真正开始/恢复」的时间：ETA 用它而不是创建时间，
	// 否则暂停过的任务会把暂停时长算进已用时间，估算严重偏大（2026-11-21）。
	_ = s.taskRepo.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": bson.M{
		"status":    1,
		"startedAt": helper.GetNowTimeString(),
	}})
	return nil
}

// StartTasks 开始任务：从暂停恢复继续扫描
func (s *service) StartTasks(ctx *gin.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	var lastErr error
	for _, id := range ids {
		if err := s.startOne(ctx, id); err != nil {
			lastErr = err
		}
	}
	return lastErr
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
		// 任务期间外部工具失败次数：弹窗顶部据此显示红色告警。
		// 走接口返回而不是前端从列表行传参，避免 keep-alive 缓存导致的陈旧值。
		"cmdFail": s.taskRepo.TaskCmdFail(ctx, taskID),
		// 任务产出统计：任务结束点开详情能直接看到战果
		"outcome": s.taskRepo.TaskOutcome(ctx, task.Name),
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
			// 进度不回退：节点重启/更新后当前目标会从头再跑一遍，
			// 目标级完成数下降会让百分比倒退（实测 81% → 50%，看着像任务重来一遍）。
			// 用户关心「还剩多少」，不是「这一轮跑到哪」，所以保留历史高水位。
			if task.Progress > progress {
				progress = task.Progress
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
// maxAutoResume 同一个任务因「重启打断」被自动重投的上限：
// 超过就落 failed，避免某条永远跑不完的任务被无限重投（2026-11-24）。
const maxAutoResume = 3

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
	swept := 0
	for _, task := range tasks {
		id := task.ID.Hex()

		// 「有人在跑」的唯一可靠信号是队列里有它；有进度 hash 不代表还活着
		// （节点重启/发布打断后 hash 会留着，2026-11-20 实测停在 8%/50% 数小时）。
		if queued, err := s.taskRepo.HasQueuedTask(ctx, id); err == nil && queued {
			continue
		}

		// 参考时间取「创建时间」与「最后一次模块结束时间」里较晚的那个：
		// 长任务会持续刷新 lastEnd，不会因为创建早而被误杀。
		ref := time.Time{}
		if t, perr := time.ParseInLocation("2006-01-02 15:04:05", task.CreatTime, time.Local); perr == nil {
			ref = t
		} else {
			ref = task.ID.Timestamp()
		}
		if _, _, _, lastEnd, perr := s.taskRepo.ProgressSummary(ctx, id); perr == nil && lastEnd != "" {
			if t, terr := time.ParseInLocation("2006-01-02 15:04:05", lastEnd, time.Local); terr == nil && t.After(ref) {
				ref = t
			}
		}
		// 关键：单个模块跑 20-60 分钟是常态（nuclei 全量模板），这段时间里没有「模块结束」事件。
		// 只看 lastEnd 会把「正在正常扫描」误判成「无进展」——2026-11-21 下午就是这么
		// 一次性把十几条任务打成失败的。用「当前模块的开始时间」作为存活信号。
		if _, _, since := s.taskRepo.CurrentActivity(ctx, id); since != "" {
			if t, terr := time.ParseInLocation("2006-01-02 15:04:05", since, time.Local); terr == nil && t.After(ref) {
				ref = t
			}
		}
		if now.Sub(ref) < staleAfter {
			continue
		}

		reason := fmt.Sprintf("超过 %s 无进展，且不在任何节点队列里（最后活动 %s）；多半是节点重启/更新打断了执行",
			staleAfter, ref.Format("2006-01-02 15:04:05"))

		// 2026-11-24：这类「没进展、也没人排队」绝大多数是发布/重启打断，不是扫描本身失败。
		// 以前直接落 failed（实测一次攒了 61 条），用户只能手动重开。
		// 现在先自动重投，沿用已有目标列表（IsStart=true 不重建），
		// 只有连续 maxAutoResume 次都没跑起来才判失败，避免无限循环。
		if task.AutoResumeCount < maxAutoResume {
			task.IsStart = true
			if _, rerr := s.commonService.CreateTaskScan(ctx, task, id, true); rerr != nil {
				logger.Error(fmt.Sprintf("auto-resume task %s failed, marking failed: %v", id, rerr))
			} else {
				if uerr := s.taskRepo.UpdateOne(ctx, bson.M{"_id": task.ID}, bson.M{
					"$set":   bson.M{"status": constants.TaskStatusRunning, "failReason": reason},
					"$inc":   bson.M{"autoResumeCount": 1},
					"$unset": bson.M{"endTime": ""},
				}); uerr != nil {
					logger.Error(fmt.Sprintf("mark auto-resume %s failed: %v", id, uerr))
					continue
				}
				swept++
				logger.Info(fmt.Sprintf("stale task auto-resumed (%d/%d): %s (%s), last activity %s",
					task.AutoResumeCount+1, maxAutoResume, task.Name, id, ref.Format("2006-01-02 15:04:05")))
				continue
			}
		}

		reason = fmt.Sprintf("%s；已自动重投 %d 次仍未跑起来", reason, task.AutoResumeCount)
		if err := s.taskRepo.UpdateOne(ctx, bson.M{"_id": task.ID}, bson.M{"$set": bson.M{
			"status":     constants.TaskStatusFailed,
			"endTime":    helper.GetNowTimeString(),
			"failReason": reason,
		}}); err != nil {
			logger.Error(fmt.Sprintf("sweep task %s failed: %v", id, err))
			continue
		}
		swept++
		logger.Info(fmt.Sprintf("stale task marked failed after %d auto-resumes: %s (%s)", task.AutoResumeCount, task.Name, id))
	}
	return swept, nil
}

// ResumeRunningTasks 服务端启动自愈：把「状态＝运行中但实际没人跑」的任务重新投递给节点。
//
// 场景：发布/重启服务端时，运行中的任务在 Redis 队列里的消息可能已过期，
// 或节点重启后没再收到任务；此时任务会停在 progress<100 且没人消费，
// 用户在页面上只看到「运行中 0%」，只能手动暂停再开始。
// 这里在启动时判定一次：没有进度、也不在队列里 → 重新投递（沿用已有目标列表，不重建）。
func (s *service) ResumeRunningTasks(ctx context.Context) (int, error) {
	filter := bson.M{
		"status":   constants.TaskStatusRunning,
		"progress": bson.M{"$lt": 100},
	}
	tasks, err := s.taskRepo.Find(ctx, filter, nil)
	if err != nil {
		return 0, fmt.Errorf("resume running tasks: %w", err)
	}

	// 启动时打一条总览：候选多少、最后恢复多少。没有这条日志时
	// 无法判断自愈到底有没有执行（2026-11-20 一开始就是"看不出跑没跑"）。
	logger.Info(fmt.Sprintf("resume scan: %d running task(s) to check", len(tasks)))
	resumed := 0
	for _, task := range tasks {
		id := task.ID.Hex()
		// 不能因为 TaskInfo:<id> 还在就跳过：目标列表残留 ≠ 有节点会来取。
		// 节点重启/发布打断后列表会留着，但没人消费（2026-11-20 实测停在 8%）。
		// 可靠判据只有两个：队列里有没有这条任务、最近有没有进度写入。
		// 有进度 hash 不代表有人在跑：节点重启/发布打断后 hash 会留下，
		// 但没人再更新它（2026-11-20 实测：etoro 任务停在 8%，4 个 hash 静止）。
		// 用「最后一次模块结束时间」判断新鲜度，超过 10 分钟视为停滞。
		_, _, _, lastEnd, perr := s.taskRepo.ProgressSummary(ctx, id)
		if perr == nil && lastEnd != "" {
			if t, terr := time.ParseInLocation("2006-01-02 15:04:05", lastEnd, time.Local); terr == nil {
				if time.Since(t) < 10*time.Minute {
					continue // 最近还有写入，节点确实在跑
				}
			}
		}
		if queued, err := s.taskRepo.HasQueuedTask(ctx, id); err == nil && queued {
			continue // 队列里还有，等节点来取
		}
		// 真正的孤儿：状态是运行中，但没有目标、没有进度、也没人排队
		task.IsStart = true // 沿用已有目标列表继续跑，而不是重建
		if _, err := s.commonService.CreateTaskScan(ctx, task, id, true); err != nil {
			logger.Error(fmt.Sprintf("resume task %s failed: %v", id, err))
			continue
		}
		resumed++
		logger.Info(fmt.Sprintf("resumed task after restart: %s (%s)", task.Name, id))
	}
	return resumed, nil
}

// DeleteByFilter 按当前筛选（名字 + 状态）批量删除任务，返回删除条数。
// 场景：外部队列刷出几十个暂停任务，一页页勾选太慢（2026-11-20）。
// 安全边界：必须至少有一个筛选条件，且单次最多 200 条，避免误操作清空全库。
func (s *service) DeleteByFilter(ctx *gin.Context, search string, status *int) (int, error) {
	filter := bson.M{}
	if search != "" {
		filter["name"] = bson.M{"$regex": search, "$options": "i"}
	}
	if status != nil && *status >= 0 {
		filter["status"] = *status
	}
	if len(filter) == 0 {
		return 0, fmt.Errorf("拒绝执行：未提供任何筛选条件（会造成全量删除）")
	}
	opts := options.Find().SetProjection(bson.M{"_id": 1}).SetLimit(200)
	tasks, err := s.taskRepo.Find(ctx, filter, opts)
	if err != nil {
		return 0, fmt.Errorf("failed to query tasks: %w", err)
	}
	if len(tasks) == 0 {
		return 0, nil
	}
	ids := make([]string, 0, len(tasks))
	for _, t := range tasks {
		ids = append(ids, t.ID.Hex())
	}
	if err := s.DeleteTasks(ctx, ids, true); err != nil {
		return 0, err
	}
	return len(ids), nil
}

// CountTasksCreatedSince 统计某时间之后创建的任务数。
func (s *service) CountTasksCreatedSince(ctx context.Context, since string) (int64, error) {
	return s.taskRepo.CountTasksCreatedSince(ctx, since)
}
