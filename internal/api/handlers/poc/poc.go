package poc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/services/poc"
	"github.com/Autumn-27/ScopeSentry/internal/utils/random"
	"github.com/gin-gonic/gin"
)

// DefaultPocSyncURL 默认 POC 源（上游 nuclei-templates 压缩包）。
// 服务端能直连 codeload.github.com 时无需再配 POC_SYNC_URL。
const DefaultPocSyncURL = "https://codeload.github.com/projectdiscovery/nuclei-templates/zip/refs/heads/main"

var pocService poc.Service

func init() {
	pocService = poc.NewService()
}

// GetPocList godoc
// @Summary 获取POC列表
// @Description 获取POC列表，支持分页和搜索
// @Tags POC管理
// @Accept json
// @Produce json
// @Param request body poc.PocListRequest true "请求参数"
// @Success 200 {object} response.Response{data=poc.PocListResponse}
// @Router /poc [post]
func GetPocList(c *gin.Context) {
	var req models.PocListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(fmt.Sprintf("GetPocList bad request%v", err))
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	result, err := pocService.GetPocList(c, &req)
	if err != nil {
		logger.Error(fmt.Sprintf("GetPocList error %v", err))
		response.InternalServerError(c, "api.error", err)
		return
	}

	response.Success(c, result, "api.success")
}

// GetPocDetail godoc
// @Summary 获取POC详情
// @Description 根据ID获取POC详情
// @Tags POC管理
// @Accept json
// @Produce json
// @Param request body poc.PocDetailRequest true "请求参数"
// @Success 200 {object} response.Response{data=poc.Poc}
// @Router /poc/detail [post]
func GetPocDetail(c *gin.Context) {
	var req models.PocDetailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	result, err := pocService.GetPocDetail(c, &req)
	if err != nil {
		// 查不到 → 404（此前一律 500，且错误里带 mongo: no documents in result）
		if errors.Is(err, poc.ErrPocNotFound) {
			response.NotFound(c, "api.not_found", err)
			return
		}
		response.InternalServerError(c, "api.error", err)
		return
	}

	response.Success(c, result, "api.success")
}

// UpdatePoc godoc
// @Summary 更新POC
// @Description 更新POC信息
// @Tags POC管理
// @Accept json
// @Produce json
// @Param request body poc.PocUpdateRequest true "请求参数"
// @Success 200 {object} response.Response
// @Router /poc/update [post]
func UpdatePoc(c *gin.Context) {
	var req models.PocUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	if err := pocService.UpdatePoc(c, &req); err != nil {
		switch {
		case errors.Is(err, poc.ErrInvalidTemplate):
			// 模板 YAML 写错是调用方参数问题，回 400 + 可读原因；以前是 500 + 原始 yaml 报错
			response.BadRequest(c, "api.bad_request", err)
		case errors.Is(err, poc.ErrPocNotFound):
			response.NotFound(c, "api.not_found", err)
		default:
			response.InternalServerError(c, "api.error", err)
		}
		return
	}

	response.Success(c, nil, "api.success")
}

// AddPoc godoc
// @Summary 添加POC
// @Description 添加新的POC
// @Tags POC管理
// @Accept json
// @Produce json
// @Param request body poc.PocAddRequest true "请求参数"
// @Success 200 {object} response.Response
// @Router /api/poc/add [post]
func AddPoc(c *gin.Context) {
	var req models.PocAddRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	if _, err := pocService.AddPoc(c.Request.Context(), &req); err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}

	response.Success(c, nil, "api.success")
}

// DeletePoc godoc
// @Summary 删除POC
// @Description 删除指定的POC
// @Tags POC管理
// @Accept json
// @Produce json
// @Param request body poc.PocDeleteRequest true "请求参数"
// @Success 200 {object} response.Response
// @Router /poc/delete [post]
func DeletePoc(c *gin.Context) {
	var req models.PocDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	deleted, err := pocService.DeletePoc(c, &req)
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	if deleted == 0 {
		// 这些 id 一个都不存在：以前照样回「成功」，界面弹「已删除」但列表没变
		response.NotFound(c, "api.poc.not_found", nil)
		return
	}

	response.Success(c, map[string]any{"deleted": deleted, "requested": len(req.IDs)}, "api.success")
}

