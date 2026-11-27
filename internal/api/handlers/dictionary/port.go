package dictionary

import (
	"errors"
	"fmt"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	dictservice "github.com/Autumn-27/ScopeSentry/internal/services/dictionary"
	"github.com/gin-gonic/gin"
)

var portService dictservice.PortService

func init() {
	portService = dictservice.NewPortService()
}

// PortDataRequest 请求体
// 用于 /data 分页与搜索
// swagger:ignore
// 仅用于绑定
type PortDataRequest struct {
	PageIndex int    `json:"pageIndex"`
	PageSize  int    `json:"pageSize"`
	Search    string `json:"search"`
}

// GetPortData 获取端口字典数据
// @Summary 获取端口字典
// @Tags 字典管理
// @Accept json
// @Produce json
// @Param request body PortDataRequest true "请求参数"
// @Success 200 {object} response.Response{data=object{list=[]models.PortDoc,total=int}}
// @Failure 400 {object} response.Response
// @Failure 500 {object} response.Response
// @Security ApiKeyAuth
// @Router /api/dictionary/port/data [post]
func GetPortData(c *gin.Context) {
	var req PortDataRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	if req.PageIndex <= 0 {
		req.PageIndex = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 10
	}

	list, total, err := portService.Get(c, req.Search, req.PageIndex, req.PageSize)
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	// 空结果回 []，别回 null：前端直接 res.data.list.map() 时 null 会炸（同 export/records 那条不变量）
	if list == nil {
		list = []models.PortDoc{}
	}
	response.Success(c, gin.H{"list": list, "total": total}, "api.success")
}

// UpgradePortDict 更新端口字典
// @Summary 更新端口字典
// @Tags 字典管理
// @Accept json
// @Produce json
// @Param request body models.PortDoc true "请求参数"
// @Success 200 {object} response.Response
// @Failure 400 {object} response.Response
// @Failure 500 {object} response.Response
// @Security ApiKeyAuth
// @Router /api/dictionary/port/upgrade [post]
func UpgradePortDict(c *gin.Context) {
	var req models.UpdatePort
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	if req.ID == "" {
		response.BadRequest(c, "api.bad_request", fmt.Errorf("id is required"))
		return
	}
	if err := portService.Update(c, req.ID, req.Name, req.Value); err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, gin.H{"code": 200, "message": "SensitiveRule updated successfully"}, "api.success")
}

// AddPortDict 新增端口字典
// @Summary 新增端口字典
// @Tags 字典管理
// @Accept json
// @Produce json
// @Param request body models.Port true "请求参数"
// @Success 200 {object} response.Response
// @Failure 400 {object} response.Response
// @Failure 500 {object} response.Response
// @Security ApiKeyAuth
// @Router /api/dictionary/port/add [post]
func AddPortDict(c *gin.Context) {
	var req models.Port
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	if err := portService.Add(c, req.Name, req.Value); err != nil {
		// 空名字/空值要给出具体原因：都回「请求参数错误」的话，前端只能猜是哪一项。
		switch {
		case errors.Is(err, dictservice.ErrPortNameRequired):
			response.BadRequest(c, "api.required.name", nil)
		case errors.Is(err, dictservice.ErrPortValueRequired):
			response.BadRequest(c, "api.required.value", nil)
		default:
			response.BadRequest(c, "api.bad_request", err)
		}
		return
	}
	response.Success(c, gin.H{"code": 200, "message": "Port dict added successfully"}, "api.success")
}

// DeletePortDict 删除端口字典
// @Summary 删除端口字典
// @Tags 字典管理
// @Accept json
// @Produce json
// @Param request body object{ids=[]string} true "请求参数"
// @Success 200 {object} response.Response
// @Failure 400 {object} response.Response
// @Failure 500 {object} response.Response
// @Security ApiKeyAuth
// @Router /api/dictionary/port/delete [post]
func DeletePortDict(c *gin.Context) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	if len(req.IDs) == 0 {
		response.BadRequest(c, "api.bad_request", fmt.Errorf("ids cannot be empty"))
		return
	}
	deleted, err := portService.Delete(c, req.IDs)
	if err != nil {
		logger.Error(err.Error())
		response.InternalServerError(c, "api.error", err)
		return
	}
	if deleted == 0 {
		// 不存在的 id 以前回 "Port dict deleted successfully"（2026-11-28 实测）
		response.NotFound(c, "api.dict.port.not_found", nil)
		return
	}
	response.Success(c, gin.H{"code": 200, "deleted": deleted, "requested": len(req.IDs)}, "api.success")
}
