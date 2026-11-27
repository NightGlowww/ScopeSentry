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
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"strconv"

	"github.com/Autumn-27/ScopeSentry/internal/services/task/common"
	"github.com/Autumn-27/ScopeSentry/internal/services/task/task"
	"github.com/gin-gonic/gin"
)

var taskService task.Service
var commonService common.Service

// TaskDetailRequest 任务详情请求
type TaskDetailRequest struct {
	ID string `json:"id" binding:"required" example:"507f1f77bcf86cd799439011"`
}

// List 获取任务列表（分页+模糊搜索）
// @Summary      获取任务列表
// @Description  支持name字段模糊搜索与分页
// @Tags         task
// @Accept       json
// @Produce      json
// @Param        data  body      ListRequest  true  "查询参数"
// @Success      200  {object}  response.SuccessResponse{data=object{list=[]models.Task,total=int}}
// @Failure      400  {object}  response.BadRequestResponse
// @Failure      500  {object}  response.InternalServerErrorResponse
// @Router       /api/task [post]
func List(c *gin.Context) {
	var req models.ListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	go func() {
		err := taskService.TaskProgress(context.Background())
		if err != nil {
			logger.Error(fmt.Sprintf("%v", err))
		}
	}()
	tasks, total, err := taskService.List(c, req.Search, req.PageIndex, req.PageSize, req.Status, req.Node, req.BindProject, req.Sort)
	if err != nil {
		logger.Error(fmt.Sprintf("%v", err))
		response.InternalServerError(c, "api.task.list.error", err)
		return
	}
	resp := map[string]interface{}{
		"list":  tasks,
		"total": total,
	}
	response.Success(c, resp, "")
}

// TaskDetail 获取任务详情
// @Summary      获取任务详情
// @Description  根据任务ID获取任务的详细信息
// @Tags         task
// @Accept       json
// @Produce      json
// @Param        data  body      TaskDetailRequest  true  "任务ID"
// @Success      200  {object}  response.SuccessResponse{data=object{name=string,target=string,ignore=string,node=[]string,allNode=bool,scheduledTasks=bool,hour=int,duplicates=string,template=string,day=int,minute=int,project=[]string,search=string,cycleType=string}}
// @Failure      400  {object}  response.BadRequestResponse
// @Failure      404  {object}  response.NotFoundResponse
// @Failure      500  {object}  response.InternalServerErrorResponse
// @Router       /api/task/detail [post]
func TaskDetail(c *gin.Context) {
	var req TaskDetailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	// 检查ID是否提供
	if req.ID == "" {
		response.BadRequest(c, "api.task.id_missing", nil)
		return
	}

	// 获取任务详情
	taskDetail, err := taskService.GetTaskDetail(c, req.ID)
	if err != nil {
		logger.Error(fmt.Sprintf("TaskDetail %v", err))
		response.InternalServerError(c, "api.error", err)
		return
	}

	if taskDetail == nil {
		response.NotFound(c, "api.task.not_found", nil)
		return
	}
	response.Success(c, taskDetail, "")
}

// AddTask 添加任务
// @Summary 添加任务
// @Description 添加新的扫描任务
// @Tags Task
// @Accept json
// @Produce json
// @Param request body AddTaskRequest true "任务信息"
// @Success 200 {object} response.Response "成功"
// @Failure 400 {object} response.Response "请求错误"
// @Failure 500 {object} response.Response "服务器错误"
// @Router /api/task/add [post]
func AddTask(c *gin.Context) {
	var req models.Task
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	// 防刷：一分钟内创建任务过多通常是外部集成失控（2026-11-20 实测
	// huntdeck 集成每分钟建一个任务，把节点队列塞满）。这里挡一道，
	// 并把阈值写清楚，便于对方调整调用节奏。
	const TaskCreateRateLimit = 10
	since := time.Now().Add(-1 * time.Minute).Format("2006-01-02 15:04:05")
	if n, cerr := taskService.CountTasksCreatedSince(c.Request.Context(), since); cerr == nil && n >= TaskCreateRateLimit {
		response.BadRequest(c, "api.task.rate_limited", fmt.Errorf("每分钟最多创建 %d 个任务，当前已创建 %d 个", TaskCreateRateLimit, n))
		return
	}

	// 检查任务名是否已存在
	exists, err := taskService.CheckTaskNameExists(c, req.Name)
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	if exists {
		response.BadRequest(c, "api.task.name_exists", nil)
		return
	}

	// 验证必要参数
	if req.Name == "" || len(req.Node) == 0 {
		response.BadRequest(c, "api.task.target_null", nil)
		return
	}

	// 创建任务
	_, err = commonService.Insert(context.Background(), &req)
	if err != nil {
		logger.Error(fmt.Sprintf("AddTask %v", err))
		response.InternalServerError(c, "api.error", err)
		return
	}

	// 如果是定时任务，创建定时任务
	if req.ScheduledTasks {
		err := SchedulerService.CreateScheduledTask(c, &req)
		if err != nil {
			response.InternalServerError(c, "api.error", err)
			return
		}
	}

	response.Success(c, nil, "api.task.add_success")
}

