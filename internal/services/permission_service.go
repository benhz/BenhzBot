package services

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	"dingteam-bot/internal/models"
)

// PermissionService 权限服务
type PermissionService struct {
	db *sql.DB
}

// NewPermissionService 创建权限服务实例
func NewPermissionService(db *sql.DB) *PermissionService {
	return &PermissionService{db: db}
}

// GetOrCreateUser 获取或创建用户（首次使用时自动创建为 member）
func (s *PermissionService) GetOrCreateUser(ctx context.Context, dingTalkUserID, username string) (*models.User, error) {
	// 先尝试查询用户
	user, err := s.GetUserByDingTalkID(ctx, dingTalkUserID)
	if err == nil {
		return user, nil
	}

	// 用户不存在，创建新用户（默认为 member）
	query := `
		INSERT INTO users (dingtalk_user_id, username, role)
		VALUES ($1, $2, $3)
		RETURNING id, dingtalk_user_id, username, role, created_at, updated_at
	`

	user = &models.User{}
	err = s.db.QueryRowContext(ctx, query, dingTalkUserID, username, models.RoleMember).Scan(
		&user.ID,
		&user.DingTalkUserID,
		&user.Username,
		&user.Role,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("创建用户失败: %w", err)
	}

	log.Printf("创建新用户: %s (%s), 角色: %s", username, dingTalkUserID, user.Role)
	return user, nil
}

// GetUserByDingTalkID 根据钉钉用户ID获取用户
func (s *PermissionService) GetUserByDingTalkID(ctx context.Context, dingTalkUserID string) (*models.User, error) {
	query := `
		SELECT id, dingtalk_user_id, username, role, created_at, updated_at
		FROM users
		WHERE dingtalk_user_id = $1
	`

	user := &models.User{}
	err := s.db.QueryRowContext(ctx, query, dingTalkUserID).Scan(
		&user.ID,
		&user.DingTalkUserID,
		&user.Username,
		&user.Role,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("用户不存在")
	}

	if err != nil {
		return nil, fmt.Errorf("查询用户失败: %w", err)
	}

	return user, nil
}

// CanExecuteCommand 检查用户是否有权限执行指定命令
func (s *PermissionService) CanExecuteCommand(ctx context.Context, dingTalkUserID string, permission models.PermissionName) (bool, models.UserRole, string, error) {
	// 1. 获取用户信息
	user, err := s.GetUserByDingTalkID(ctx, dingTalkUserID)
	if err != nil {
		// 用户不存在，默认为 member 角色
		user = &models.User{
			DingTalkUserID: dingTalkUserID,
			Role:           models.RoleMember,
		}
	}

	// 2. 查询角色是否拥有该权限
	query := `
		SELECT COUNT(*) > 0
		FROM role_permissions
		WHERE role = $1 AND permission_name = $2
	`

	var hasPermission bool
	err = s.db.QueryRowContext(ctx, query, user.Role, permission).Scan(&hasPermission)
	if err != nil {
		return false, user.Role, "", fmt.Errorf("查询权限失败: %w", err)
	}

	// 3. 生成原因说明
	var reason string
	if hasPermission {
		reason = fmt.Sprintf("用户角色为 %s，有权限执行 %s", user.Role, permission)
	} else {
		reason = fmt.Sprintf("用户角色为 %s，无权限执行 %s", user.Role, permission)
	}

	return hasPermission, user.Role, reason, nil
}

// PromoteToAdmin 提升用户为子管理员
func (s *PermissionService) PromoteToAdmin(ctx context.Context, operatorID, targetUserID, targetUsername string) error {
	// 1. 验证操作者是否为主管理员
	operator, err := s.GetUserByDingTalkID(ctx, operatorID)
	if err != nil {
		return fmt.Errorf("操作者不存在")
	}

	if operator.Role != models.RoleSuperAdmin {
		return fmt.Errorf("只有主管理员可以添加子管理员")
	}

	// 2. 获取或创建目标用户
	targetUser, err := s.GetOrCreateUser(ctx, targetUserID, targetUsername)
	if err != nil {
		return fmt.Errorf("获取目标用户失败: %w", err)
	}

	// 3. 检查目标用户是否已经是管理员
	if targetUser.Role == models.RoleSuperAdmin {
		return fmt.Errorf("目标用户已经是主管理员")
	}

	if targetUser.Role == models.RoleAdmin {
		return fmt.Errorf("目标用户已经是子管理员")
	}

	// 4. 更新角色
	query := `
		UPDATE users
		SET role = $1, updated_at = CURRENT_TIMESTAMP
		WHERE dingtalk_user_id = $2
	`

	_, err = s.db.ExecContext(ctx, query, models.RoleAdmin, targetUserID)
	if err != nil {
		return fmt.Errorf("更新角色失败: %w", err)
	}

	// 5. 记录审计日志
	s.logPermissionAction(ctx, operatorID, "promote_to_admin", "user", targetUserID, "granted", "主管理员提升用户为子管理员")

	log.Printf("用户 %s 被提升为子管理员 (操作者: %s)", targetUserID, operatorID)
	return nil
}

