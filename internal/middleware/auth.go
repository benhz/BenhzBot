package middleware

import (
	"net/http"

	"dingteam-bot/internal/models"
	"dingteam-bot/internal/services"

	"github.com/gin-gonic/gin"
)

// PermissionMiddleware 权限验证中间件
type PermissionMiddleware struct {
	permService *services.PermissionService
}

// NewPermissionMiddleware 创建权限验证中间件
func NewPermissionMiddleware(permService *services.PermissionService) *PermissionMiddleware {
	return &PermissionMiddleware{
		permService: permService,
	}
}

// RequirePermission 要求特定权限的中间件
func (m *PermissionMiddleware) RequirePermission(permission models.PermissionName) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从 Header 中获取操作者 ID
		operatorID := c.GetHeader("X-Operator-ID")
		if operatorID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "缺少操作者ID (X-Operator-ID header)",
			})
			c.Abort()
			return
		}

		// 检查权限
		allowed, userRole, reason, err := m.permService.CanExecuteCommand(
			c.Request.Context(),
			operatorID,
			permission,
		)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "权限验证失败",
			})
			c.Abort()
			return
		}

		if !allowed {
			// 记录审计日志
			m.permService.LogPermissionCheck(c.Request.Context(), operatorID, permission, false, reason)

			c.JSON(http.StatusForbidden, gin.H{
				"error":     "权限不足",
				"reason":    reason,
				"user_role": userRole,
			})
			c.Abort()
			return
		}

		// 权限通过，将用户信息存储到上下文
		c.Set("operator_id", operatorID)
		c.Set("user_role", userRole)

		// 记录审计日志
		m.permService.LogPermissionCheck(c.Request.Context(), operatorID, permission, true, reason)

		c.Next()
	}
}

// RequireRole 要求特定角色的中间件
func (m *PermissionMiddleware) RequireRole(roles ...models.UserRole) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从 Header 中获取操作者 ID
		operatorID := c.GetHeader("X-Operator-ID")
		if operatorID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "缺少操作者ID (X-Operator-ID header)",
			})
			c.Abort()
			return
		}

		// 获取用户信息
		user, err := m.permService.GetUserByDingTalkID(c.Request.Context(), operatorID)
		if err != nil {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "用户不存在或权限不足",
			})
			c.Abort()
			return
		}

		// 检查角色
		hasRole := false
		for _, role := range roles {
			if user.Role == role {
				hasRole = true
				break
			}
		}

		if !hasRole {
			c.JSON(http.StatusForbidden, gin.H{
				"error":     "角色权限不足",
				"user_role": user.Role,
			})
			c.Abort()
			return
		}

		// 角色验证通过，将用户信息存储到上下文
		c.Set("operator_id", operatorID)
		c.Set("user_role", user.Role)

		c.Next()
	}
}

// RequireSuperAdmin 要求主管理员权限的中间件
func (m *PermissionMiddleware) RequireSuperAdmin() gin.HandlerFunc {
	return m.RequireRole(models.RoleSuperAdmin)
}

// RequireAdmin 要求管理员（包括主管理员和子管理员）权限的中间件
func (m *PermissionMiddleware) RequireAdmin() gin.HandlerFunc {
	return m.RequireRole(models.RoleSuperAdmin, models.RoleAdmin)
}