// DeleteTaskRequest 删除任务请求
// 支持批量删除与是否删除资产
// ids: 任务ID列表
// delA: 是否同时删除资产
type DeleteTaskRequest struct {
	IDs  []string `json:"ids" binding:"required" example:"[\"507f1f77bcf86cd799439011\",\"507f1f77bcf86cd799439012\"]"`
	DelA bool     `json:"delA" binding:"omitempty" example:"false"`
}

// RetestTaskRequest 重测任务请求
type RetestTaskRequest struct {
	ID string `json:"id" binding:"required" example:"507f1f77bcf86cd799439011"`
}

// DeleteTask 删除任务
// @Summary      删除任务
// @Description  批量删除任务，支持可选删除其产生的资产，并清理调度与缓存
// @Tags         task
// @Accept       json
// @Produce      json
// @Param        data  body      DeleteTaskRequest  true  "删除参数"
// @Success      200  {object}  response.SuccessResponse
// @Failure      400  {object}  response.BadRequestResponse
// @Failure      500  {object}  response.InternalServerErrorResponse
// @Router       /api/task/delete [post]
func DeleteTask(c *gin.Context) {
	var req DeleteTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	if len(req.IDs) == 0 {
		response.BadRequest(c, "api.bad_request", fmt.Errorf("ids is required"))
		return
	}

	// 先按 _id 查真实存在的任务：以前直接把 len(req.IDs) 当成删除条数，
	// id 打错或者已经被删掉也照样报「已删除 N 个」（2026-11-28 实测）。
	exist, err := taskService.GetTasksByIDs(c, req.IDs)
	if err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	if len(exist) == 0 {
		response.NotFound(c, "api.error", fmt.Errorf("任务不存在"))
		return
	}
	existSet := make(map[string]bool, len(exist))
	for _, t := range exist {
		existSet[t.ID.Hex()] = true
	}
	hits := make([]string, 0, len(exist))
	missing := make([]string, 0)
	for _, id := range req.IDs {
		if existSet[id] {
			hits = append(hits, id)
		} else {
			missing = append(missing, id)
		}
	}
	if err := taskService.DeleteTasks(c, hits, req.DelA); err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	// 回真实删除条数：界面才能说「已删除 N 个」，而不是笼统的成功
	data := map[string]any{"deleted": len(hits)}
	if len(missing) > 0 {
		data["missing"] = missing
	}
	response.Success(c, data, "api.success")
}

// RetestTask 重测任务
// @Summary      重测任务
// @Description  重新创建扫描任务并重置进度
// @Tags         task
// @Accept       json
// @Produce      json
// @Param        data  body      RetestTaskRequest  true  "任务ID"
// @Success      200  {object}  response.SuccessResponse
// @Failure      400  {object}  response.BadRequestResponse
// @Failure      404  {object}  response.NotFoundResponse
// @Failure      500  {object}  response.InternalServerErrorResponse
// @Router       /api/task/retest [post]
func RetestTask(c *gin.Context) {
	var req RetestTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	if req.ID == "" {
		response.BadRequest(c, "api.bad_request", fmt.Errorf("id is required"))
		return
	}

	if err := taskService.RetestTask(c, req.ID); err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, nil, "api.success")
}

// StopTaskRequest 停止任务请求
type StopTaskRequest struct {
	IDs []string `json:"ids" binding:"required" example:"[\"507f1f77bcf86cd799439011\"]"`
}

// StartTaskRequest 开始任务请求
type StartTaskRequest struct {
	IDs []string `json:"ids" binding:"required" example:"[\"507f1f77bcf86cd799439011\"]"`
}

// StopTask 停止任务
// @Summary      停止任务
// @Description  批量停止任务，向节点广播停止并将状态置为2
// @Tags         task
// @Accept       json
// @Produce      json
// @Param        data  body      StopTaskRequest  true  "任务ID列表"
// @Success      200  {object}  response.SuccessResponse
// @Failure      400  {object}  response.BadRequestResponse
// @Failure      500  {object}  response.InternalServerErrorResponse
// @Router       /api/task/stop [post]
func StopTask(c *gin.Context) {
	var req StopTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	if len(req.IDs) == 0 {
		response.BadRequest(c, "api.bad_request", fmt.Errorf("ids is required"))
		return
	}
	// 同删除：先确认任务存在，否则「暂停成功」是假的（2026-11-28 实测）
	exist, err := taskService.GetTasksByIDs(c, req.IDs)
	if err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	if len(exist) == 0 {
		response.NotFound(c, "api.error", fmt.Errorf("任务不存在"))
		return
	}
	hits := make([]string, 0, len(exist))
	for _, t := range exist {
		hits = append(hits, t.ID.Hex())
	}
	if err := taskService.StopTasks(c, hits); err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, map[string]any{"updated": len(hits), "ids": hits}, "api.success")
}

