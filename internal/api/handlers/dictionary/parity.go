// handlers-------------------------------------
// @file      : parity.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2026/11/18 03:20
// -------------------------------------------

package dictionary

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	dictservice "github.com/Autumn-27/ScopeSentry/internal/services/dictionary"
	"github.com/gin-gonic/gin"
)

// writeDictError 把服务层哨兵错误映射到 HTTP 状态：
// 参数类 → 400，不存在 → 404，其余 → 500。
func writeDictError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, dictservice.ErrInvalidKind),
		errors.Is(err, dictservice.ErrInvalidName),
		errors.Is(err, dictservice.ErrDictionaryExists),
		errors.Is(err, dictservice.ErrPageSizeTooLarge),
		errors.Is(err, dictservice.ErrTooManyLines),
		errors.Is(err, dictservice.ErrEmptyLines),
		errors.Is(err, dictservice.ErrLineTooLong),
		errors.Is(err, dictservice.ErrEmptyKeyword),
		errors.Is(err, dictservice.ErrInvalidUploadMode),
		errors.Is(err, dictservice.ErrUploadTooLarge):
		response.BadRequest(c, "api.bad_request", err)
	case errors.Is(err, dictservice.ErrDictionaryNotFound),
		errors.Is(err, dictservice.ErrUploadNotFound):
		response.NotFound(c, "api.not_found", err)
	default:
		response.InternalServerError(c, "api.error", err)
	}
}

// List 字典列表（对标 ARL-Next dictionary.py 的列表）
//
// @Summary      字典列表
// @Description  按 kind 分页列出字典；pageSize 上限 200，超出 400
// @Tags         字典管理
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        request  body      models.DictionaryListRequest  true  "查询条件"
// @Success      200      {object}  response.SuccessResponse{data=models.DictionaryListResult}
// @Failure      400      {object}  response.BadRequestResponse
// @Failure      500      {object}  response.InternalServerErrorResponse
// @Router       /api/dictionary/list [post]
func ListDictionaries(c *gin.Context) {
	var req models.DictionaryListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	result, err := manageService.ListPaged(c.Request.Context(), req.Kind, req.Search, req.PageIndex, req.PageSize)
	if err != nil {
		writeDictError(c, err)
		return
	}
	response.Success(c, result, "api.success")
}

// Preview 预览字典前 200 行
//
// @Summary      字典预览
// @Tags         字典管理
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        request  body      models.DictionaryPreviewRequest  true  "kind / name / limit"
// @Success      200      {object}  response.SuccessResponse{data=models.DictionaryPreviewResult}
// @Failure      400      {object}  response.BadRequestResponse
// @Failure      404      {object}  response.NotFoundResponse
// @Router       /api/dictionary/preview [post]
func Preview(c *gin.Context) {
	var req models.DictionaryPreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	result, err := manageService.Preview(c.Request.Context(), req.Kind, req.Name, req.Limit)
	if err != nil {
		writeDictError(c, err)
		return
	}
	response.Success(c, result, "api.success")
}

// Search 在单个字典内搜行
//
// @Summary      字典行搜索
// @Tags         字典管理
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        request  body      models.DictionarySearchRequest  true  "kind / name / keyword / limit"
// @Success      200      {object}  response.SuccessResponse{data=models.DictionarySearchResult}
// @Router       /api/dictionary/search [post]
func Search(c *gin.Context) {
	var req models.DictionarySearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	result, err := manageService.SearchLines(c.Request.Context(), req.Kind, req.Name, req.Keyword, req.Limit)
	if err != nil {
		writeDictError(c, err)
		return
	}
	response.Success(c, result, "api.success")
}

// Append 追加行（自动去重）
//
// @Summary      字典追加
// @Tags         字典管理
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        request  body      models.DictionaryAppendRequest  true  "kind / name / lines"
// @Success      200      {object}  response.SuccessResponse{data=models.DictionaryAppendResult}
// @Router       /api/dictionary/append [post]
func Append(c *gin.Context) {
	var req models.DictionaryAppendRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	result, err := manageService.Append(c.Request.Context(), req.Kind, req.Name, req.Lines)
	if err != nil {
		writeDictError(c, err)
		return
	}
	response.Success(c, result, "api.success")
}