// GetAllPocData godoc
// @Summary 获取所有POC数据
// @Description 获取所有POC数据，包含id、name、time、tags字段
// @Tags POC管理
// @Accept json
// @Produce json
// @Success 200 {object} response.Response{data=map[string]interface{}}
// @Router /poc/data/all [get]
func GetAllPocData(c *gin.Context) {
	result, err := pocService.GetAllPocData(context.Background())
	if err != nil {
		logger.Error(fmt.Sprintf("GetAllPocData error %v", err))
		response.InternalServerError(c, "api.error", err)
		return
	}

	response.Success(c, map[string]interface{}{
		"list": result,
	}, "api.success")
}

// SyncPoc 立即从上游仓库同步 POC（「立即同步」按钮 + 定时任务共用同一实现）。
// 请求体可选 {"url":"..."}；留空用环境变量 POC_SYNC_URL；再留空用内置默认源。
// @Summary      立即同步POC
// @Tags         POC管理
// @Accept       json
// @Produce      json
// @Success      200  {object}  response.SuccessResponse{data=models.PocImportResponse}
// @Router       /poc/data/sync [post]
func SyncPoc(c *gin.Context) {
	var req struct {
		URL string `json:"url"`
	}
	_ = c.ShouldBindJSON(&req)

	url := strings.TrimSpace(req.URL)
	if url == "" {
		url = strings.TrimSpace(os.Getenv("POC_SYNC_URL"))
	}
	if url == "" {
		url = DefaultPocSyncURL
	}

	// 异步执行：同步一次要下载+逐个文件入库（实测 ~25 秒），
	// 同步阻塞会让前端在整个过程里只能干等、拿不到任何进度。
	// 这里立刻返回，真实进度走 /poc/data/import/status 轮询。
	go func(u string) {
		defer func() {
			if r := recover(); r != nil {
				logger.Error(fmt.Sprintf("SyncPoc panic: %v", r))
			}
		}()
		if _, err := pocService.SyncFromURL(context.Background(), u); err != nil {
			logger.Error(fmt.Sprintf("SyncPoc background error %v", err))
		}
	}(url)

	response.Success(c, map[string]interface{}{"message": "sync started"}, "api.success")
}

// ImportPoc godoc
// @Summary 导入POC文件
// @Description 上传ZIP文件并导入POC
// @Tags POC管理
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "POC文件(ZIP格式)"
// @Success 200 {object} response.Response{data=models.PocImportResponse}
// @Router /poc/data/import [post]
func ImportPoc(c *gin.Context) {
	// 获取上传的文件
	file, err := c.FormFile("file")
	if err != nil {
		logger.Error(fmt.Sprintf("ImportPoc get file error %v", err))
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	// 检查文件扩展名
	if !strings.HasSuffix(strings.ToLower(file.Filename), ".zip") {
		response.BadRequest(c, "api.bad_request", fmt.Errorf("file must be zip format"))
		return
	}

	// 检查文件名安全性
	if strings.Contains(file.Filename, "..") || strings.Contains(file.Filename, "/") || strings.Contains(file.Filename, "\\") {
		response.BadRequest(c, "api.bad_request", fmt.Errorf("unsafe filename"))
		return
	}

	// 使用系统临时目录
	tempDir := os.TempDir()
	fileName := random.GenerateString(5) + ".zip"
	filePath := filepath.Join(tempDir, fileName)

	if err := c.SaveUploadedFile(file, filePath); err != nil {
		logger.Error(fmt.Sprintf("ImportPoc save file error %v", err))
		response.InternalServerError(c, "api.error", err)
		return
	}

	// overwrite 默认 true：导入即同步（同 id 内容变化会更新，保留 _id）
	overwrite := true
	if v := strings.TrimSpace(c.PostForm("overwrite")); v != "" {
		overwrite = v != "false" && v != "0"
	}

	// 异步处理导入
	go func() {
		result, err := pocService.ImportPocWithOptions(c, filePath, overwrite)
		if err != nil {
			logger.Error(fmt.Sprintf("ImportPoc process error %v", err))
		} else {
			logger.Info(fmt.Sprintf("ImportPoc success: %+v", result))
		}
	}()

	response.Success(c, map[string]interface{}{
		"message": "Importing in progress",
	}, "api.success")
}

// ImportPocStatus 返回当前（或最近一次）POC 导入进度。
// 前端点完「导入」后轮询它，把黑盒变成进度条。
// @Summary      查询POC导入进度
// @Tags         POC管理
// @Produce      json
// @Router       /poc/data/import/status [get]
func ImportPocStatus(c *gin.Context) {
	snap := poc.GlobalImportProgress.Snapshot()
	response.Success(c, snap, "api.success")
}
