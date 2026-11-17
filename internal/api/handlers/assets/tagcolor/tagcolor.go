package tagcolor

import (
	"errors"
	"fmt"
	"sync"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	svc "github.com/Autumn-27/ScopeSentry/internal/services/assets/tagcolor"
	"github.com/Autumn-27/ScopeSentry/internal/utils/tagpalette"
	"github.com/gin-gonic/gin"
)

var (
	tagColorService     svc.Service
	tagColorServiceOnce sync.Once
)

func getService() svc.Service {
	tagColorServiceOnce.Do(func() { tagColorService = svc.NewService() })
	return tagColorService
}

// List 返回所有标签颜色覆盖（issue #217）
// @Summary 标签颜色覆盖列表
// @Tags 通用操作
// @Router /api/assets/tag_color/list [post]
func List(c *gin.Context) {
	colors, err := getService().List(c.Request.Context())
	if err != nil {
		logger.Error(fmt.Sprintf("list tag colors: %v", err))
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, gin.H{"colors": colors}, "api.success")
}

// Set 设置/清除某个标签的颜色覆盖
// @Summary 设置标签颜色
// @Tags 通用操作
// @Router /api/assets/tag_color/set [post]
func Set(c *gin.Context) {
	var req models.TagColorRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	if err := getService().Set(c.Request.Context(), req.Tag, req.Color); err != nil {
		if errors.Is(err, tagpalette.ErrInvalidTag) || errors.Is(err, tagpalette.ErrInvalidColor) {
			response.BadRequest(c, "api.bad_request", err)
			return
		}
		logger.Error(fmt.Sprintf("set tag color: %v", err))
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, nil, "api.success")
}
