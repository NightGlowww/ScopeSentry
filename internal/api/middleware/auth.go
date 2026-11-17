package middleware

import (
	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
)

// AuthMiddleware JWT认证中间件
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			response.Unauthorized(c, "api.unauthorized", nil)
			c.Abort()
			return
		}

		// 检查Bearer token格式
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			response.Unauthorized(c, "api.unauthorized", nil)
			c.Abort()
			return
		}

		tokenString := parts[1]

		// 解析token
		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return []byte(config.GlobalConfig.JWT.Secret), nil
		})

		if err != nil {
			// 细节只进服务端日志；返回给前端统一是「未授权访问」，不带内部错误串
			logger.Error("Failed to parse token", zap.Error(err))
			response.Unauthorized(c, "api.unauthorized", nil)
			c.Abort()
			return
		}

		// 验证token
		if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
			// 将用户信息存储到上下文中
			c.Set("userID", claims["userID"])
			c.Set("username", claims["username"])
			c.Set("role", claims["role"])
			c.Next()
		} else {
			response.Unauthorized(c, "api.unauthorized", nil)
			c.Abort()
			return
		}
	}
}
