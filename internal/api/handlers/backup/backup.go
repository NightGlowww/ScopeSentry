// backup-------------------------------------
// @file      : backup.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2026/11/18 01:30
// -------------------------------------------

package backup

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	backupSvc "github.com/Autumn-27/ScopeSentry/internal/services/backup"
	"github.com/gin-gonic/gin"
)

// maxUploadRequestBytes 上传请求体上限，和 service 层限制保持一致。
const maxUploadRequestBytes int64 = 8 << 30

var service backupSvc.Service

func init() {
	service = backupSvc.NewService()
}

// Create 创建全量备份
// @Summary 创建备份
// @Tags backup
// @Accept json
// @Produce json
// @Param request body models.BackupCreateRequest false "备份备注"
// @Success 200 {object} models.BackupCreateResponse
// @Router /api/backup/create [post]
func Create(c *gin.Context) {
	var req models.BackupCreateRequest
	if c.Request != nil && c.Request.Body != nil && c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.BadRequest(c, "api.bad_request", err)
			return
		}
	}
	info, err := service.Create(c.Request.Context(), req.Note)
	if err != nil {
		handleError(c, err)
		return
	}
	response.Success(c, models.BackupCreateResponse{
		Name:   info.Name,
		Size:   info.Size,
		SHA256: info.SHA256,
	}, "api.success")
}

// List 备份列表
// @Summary 备份列表
// @Tags backup
// @Produce json
// @Success 200 {array} models.BackupInfo
// @Router /api/backup/list [get]
func List(c *gin.Context) {
	infos, err := service.List()
	if err != nil {
		handleError(c, err)
		return
	}
	response.Success(c, infos, "api.success")
}

// Status 当前任务状态
// @Summary 备份任务状态
// @Tags backup
// @Produce json
// @Success 200 {object} models.BackupStatus
// @Router /api/backup/status [get]
func Status(c *gin.Context) {
	response.Success(c, service.Status(), "api.success")
}

// Download 下载备份包
// @Summary 下载备份
// @Tags backup
// @Produce application/octet-stream
// @Param name path string true "备份文件名"
// @Router /api/backup/download/{name} [get]
func Download(c *gin.Context) {
	name := c.Param("name")
	full, err := service.Path(name)
	if err != nil {
		handleError(c, err)
		return
	}
	info, statErr := os.Stat(full)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			response.NotFound(c, "api.not_found", statErr)
			return
		}
		response.InternalServerError(c, "api.server_error", statErr)
		return
	}
	if !info.Mode().IsRegular() {
		response.NotFound(c, "api.not_found", fmt.Errorf("%s is not a regular file", name))
		return
	}
	c.FileAttachment(full, name)
}

// Delete 删除备份包
// @Summary 删除备份
// @Tags backup
// @Param name path string true "备份文件名"
// @Router /api/backup/{name} [delete]
func Delete(c *gin.Context) {
	if err := service.Delete(c.Param("name")); err != nil {
		handleError(c, err)
		return
	}
	response.Success(c, nil, "api.success")
}

// Restore 从备份包恢复
// @Summary 恢复备份
// @Tags backup
// @Accept json
// @Produce json
// @Param request body models.BackupRestoreRequest true "备份文件名与确认字段"
// @Success 200 {object} models.BackupRestoreResult
// @Router /api/backup/restore [post]
func Restore(c *gin.Context) {
	var req models.BackupRestoreRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	if req.Confirm != "RESTORE" {
		response.BadRequest(c, "api.bad_request", backupSvc.ErrConfirmRequired)
		return
	}
	result, err := service.Restore(c.Request.Context(), req.Name)
	if err != nil {
		handleError(c, err)
		return
	}
	response.Success(c, result, "api.success")
}

// Upload 上传外部备份包
// @Summary 上传备份
// @Tags backup
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "备份包"
// @Param sha256 formData string false "期望的 sha256"
// @Router /api/backup/upload [post]
func Upload(c *gin.Context) {
	if c.Request != nil && c.Request.Body != nil {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadRequestBytes)
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	f, err := fileHeader.Open()
	if err != nil {
		response.InternalServerError(c, "api.server_error", err)
		return
	}
	defer f.Close()

	info, err := service.Upload(c.Request.Context(), fileHeader.Filename, f, c.PostForm("sha256"))
	if err != nil {
		handleError(c, err)
		return
	}
	response.Success(c, models.BackupCreateResponse{
		Name:   info.Name,
		Size:   info.Size,
		SHA256: info.SHA256,
	}, "api.success")
}

// handleError 统一错误映射：非法名字/冲突/校验失败 → 400，缺失 → 404，其余 → 500。
func handleError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, backupSvc.ErrInvalidName):
		response.BadRequest(c, "api.bad_request", err)
	case errors.Is(err, backupSvc.ErrNotFound):
		response.NotFound(c, "api.not_found", err)
	case errors.Is(err, backupSvc.ErrJobRunning),
		errors.Is(err, backupSvc.ErrConfirmRequired),
		errors.Is(err, backupSvc.ErrAlreadyExists),
		errors.Is(err, backupSvc.ErrChecksumMismatch),
		errors.Is(err, backupSvc.ErrManifestInvalid),
		errors.Is(err, backupSvc.ErrMongoDown):
		response.BadRequest(c, "api.bad_request", err)
	default:
		response.InternalServerError(c, "api.server_error", err)
	}
}
