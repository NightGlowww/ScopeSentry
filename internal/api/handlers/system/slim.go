package system

import (
	"errors"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/services/maintenance"
	"github.com/gin-gonic/gin"
)

// SlimStorageRequest 大字段瘦身请求
type SlimStorageRequest struct {
	Collection string `json:"collection"`
	KeepBytes  int    `json:"keepBytes"`
	Limit      int    `json:"limit"`
	// DryRun 不传时按 true 处理：这个操作不可逆，默认必须是「先看一眼」
	DryRun  *bool  `json:"dryRun"`
	Confirm string `json:"confirm"`
	// AfterID 续跑游标：传上一批返回的 nextAfterId
	AfterID string `json:"afterId"`
}

// SlimStorage 处理 POST /api/system/storage/slim。
//
// 门禁与备份恢复一致：真正执行要 confirm=SLIM，且 dryRun 显式传 false；
// 默认只统计（不写库），返回可回收字节与剩余待处理条数，便于分批跑。
func SlimStorage(c *gin.Context) {
	var req SlimStorageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	dryRun := true
	if req.DryRun != nil {
		dryRun = *req.DryRun
	}
	res, err := maintenance.SlimBodies(c.Request.Context(), maintenance.SlimOptions{
		Collection: req.Collection,
		KeepBytes:  req.KeepBytes,
		Limit:      req.Limit,
		DryRun:     dryRun,
		Confirm:    req.Confirm,
		AfterID:    req.AfterID,
	})
	if err != nil {
		// 参数类错误（确认串不对、集合不在白名单）都是调用方问题，回 400；
		// 只有真正执行失败才算 500。
		if errors.Is(err, maintenance.ErrConfirmRequired) || errors.Is(err, maintenance.ErrBadCollection) {
			response.BadRequest(c, "api.bad_request", err)
			return
		}
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, res, "api.success")
}