// DeleteEntries 删除指定行
//
// @Summary      删除字典行
// @Tags         字典管理
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        request  body      models.DictionaryDeleteEntriesRequest  true  "kind / name / entries"
// @Success      200      {object}  response.SuccessResponse{data=models.DictionaryDeleteEntriesResult}
// @Router       /api/dictionary/delete_entries [post]
func DeleteEntries(c *gin.Context) {
	var req models.DictionaryDeleteEntriesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	result, err := manageService.DeleteEntries(c.Request.Context(), req.Kind, req.Name, req.Entries)
	if err != nil {
		writeDictError(c, err)
		return
	}
	response.Success(c, result, "api.success")
}

// DeleteFile 删除整个字典
//
// @Summary      删除字典
// @Tags         字典管理
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        request  body      models.DictionaryDeleteFileRequest  true  "kind / name"
// @Success      200      {object}  response.SuccessResponse{data=models.DictionaryDeleteFileResult}
// @Router       /api/dictionary/delete_file [post]
func DeleteFile(c *gin.Context) {
	var req models.DictionaryDeleteFileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	result, err := manageService.DeleteFileByName(c.Request.Context(), req.Kind, req.Name)
	if err != nil {
		writeDictError(c, err)
		return
	}
	response.Success(c, result, "api.success")
}

// Download 按 kind + name 下载字典内容
//
// @Summary      下载字典
// @Tags         字典管理
// @Produce      application/octet-stream
// @Security     ApiKeyAuth
// @Param        kind  query  string  true  "字典类型"
// @Param        name  query  string  true  "字典名"
// @Success      200
// @Router       /api/dictionary/download [get]
func DownloadByKind(c *gin.Context) {
	kind := c.Query("kind")
	name := c.Query("name")
	if kind == "" || name == "" {
		response.BadRequest(c, "api.bad_request", fmt.Errorf("kind and name are required"))
		return
	}

	reader, length, filename, err := manageService.OpenDownload(c.Request.Context(), kind, name)
	if err != nil {
		writeDictError(c, err)
		return
	}
	defer reader.Close()

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	c.DataFromReader(http.StatusOK, length, "application/octet-stream", reader, nil)
}

// Create 新建字典
//
// @Summary      新建字典
// @Tags         字典管理
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        request  body      models.DictionaryCreateRequest  true  "kind / name / content?"
// @Success      200      {object}  response.SuccessResponse{data=models.DictionaryCreateResult}
// @Router       /api/dictionary/create [post]
func CreateDictionary(c *gin.Context) {
	var req models.DictionaryCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	result, err := manageService.CreateByKind(c.Request.Context(), req.Kind, req.Name, req.Content)
	if err != nil {
		writeDictError(c, err)
		return
	}
	response.Success(c, result, "api.success")
}

// uploadService 大文件上传服务（包级单例，任务表跨请求共享）
var uploadService dictservice.UploadService

func init() {
	uploadService = dictservice.NewUploadService()
}

// maxUploadRequestBytes 整个 multipart 请求体上限（文件 200MB + 表单/边界余量）
const maxUploadRequestBytes = int64(dictservice.MaxUploadBytes) + (8 << 20)

// UploadLarge 大文件上传：流式落临时文件，后台解析，返回 taskId
//
// @Summary      大文件上传字典
// @Description  单文件上限 200MB；流式写临时文件，后台按行去重入库，进度用 upload_status 查
// @Tags         字典管理
// @Accept       mpfd
// @Produce      json
// @Security     ApiKeyAuth
// @Param        kind  formData  string  true  "字典类型"
// @Param        name  formData  string  true  "字典名"
// @Param        mode  formData  string  false "create（默认）/ append"
// @Param        file  formData  file    true  "字典文件"
// @Success      200   {object}  response.SuccessResponse{data=models.DictionaryUploadTask}
// @Router       /api/dictionary/upload_large [post]
func UploadLarge(c *gin.Context) {
	// 先兜住请求体，避免超大请求把磁盘/内存打满
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadRequestBytes)

	fileHeader, err := c.FormFile("file")
	if err != nil {
		response.BadRequest(c, "api.bad_request", fmt.Errorf("missing file: %w", err))
		return
	}
	kind := c.PostForm("kind")
	name := c.PostForm("name")
	mode := c.PostForm("mode")

	opened, err := fileHeader.Open()
	if err != nil {
		response.BadRequest(c, "api.bad_request", fmt.Errorf("open upload file: %w", err))
		return
	}
	defer opened.Close()

	task, err := uploadService.Start(c.Request.Context(), dictservice.UploadRequest{
		Kind:       kind,
		Name:       name,
		Mode:       mode,
		Reader:     opened,
		TotalBytes: fileHeader.Size,
	})
	if err != nil {
		writeDictError(c, err)
		return
	}
	logger.Info(fmt.Sprintf("dictionary upload task %s accepted: kind=%s name=%s lines=%d", task.TaskID, task.Kind, task.Name, task.TotalLines))
	response.Success(c, task, "api.success")
}

