package task

import (
	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/gin-gonic/gin"
)

type assignProjectRequest struct {
	IDs       []string `json:"ids"`
	ProjectID string   `json:"projectId"`
}

// AssignProject 任务归属补录：把已有任务挂到项目下（projectId 传空 = 解除归属）。
func AssignProject(c *gin.Context) {
	var req assignProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.invalidParams", err)
		return
	}
	n, err := taskService.AssignProject(c, req.IDs, req.ProjectID)
	if err != nil {
		response.BadRequest(c, "api.task.assignProjectFailed", err)
		return
	}
	response.Success(c, gin.H{"updated": n}, "api.success")
}
