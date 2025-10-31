package models

import "time"

// UserRole 用户角色类型
type UserRole string

const (
	RoleSuperAdmin UserRole = "super_admin" // 主管理员
	RoleAdmin      UserRole = "admin"       // 子管理员
	RoleMember     UserRole = "member"      // 普通成员
)

// PermissionName 权限名称类型
type PermissionName string

const (
	PermAddAdmin     PermissionName = "add_admin"     // 添加子管理员
	PermRemoveAdmin  PermissionName = "remove_admin"  // 移除子管理员
	PermCreateTask   PermissionName = "create_task"   // 创建任务
	PermUpdateTask   PermissionName = "update_task"   // 更新任务
	PermDeleteTask   PermissionName = "delete_task"   // 删除任务
	PermListTasks    PermissionName = "list_tasks"    // 查看任务列表
	PermCompleteTask PermissionName = "complete_task" // 打卡完成任务
	PermViewStats    PermissionName = "view_stats"    // 查看统计
)

// User 用户模型
type User struct {
	ID             int       `json:"id"`
	DingTalkUserID string    `json:"dingtalk_user_id"`
	Username       string    `json:"username"`
	Role           UserRole  `json:"role"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Permission 权限模型
type Permission struct {
	ID             int       `json:"id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	CommandPattern string    `json:"command_pattern"`
	CreatedAt      time.Time `json:"created_at"`
}

// RolePermission 角色权限映射
type RolePermission struct {
	Role           UserRole   `json:"role"`
	PermissionName string     `json:"permission_name"`
	CreatedAt      time.Time  `json:"created_at"`
}

// PermissionAuditLog 权限审计日志
type PermissionAuditLog struct {
	ID           int       `json:"id"`
	UserID       string    `json:"user_id"`
	Action       string    `json:"action"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	Result       string    `json:"result"` // granted, denied
	Reason       string    `json:"reason"`
	IPAddress    string    `json:"ip_address"`
	UserAgent    string    `json:"user_agent"`
	CreatedAt    time.Time `json:"created_at"`
}

// PermissionCheckRequest 权限检查请求
type PermissionCheckRequest struct {
	UserID string         `json:"user_id" binding:"required"`
	Action PermissionName `json:"action" binding:"required"`
}

// PermissionCheckResponse 权限检查响应
type PermissionCheckResponse struct {
	Allowed  bool     `json:"allowed"`
	UserRole UserRole `json:"user_role"`
	Reason   string   `json:"reason"`
}

// UserRoleUpdateRequest 用户角色更新请求
type UserRoleUpdateRequest struct {
	OperatorID string   `json:"operator_id" binding:"required"` // 操作者ID（用于权限验证）
	TargetRole UserRole `json:"target_role" binding:"required"`  // 目标角色
}

// IsValidRole 检查角色是否合法
func IsValidRole(role string) bool {
	return role == string(RoleSuperAdmin) ||
		role == string(RoleAdmin) ||
		role == string(RoleMember)
}

// IsValidPermission 检查权限名称是否合法
func IsValidPermission(perm string) bool {
	validPerms := []PermissionName{
		PermAddAdmin,
		PermRemoveAdmin,
		PermCreateTask,
		PermUpdateTask,
		PermDeleteTask,
		PermListTasks,
		PermCompleteTask,
		PermViewStats,
	}
	for _, p := range validPerms {
		if string(p) == perm {
			return true
		}
	}
	return false
}
