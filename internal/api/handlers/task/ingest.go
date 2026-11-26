package task

import (
	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/services/task/task"
	"github.com/gin-gonic/gin"
)

// IngestToProject 项目优先接入：把一批目标接进项目，并建一条绑定该项目的任务。
//
// 给外部集成（HuntDeck 等）用：它们手里只有「项目 + 新资产」，
// 不该自己判断项目是否存在、也不该自己拼任务名。
//
// @Router /api/task/ingest [post]
func IngestToProject(c *gin.Context) {
	var req task.IngestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	res, err := taskService.IngestToProject(c, req)
	if err != nil {
		response.BadRequest(c, "api.error", err)
		return
	}
	response.Success(c, res, "api.success")
}
