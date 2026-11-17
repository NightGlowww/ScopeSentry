package user

import (
	"github.com/Autumn-27/ScopeSentry/internal/api/handlers/user"
	"github.com/Autumn-27/ScopeSentry/internal/api/routes/common"
	"github.com/Autumn-27/ScopeSentry/internal/constants"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/gin-gonic/gin"
)

// RegisterUserRoutes 注册用户相关路由（对标 ARL-Next user.py）。
//
// 权限映射：
//
//	GET    /api/user        user:list
//	GET    /api/user/:id    user:read
//	POST   /api/user        user:create
//	POST   /api/user/register  user:create（兼容旧注册路径，语义与 POST /api/user 完全一致）
//	PUT    /api/user/:id    user:update
//	DELETE /api/user/:id    user:delete
//
// 只有 /login 匿名可访问（见 routes/audit 的白名单）；未认证一律 401，
// 角色没有对应权限（例如普通 user 访问列表）由 PermissionMiddleware 返回 403。
func RegisterUserRoutes(api *gin.RouterGroup) {
	userRoutes := models.RouteGroup{
		Path: "/user",
		Routes: []models.Route{
			// 公开路由：登录
			{
				Method:  "POST",
				Path:    "/login",
				Handler: user.Login,
			},
			// 自助改密（需要认证）
			{
				Method:      "POST",
				Path:        "/changePassword",
				Handler:     user.ChangePassword,
				Middlewares: common.WithAuth(),
			},

			// 用户管理（认证 + 细粒度权限）
			{
				Method:      "GET",
				Path:        "",
				Handler:     user.List,
				Middlewares: common.WithAuthAndPermission(constants.PermissionUserList),
			},
			{
				Method:      "GET",
				Path:        "/:id",
				Handler:     user.Get,
				Middlewares: common.WithAuthAndPermission(constants.PermissionUserRead),
			},
			{
				Method:      "POST",
				Path:        "",
				Handler:     user.Create,
				Middlewares: common.WithAuthAndPermission(constants.PermissionUserCreate),
			},
			{
				Method:      "POST",
				Path:        "/register",
				Handler:     user.Create,
				Middlewares: common.WithAuthAndPermission(constants.PermissionUserCreate),
			},
			{
				Method:      "PUT",
				Path:        "/:id",
				Handler:     user.Update,
				Middlewares: common.WithAuthAndPermission(constants.PermissionUserUpdate),
			},
			{
				Method:      "DELETE",
				Path:        "/:id",
				Handler:     user.Delete,
				Middlewares: common.WithAuthAndPermission(constants.PermissionUserDelete),
			},
		},
	}

	// 注册用户路由组
	common.RegisterRouteGroup(api, userRoutes)
}