// UploadStatus 查询上传任务进度
//
// @Summary      上传任务进度
// @Tags         字典管理
// @Produce      json
// @Security     ApiKeyAuth
// @Param        taskId  query  string  true  "任务 ID"
// @Success      200     {object}  response.SuccessResponse{data=models.DictionaryUploadTask}
// @Router       /api/dictionary/upload_status [get]
func UploadStatus(c *gin.Context) {
	taskID := c.Query("taskId")
	task, err := uploadService.Status(c.Request.Context(), taskID)
	if err != nil {
		writeDictError(c, err)
		return
	}
	response.Success(c, task, "api.success")
}

// ---- dict_cdn / dict_brute：ARL-Next 的 cdn_dict.py / brute_dict.py ----

// CDNList CDN 字典列表（固定 kind=cdn）
//
// @Summary      CDN 字典列表
// @Tags         字典管理
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        request  body      models.DictionaryListRequest  false  "查询条件"
// @Success      200      {object}  response.SuccessResponse{data=models.DictionaryListResult}
// @Router       /api/dict_cdn/list [post]
func CDNList(c *gin.Context) {
	listByFixedKind(c, models.DictKindCDN)
}

// CDNSave 保存 CDN 字典（存在覆盖、不存在新建）
//
// @Summary      保存 CDN 字典
// @Tags         字典管理
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        request  body      models.DictionarySaveRequest  true  "name / content"
// @Success      200      {object}  response.SuccessResponse{data=models.DictionaryCreateResult}
// @Router       /api/dict_cdn/save [post]
func CDNSave(c *gin.Context) {
	saveByFixedKind(c, models.DictKindCDN)
}

// BruteList 爆破字典列表（固定 kind=brute）
//
// @Summary      爆破字典列表
// @Tags         字典管理
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        request  body      models.DictionaryListRequest  false  "查询条件"
// @Success      200      {object}  response.SuccessResponse{data=models.DictionaryListResult}
// @Router       /api/dict_brute/list [post]
func BruteList(c *gin.Context) {
	listByFixedKind(c, models.DictKindBrute)
}

// BruteSave 保存爆破字典（存在覆盖、不存在新建）
//
// @Summary      保存爆破字典
// @Tags         字典管理
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        request  body      models.DictionarySaveRequest  true  "name / content"
// @Success      200      {object}  response.SuccessResponse{data=models.DictionaryCreateResult}
// @Router       /api/dict_brute/save [post]
func BruteSave(c *gin.Context) {
	saveByFixedKind(c, models.DictKindBrute)
}

// listByFixedKind dict_cdn / dict_brute 共用：kind 固定在路由上，请求体只带搜索与分页。
func listByFixedKind(c *gin.Context, kind string) {
	var req models.DictionaryListRequest
	// 允许空 body：前端初次进页面可能不带参数
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.BadRequest(c, "api.bad_request", err)
			return
		}
	}
	result, err := manageService.ListPaged(c.Request.Context(), kind, req.Search, req.PageIndex, req.PageSize)
	if err != nil {
		writeDictError(c, err)
		return
	}
	response.Success(c, result, "api.success")
}

// saveByFixedKind dict_cdn / dict_brute 共用保存逻辑。
func saveByFixedKind(c *gin.Context, kind string) {
	var req models.DictionarySaveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	result, err := manageService.SaveByKind(c.Request.Context(), kind, req.Name, req.Content, req.Lines)
	if err != nil {
		writeDictError(c, err)
		return
	}
	response.Success(c, result, "api.success")
}
