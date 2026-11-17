package dataview

import (
	"errors"
	"fmt"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	svc "github.com/Autumn-27/ScopeSentry/internal/services/assets/dataview"
	"github.com/gin-gonic/gin"
)

var service = svc.NewService()

func fail(c *gin.Context, err error) {
	if errors.Is(err, svc.ErrInvalidIDs) {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	logger.Error(fmt.Sprintf("dataview: %v", err))
	response.InternalServerError(c, "api.error", err)
}

// ListFileLeak 文件泄露列表
// @Router /api/assets/fileleak/list [post]
func ListFileLeak(c *gin.Context) {
	var req models.FileLeakListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	rows, total, err := service.ListFileLeak(c.Request.Context(), req)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"list": rows, "total": total}, "api.success")
}

// DeleteFileLeak 删除文件泄露结果
// @Router /api/assets/fileleak/delete [post]
func DeleteFileLeak(c *gin.Context) {
	var req models.DataViewDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	deleted, err := service.DeleteFileLeak(c.Request.Context(), req.IDs)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": deleted}, "api.success")
}

// ListNpoc NPoC 服务列表
// @Router /api/assets/npoc_service/list [post]
func ListNpoc(c *gin.Context) {
	var req models.NpocServiceListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	rows, total, err := service.ListNpoc(c.Request.Context(), req)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"list": rows, "total": total}, "api.success")
}

// DeleteNpoc 删除 NPoC 服务结果
// @Router /api/assets/npoc_service/delete [post]
func DeleteNpoc(c *gin.Context) {
	var req models.DataViewDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	deleted, err := service.DeleteNpoc(c.Request.Context(), req.IDs)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": deleted}, "api.success")
}