// StartTask 开始任务（从暂停恢复）
// @Summary      开始任务
// @Description  批量开始任务，从暂停恢复继续扫描
// @Tags         task
// @Accept       json
// @Produce      json
// @Param        data  body      StartTaskRequest  true  "任务ID列表"
// @Success      200  {object}  response.SuccessResponse
// @Failure      400  {object}  response.BadRequestResponse
// @Failure      500  {object}  response.InternalServerErrorResponse
// @Router       /api/task/start [post]
func StartTask(c *gin.Context) {
	var req StartTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	if len(req.IDs) == 0 {
		response.BadRequest(c, "api.bad_request", fmt.Errorf("ids is required"))
		return
	}
	// 批量开始：逐个执行并把结果汇总，界面能说清「哪几个没起来、为什么」。
	started, failed := taskService.StartTasksReport(c, req.IDs)
	result := map[string]interface{}{
		"started":  started,
		"failed":   len(failed),
		"failures": failed,
	}
	if started == 0 && len(failed) > 0 {
		// 全失败沿用 400（保持「点开始没反应」时能被脚本/客户端识别）
		response.BadRequest(c, "api.error", fmt.Errorf("%s", firstFailureReason(failed)))
		return
	}
	response.Success(c, result, "api.success")
}

// firstFailureReason 取第一个失败原因（map 无序，但错误信息对用户足够）
func firstFailureReason(failed map[string]string) string {
	for _, v := range failed {
		return v
	}
	return ""
}

// ProgressInfoRequest 任务进度信息请求
type ProgressInfoRequest struct {
	ID        string `json:"id" binding:"required" example:"507f1f77bcf86cd799439011"`
	PageIndex int    `json:"pageIndex" binding:"omitempty,min=1" example:"1"`
	PageSize  int    `json:"pageSize" binding:"omitempty,min=1" example:"10"`
}

// ProgressInfo 获取任务进度信息
// @Summary      获取任务进度信息
// @Description  获取指定任务的详细进度信息，包括各扫描阶段的时间戳
// @Tags         task
// @Accept       json
// @Produce      json
// @Param        data  body      ProgressInfoRequest  true  "任务ID和分页参数"
// @Success      200  {object}  response.SuccessResponse{data=object{list=[]models.TaskProgress,total=int}}
// @Failure      400  {object}  response.BadRequestResponse
// @Failure      404  {object}  response.NotFoundResponse
// @Failure      500  {object}  response.InternalServerErrorResponse
// @Router       /api/task/progress/info [post]
func ProgressInfo(c *gin.Context) {
	var req ProgressInfoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	// 设置默认分页参数
	if req.PageIndex <= 0 {
		req.PageIndex = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 10
	}

	progressData, err := taskService.GetTaskProgress(c, req.ID, req.PageIndex, req.PageSize)
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}

	response.Success(c, progressData, "")
}

// GetAllTaskNames 获取所有任务名称
// @Summary      获取所有任务名称
// @Description  返回所有任务名称，按 id 倒序排列
// @Tags         task
// @Accept       json
// @Produce      json
// @Success      200  {object}  response.SuccessResponse{data=[]object{id=string,name=string}}
// @Failure      500  {object}  response.InternalServerErrorResponse
// @Router       /api/task/names [post]
func GetAllTaskNames(c *gin.Context) {
	tasks, err := taskService.GetAllTaskNames(c)
	if err != nil {
		logger.Error(fmt.Sprintf("GetAllTaskNames %v", err))
		response.InternalServerError(c, "api.error", err)
		return
	}

	// 构建返回数据，只包含 id 和 name
	result := make([]map[string]interface{}, 0, len(tasks))
	for _, t := range tasks {
		result = append(result, map[string]interface{}{
			"id":   t.ID.Hex(),
			"name": t.Name,
		})
	}

	response.Success(c, result, "")
}

func init() {
	taskService = task.NewService()
	commonService = common.NewService()
}

// DeleteByFilter 按当前筛选批量删除任务。
// 安全边界：必须给至少一个筛选条件（名字或状态），单次最多 200 条。
func DeleteByFilter(c *gin.Context) {
	var req models.ListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	if req.Search == "" && (req.Status == nil || *req.Status < 0) {
		response.BadRequest(c, "api.bad_request", fmt.Errorf("need search or status to avoid full delete"))
		return
	}
	n, err := taskService.DeleteByFilter(c, req.Search, req.Status)
	if err != nil {
		logger.Error(fmt.Sprintf("DeleteByFilter: %v", err))
		response.InternalServerError(c, "api.task.delete.error", err)
		return
	}
	response.Success(c, map[string]interface{}{"deleted": n}, "api.success")
}

// DispatchRejectStats 返回各道派发闸门的拒绝次数（进程内累计，重启归零）。
// 用途：外部集成反复撞闸时一眼看出来，不用翻日志。
func DispatchRejectStats(c *gin.Context) {
	response.Success(c, common.RejectStats(), "api.success")
}

// RecentRejections 返回最近 N 条派发拒绝记录（持久化，重启不丢）。
func RecentRejections(c *gin.Context) {
	limit, _ := strconv.ParseInt(c.DefaultQuery("limit", "10"), 10, 64)
	rows, err := common.RecentRejections(limit)
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, rows, "api.success")
}
