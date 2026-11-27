package user

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/services/user"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// 用户管理 handler（对标 ARL-Next user.py）：
//
//   - 登录 / 自助改密：匿名 + JWT（历史能力，保持不变）；
//   - 列表 / 详情 / 创建 / 更新 / 删除：JWT + 细粒度权限（见 routes/user）；
//   - pageSize > 200 一律 400；非法 id 一律 400；不存在一律 404。
//
// 响应里不会出现密码哈希：models.User.Password 的 json tag 是 "-"。

var userService user.Service

// ChangePasswordRequest 修改密码请求
type ChangePasswordRequest struct {
	NewPassword string `json:"newPassword" binding:"required" example:"newStrongPassword123"`
}

// LoginRequest 登录请求
type LoginRequest struct {
	Username string `json:"username" binding:"required" example:"admin"`
	Password string `json:"password" binding:"required" example:"password123"`
}

// respondUserError 统一错误出口：可判定的转 400 / 404，其余 500
func respondUserError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, user.ErrInvalidID):
		response.BadRequestWithError(c, "api.bad_request.id", err)
	case errors.Is(err, user.ErrUserNotFound):
		response.NotFound(c, "api.not_found", err)
	case errors.Is(err, user.ErrInvalidUserData),
		errors.Is(err, user.ErrInvalidRole),
		errors.Is(err, user.ErrInvalidStatus),
		errors.Is(err, user.ErrWeakPassword),
		errors.Is(err, user.ErrUsernameExists),
		errors.Is(err, user.ErrEmailExists),
		errors.Is(err, user.ErrDuplicateUser),
		errors.Is(err, user.ErrCannotDeleteSelf),
		errors.Is(err, user.ErrLastSuperAdmin):
		response.BadRequestWithError(c, "api.bad_request", err)
	default:
		logger.Error("user: request failed", zap.Error(err))
		response.InternalServerError(c, "api.server_error", err)
	}
}

// parsePageParams 分页参数归一：pageSize 上限 200，超过报错（handler 转 400）
func parsePageParams(pageIndex, pageSize int) (int, int, error) {
	if pageIndex <= 0 {
		pageIndex = 1
	}
	if pageSize <= 0 {
		pageSize = models.UserDefaultPageSize
	}
	if pageSize > models.UserMaxPageSize {
		return 0, 0, fmt.Errorf("pageSize 不能超过 %d", models.UserMaxPageSize)
	}
	return pageIndex, pageSize, nil
}

// currentUserID 从 JWT 中间件写入的上下文取当前用户 ID
func currentUserID(c *gin.Context) string {
	value, exists := c.Get("userID")
	if !exists {
		return ""
	}
	id, _ := value.(string)
	return id
}

// Login 用户登录
// @Summary      用户登录
// @Description  用户登录接口
// @Tags         user
// @Accept       json
// @Produce      json
// @Param        loginRequest  body      LoginRequest  true  "登录信息"
// @Success      200  {object}  response.SuccessResponse{data=object{access_token=string}}
// @Failure      400  {object}  response.BadRequestResponse
// @Failure      500  {object}  response.InternalServerErrorResponse
// @Router       /user/login [post]
func Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	token, err := userService.Login(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		response.BadRequest(c, "api.user.login.failed", err)
		return
	}

	response.Success(c, gin.H{"access_token": token}, "api.user.login.success")
}

// ChangePassword 用户修改密码
// @Summary      修改当前用户密码
// @Description  通过 JWT 获取用户信息，修改其密码（最短 8 位，SHA256 存储）
// @Tags         user
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        changePasswordRequest  body      ChangePasswordRequest  true  "修改密码信息"
// @Success      200  {object}  response.SuccessResponse
// @Failure      400  {object}  response.BadRequestResponse
// @Failure      401  {object}  response.UnauthorizedResponse
// @Router       /user/changePassword [post]
func ChangePassword(c *gin.Context) {
	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	userID := currentUserID(c)
	if userID == "" {
		response.Unauthorized(c, "api.unauthorized", nil)
		return
	}

	if err := userService.ChangePassword(c.Request.Context(), userID, req.NewPassword); err != nil {
		respondUserError(c, err)
		return
	}
	response.Success(c, nil, "api.user.change_password.success")
}