// DemoteFromAdmin 移除用户的子管理员权限
func (s *PermissionService) DemoteFromAdmin(ctx context.Context, operatorID, targetUserID string) error {
	// 1. 验证操作者是否为主管理员
	operator, err := s.GetUserByDingTalkID(ctx, operatorID)
	if err != nil {
		return fmt.Errorf("操作者不存在")
	}

	if operator.Role != models.RoleSuperAdmin {
		return fmt.Errorf("只有主管理员可以移除子管理员")
	}

	// 2. 获取目标用户
	targetUser, err := s.GetUserByDingTalkID(ctx, targetUserID)
	if err != nil {
		return fmt.Errorf("目标用户不存在")
	}

	// 3. 检查目标用户是否为子管理员
	if targetUser.Role == models.RoleSuperAdmin {
		return fmt.Errorf("不能降级主管理员")
	}

	if targetUser.Role == models.RoleMember {
		return fmt.Errorf("目标用户已经是普通成员")
	}

	// 4. 更新角色
	query := `
		UPDATE users
		SET role = $1, updated_at = CURRENT_TIMESTAMP
		WHERE dingtalk_user_id = $2
	`

	_, err = s.db.ExecContext(ctx, query, models.RoleMember, targetUserID)
	if err != nil {
		return fmt.Errorf("更新角色失败: %w", err)
	}

	// 5. 记录审计日志
	s.logPermissionAction(ctx, operatorID, "demote_from_admin", "user", targetUserID, "granted", "主管理员移除用户的子管理员权限")

	log.Printf("用户 %s 被降级为普通成员 (操作者: %s)", targetUserID, operatorID)
	return nil
}

// GetUserPermissions 获取用户的所有权限
func (s *PermissionService) GetUserPermissions(ctx context.Context, dingTalkUserID string) ([]string, error) {
	// 1. 获取用户信息
	user, err := s.GetUserByDingTalkID(ctx, dingTalkUserID)
	if err != nil {
		return nil, err
	}

	// 2. 查询该角色的所有权限
	query := `
		SELECT permission_name
		FROM role_permissions
		WHERE role = $1
		ORDER BY permission_name
	`

	rows, err := s.db.QueryContext(ctx, query, user.Role)
	if err != nil {
		return nil, fmt.Errorf("查询权限列表失败: %w", err)
	}
	defer rows.Close()

	var permissions []string
	for rows.Next() {
		var perm string
		if err := rows.Scan(&perm); err != nil {
			return nil, err
		}
		permissions = append(permissions, perm)
	}

	return permissions, nil
}

// ListUsersByRole 列出指定角色的所有用户
func (s *PermissionService) ListUsersByRole(ctx context.Context, role models.UserRole) ([]models.User, error) {
	query := `
		SELECT id, dingtalk_user_id, username, role, created_at, updated_at
		FROM users
		WHERE role = $1
		ORDER BY created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, role)
	if err != nil {
		return nil, fmt.Errorf("查询用户列表失败: %w", err)
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var user models.User
		err := rows.Scan(
			&user.ID,
			&user.DingTalkUserID,
			&user.Username,
			&user.Role,
			&user.CreatedAt,
			&user.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}

	return users, nil
}

// logPermissionAction 记录权限操作审计日志
func (s *PermissionService) logPermissionAction(ctx context.Context, userID, action, resourceType, resourceID, result, reason string) {
	query := `
		INSERT INTO permission_audit_logs
		(user_id, action, resource_type, resource_id, result, reason)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	_, err := s.db.ExecContext(ctx, query, userID, action, resourceType, resourceID, result, reason)
	if err != nil {
		log.Printf("记录审计日志失败: %v", err)
	}
}

// LogPermissionCheck 记录权限检查日志（公开方法，供外部调用）
func (s *PermissionService) LogPermissionCheck(ctx context.Context, userID string, action models.PermissionName, allowed bool, reason string) {
	result := "granted"
	if !allowed {
		result = "denied"
	}

	s.logPermissionAction(ctx, userID, string(action), "command", "", result, reason)
}

// ========================================
// 超级管理员初始化
// ========================================

// InitializeSuperAdmins 初始化超级管理员
// 从配置文件中读取 ADMIN_USERS，并确保他们在数据库中被设置为 super_admin 角色
// 这个方法应该在应用启动时调用
func (s *PermissionService) InitializeSuperAdmins(ctx context.Context, adminUserIDs []string) error {
	if len(adminUserIDs) == 0 {
		log.Println("⚠️  警告：未配置超级管理员（ADMIN_USERS 为空）")
		return nil
	}

	log.Printf("🔐 开始初始化超级管理员，共 %d 个用户...", len(adminUserIDs))

	for _, userID := range adminUserIDs {
		// 检查用户是否已存在
		user, err := s.GetUserByDingTalkID(ctx, userID)

		if err != nil {
			// 用户不存在，创建为超级管理员
			query := `
				INSERT INTO users (dingtalk_user_id, username, role)
				VALUES ($1, $2, $3)
				ON CONFLICT (dingtalk_user_id) DO UPDATE
				SET role = $3, updated_at = CURRENT_TIMESTAMP
			`

			_, err = s.db.ExecContext(ctx, query, userID, userID, models.RoleSuperAdmin)
			if err != nil {
				log.Printf("❌ 初始化超级管理员失败 (用户: %s): %v", userID, err)
				continue
			}

			log.Printf("✅ 创建超级管理员: %s", userID)
		} else {
			// 用户已存在，更新为超级管理员（如果角色不对）
			if user.Role != models.RoleSuperAdmin {
				query := `
					UPDATE users
					SET role = $1, updated_at = CURRENT_TIMESTAMP
					WHERE dingtalk_user_id = $2
				`

				_, err = s.db.ExecContext(ctx, query, models.RoleSuperAdmin, userID)
				if err != nil {
					log.Printf("❌ 更新超级管理员失败 (用户: %s): %v", userID, err)
					continue
				}

				log.Printf("✅ 更新用户为超级管理员: %s (原角色: %s)", userID, user.Role)
			} else {
				log.Printf("✓ 超级管理员已存在: %s", userID)
			}
		}
	}

	log.Println("✅ 超级管理员初始化完成")
	return nil
}
