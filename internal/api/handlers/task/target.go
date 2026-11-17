package task

import (
	"errors"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	svc "github.com/Autumn-27/ScopeSentry/internal/services/task/task"
	"github.com/gin-gonic/gin"
)

// PauseTarget 暂停（跳过）任务里的某个目标（issue #277）
// @Summary 暂停任务目标
// @Description 未被节点领取的目标会从队列摘除并减少 taskNum；已领取的目标只记录状态
// @Tags 任务
// @Accept json
// @Produce json
// @Router /api/task/target/pause [post]
func PauseTarget(c *gin.Context) {
	var req models.TaskTargetControlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	if err := taskService.PauseTarget(c, req.TaskID, req.Target); err != nil {
		respondTargetControlError(c, err)
		return
	}
	response.Success(c, nil, "api.success")
}

// ResumeTarget 恢复被暂停的目标
// @Summary 恢复任务目标
// @Tags 任务
// @Accept json
// @Produce json
// @Router /api/task/target/resume [post]
func ResumeTarget(c *gin.Context) {
	var req models.TaskTargetControlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	if err := taskService.ResumeTarget(c, req.TaskID, req.Target); err != nil {
		respondTargetControlError(c, err)
		return
	}
	response.Success(c, nil, "api.success")
}

func respondTargetControlError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, svc.ErrTaskInvalid):
		response.BadRequest(c, "api.bad_request", err)
	case errors.Is(err, svc.ErrTaskNotFound):
		response.NotFound(c, "api.not_found", err)
	case errors.Is(err, svc.ErrTaskFinished):
		response.BadRequest(c, "api.bad_request", err)
	default:
		response.InternalServerError(c, "api.error", err)
	}
}