// List 用户列表
// @Summary      用户列表
// @Description  分页列出用户；keyword 模糊匹配用户名/邮箱，role/status 精确过滤；pageSize 上限 200
// @Tags         user
// @Produce      json
// @Security     ApiKeyAuth
// @Param        keyword    query  string  false  "关键字（用户名 / 邮箱）"
// @Param        role       query  string  false  "角色"
// @Param        status     query  string  false  "状态"
// @Param        pageIndex  query  int     false  "页码"  default(1)
// @Param        pageSize   query  int     false  "每页数量"  default(20)
// @Success      200  {object}  response.SuccessResponse
// @Failure      400  {object}  response.BadRequestResponse
// @Failure      401  {object}  response.UnauthorizedResponse
// @Router       /user [get]
func List(c *gin.Context) {
	var query models.UserListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	pageIndex := query.PageIndex
	if pageIndex <= 0 {
		pageIndex = query.Page
	}
	pageSize := query.PageSize
	if pageSize <= 0 {
		pageSize = query.Size
	}
	pageIndex, pageSize, err := parsePageParams(pageIndex, pageSize)
	if err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	query.PageIndex, query.PageSize = pageIndex, pageSize

	list, total, err := userService.ListUsers(c.Request.Context(), query)
	if err != nil {
		respondUserError(c, err)
		return
	}

	response.Success(c, gin.H{
		"list":      list,
		"total":     total,
		"pageIndex": pageIndex,
		"pageSize":  pageSize,
	}, "api.success")
}

// Get 用户详情
// @Summary      用户详情
// @Tags         user
// @Produce      json
// @Security     ApiKeyAuth
// @Param        id  path  string  true  "用户ID"
// @Success      200  {object}  response.SuccessResponse{data=models.User}
// @Failure      400  {object}  response.BadRequestResponse
// @Failure      401  {object}  response.UnauthorizedResponse
// @Failure      404  {object}  response.NotFoundResponse
// @Router       /user/{id} [get]
func Get(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		response.BadRequestWithError(c, "api.required.id", nil)
		return
	}

	record, err := userService.GetUserByID(c.Request.Context(), id)
	if err != nil {
		respondUserError(c, err)
		return
	}
	if record == nil {
		response.NotFound(c, "api.not_found", nil)
		return
	}
	response.Success(c, record, "api.success")
}

// Create 创建用户（注册语义）
// @Summary      创建用户
// @Description  校验角色/密码长度/用户名邮箱唯一；密码 SHA256 存储
// @Tags         user
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        userCreate  body      models.UserCreate  true  "用户信息"
// @Success      200  {object}  response.SuccessResponse{data=models.User}
// @Failure      400  {object}  response.BadRequestResponse
// @Failure      401  {object}  response.UnauthorizedResponse
// @Router       /user [post]
func Create(c *gin.Context) {
	var req models.UserCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	created, err := userService.CreateUser(c.Request.Context(), &req)
	if err != nil {
		respondUserError(c, err)
		return
	}
	response.Success(c, created, "api.success")
}

// Update 更新用户
// @Summary      更新用户
// @Description  更新 email / role / status，可选重置密码（最短 8 位）
// @Tags         user
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        id          path      string             true  "用户ID"
// @Param        userUpdate  body      models.UserUpdate  true  "更新信息"
// @Success      200  {object}  response.SuccessResponse{data=models.User}
// @Failure      400  {object}  response.BadRequestResponse
// @Failure      401  {object}  response.UnauthorizedResponse
// @Failure      404  {object}  response.NotFoundResponse
// @Router       /user/{id} [put]
func Update(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		response.BadRequestWithError(c, "api.required.id", nil)
		return
	}
	var req models.UserUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}

	updated, err := userService.UpdateUser(c.Request.Context(), id, &req)
	if err != nil {
		respondUserError(c, err)
		return
	}
	response.Success(c, updated, "api.success")
}

// Delete 删除用户
// @Summary      删除用户
// @Description  不能删除自己，也不能删除最后一个 super_admin
// @Tags         user
// @Produce      json
// @Security     ApiKeyAuth
// @Param        id  path  string  true  "用户ID"
// @Success      200  {object}  response.SuccessResponse
// @Failure      400  {object}  response.BadRequestResponse
// @Failure      401  {object}  response.UnauthorizedResponse
// @Failure      404  {object}  response.NotFoundResponse
// @Router       /user/{id} [delete]
func Delete(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		response.BadRequestWithError(c, "api.required.id", nil)
		return
	}

	// 当前操作者必须可识别：没有 actorID 就无法执行「不能删除自己」的判定
	actorID := currentUserID(c)
	if actorID == "" {
		response.Unauthorized(c, "api.unauthorized", nil)
		return
	}

	if err := userService.DeleteUser(c.Request.Context(), id, actorID); err != nil {
		respondUserError(c, err)
		return
	}
	response.Success(c, gin.H{"id": id, "deleted": true}, "api.success")
}

func init() {
	userService = user.NewService()
}
