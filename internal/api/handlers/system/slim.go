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
	})
	if err != nil {
		if errors.Is(err, maintenance.ErrConfirmRequired) {
			response.BadRequest(c, "api.bad_request", err)
			return
		}
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, res, "api.success")
}
