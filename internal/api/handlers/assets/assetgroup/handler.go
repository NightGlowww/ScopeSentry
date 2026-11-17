package assetgroup

import (
	"errors"
	"fmt"
	"sync"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	svc "github.com/Autumn-27/ScopeSentry/internal/services/assets/assetgroup"
	"github.com/gin-gonic/gin"
)

var (
	agService     svc.Service
	agServiceOnce sync.Once
)

func getService() svc.Service {
	agServiceOnce.Do(func() { agService = svc.NewService() })
	return agService
}

func respondError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, svc.ErrGroupExists):
		response.BadRequestWithError(c, "api.bad_request", err)
	case errors.Is(err, svc.ErrGroupNotFound), errors.Is(err, svc.ErrScopeNotFound):
		response.NotFound(c, "api.not_found", err)
	case errors.Is(err, svc.ErrInvalidInput):
		response.BadRequestWithError(c, "api.bad_request", err)
	default:
		logger.Error(fmt.Sprintf("asset group: %v", err))
		response.InternalServerError(c, "api.error", err)
	}
}

// ListGroups 集团列表（含 scopeCount）
// @Summary 资产集团列表
// @Tags 资产
// @Router /api/assets/asset_group/list [post]
func ListGroups(c *gin.Context) {
	items, err := getService().ListGroups(c.Request.Context())
	if err != nil {
		respondError(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "total": len(items)}, "api.success")
}

// AddGroup 新建集团
// @Summary 新建资产集团
// @Tags 资产
// @Router /api/assets/asset_group/add [post]
func AddGroup(c *gin.Context) {
	var req models.AssetGroupAddRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	id, err := getService().CreateGroup(c.Request.Context(), &req)
	if err != nil {
		respondError(c, err)
		return
	}
	response.Success(c, gin.H{"id": id}, "api.success")
}

// EditGroup 编辑集团
// @Summary 编辑资产集团
// @Tags 资产
// @Router /api/assets/asset_group/edit [post]
func EditGroup(c *gin.Context) {
	var req models.AssetGroupEditRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	if err := getService().UpdateGroup(c.Request.Context(), &req); err != nil {
		respondError(c, err)
		return
	}
	response.Success(c, nil, "api.success")
}

// DeleteGroup 删除集团（dissolve/transfer）
// @Summary 删除资产集团
// @Tags 资产
// @Router /api/assets/asset_group/delete [post]
func DeleteGroup(c *gin.Context) {
	var req models.AssetGroupDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	if err := getService().DeleteGroup(c.Request.Context(), &req); err != nil {
		respondError(c, err)
		return
	}
	response.Success(c, nil, "api.success")
}

// ReorderGroups 重排集团
// @Summary 资产集团排序
// @Tags 资产
// @Router /api/assets/asset_group/reorder [post]
func ReorderGroups(c *gin.Context) {
	var req models.AssetGroupReorderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	if err := getService().ReorderGroups(c.Request.Context(), req.OrderIDs); err != nil {
		respondError(c, err)
		return
	}
	response.Success(c, nil, "api.success")
}

// ListScopes 地契列表
// @Summary 资产组地契列表
// @Tags 资产
// @Router /api/assets/asset_scope/list [post]
func ListScopes(c *gin.Context) {
	var req struct {
		GroupID   string `json:"groupId"`
		Search    string `json:"search"`
		PageIndex int    `json:"pageIndex"`
		PageSize  int    `json:"pageSize"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	items, total, err := getService().ListScopes(c.Request.Context(), req.GroupID, req.Search, req.PageIndex, req.PageSize)
	if err != nil {
		respondError(c, err)
		return
	}
	response.Success(c, gin.H{"list": items, "total": total}, "api.success")
}

// AddScope 新建地契
// @Summary 新建资产组地契
// @Tags 资产
// @Router /api/assets/asset_scope/add [post]
func AddScope(c *gin.Context) {
	var req models.AssetScopeAddRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	id, err := getService().CreateScope(c.Request.Context(), &req)
	if err != nil {
		respondError(c, err)
		return
	}
	response.Success(c, gin.H{"id": id}, "api.success")
}

// UpdateScope 编辑地契
// @Summary 编辑资产组地契
// @Tags 资产
// @Router /api/assets/asset_scope/update [post]
func UpdateScope(c *gin.Context) {
	var req models.AssetScopeUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	if err := getService().UpdateScope(c.Request.Context(), &req); err != nil {
		respondError(c, err)
		return
	}
	response.Success(c, nil, "api.success")
}

// DeleteScopes 批量删除地契
// @Summary 删除资产组地契
// @Tags 资产
// @Router /api/assets/asset_scope/delete [post]
func DeleteScopes(c *gin.Context) {
	var req models.AssetScopeIDsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	if err := getService().DeleteScopes(c.Request.Context(), req.IDs); err != nil {
		respondError(c, err)
		return
	}
	response.Success(c, nil, "api.success")
}

// MoveScopes 批量移动地契
// @Summary 移动资产组地契
// @Tags 资产
// @Router /api/assets/asset_scope/batch_move_group [post]
func MoveScopes(c *gin.Context) {
	var req models.AssetScopeMoveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	if err := getService().MoveScopes(c.Request.Context(), req.IDs, req.GroupID); err != nil {
		respondError(c, err)
		return
	}
	response.Success(c, nil, "api.success")
}

// BindEnterprise 绑定企业
// @Summary 绑定企业主体
// @Tags 资产
// @Router /api/assets/asset_scope/bind_enterprise [post]
func BindEnterprise(c *gin.Context) {
	var req models.AssetScopeBindEnterpriseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	if err := getService().BindEnterprise(c.Request.Context(), req.ID, req.Enterprise); err != nil {
		respondError(c, err)
		return
	}
	response.Success(c, nil, "api.success")
}

// ClearUpdateErrors 清空同步错误
// @Summary 清空地契同步错误
// @Tags 资产
// @Router /api/assets/asset_scope/clear_update_error [post]
func ClearUpdateErrors(c *gin.Context) {
	var req models.AssetScopeIDsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	if err := getService().ClearUpdateErrors(c.Request.Context(), req.IDs); err != nil {
		respondError(c, err)
		return
	}
	response.Success(c, nil, "api.success")
}
