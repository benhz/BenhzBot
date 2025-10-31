package handlers

import (
	"database/sql"
	"fmt"
	"net/http"

	"dingteam-bot/internal/models"
	"dingteam-bot/internal/services"

	"github.com/gin-gonic/gin"
)

// APIHandler HTTP API 处理器（供 Dify 调用）
type APIHandler struct {
	permService *services.PermissionService
	taskService *services.TaskService
	statsService *services.StatsService
}

// NewAPIHandler 创建 API 处理器
func NewAPIHandler(permService *services.PermissionService, taskService *services.TaskService, statsService *services.StatsService) *APIHandler {
	return &APIHandler{
		permService:  permService,
		taskService:  taskService,
		statsService: statsService,
	}
}

// ========================================
// 权限相关 API
// ========================================

// CheckPermission 检查用户权限
// GET /api/v1/permissions/check?user_id={userID}&action={action}
func (h *APIHandler) CheckPermission(c *gin.Context) {
	userID := c.Query("user_id")
	action := c.Query("action")

	if userID == "" || action == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "user_id 和 action 参数不能为空",
		})
		return
	}

	// 验证权限名称是否合法
	if !models.IsValidPermission(action) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "无效的权限名称",
		})
		return
	}

	// 执行权限检查
	allowed, userRole, reason, err := h.permService.CanExecuteCommand(
		c.Request.Context(),
		userID,
		models.PermissionName(action),
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	// 记录审计日志
	h.permService.LogPermissionCheck(c.Request.Context(), userID, models.PermissionName(action), allowed, reason)

	c.JSON(http.StatusOK, gin.H{
		"allowed":   allowed,
		"user_role": userRole,
		"reason":    reason,
	})
}

// GetUserInfo 获取用户信息
// GET /api/v1/users/:userID
func (h *APIHandler) GetUserInfo(c *gin.Context) {
	userID := c.Param("userID")

	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "用户ID不能为空",
		})
		return
	}

	// 获取用户信息
	user, err := h.permService.GetUserByDingTalkID(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "用户不存在",
		})
		return
	}

	// 获取用户权限列表
	permissions, err := h.permService.GetUserPermissions(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "获取权限列表失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user_id":     user.DingTalkUserID,
		"username":    user.Username,
		"role":        user.Role,
		"permissions": permissions,
		"created_at":  user.CreatedAt,
		"updated_at":  user.UpdatedAt,
	})
}

// PromoteUser 提升用户为子管理员
// POST /api/v1/admin/users/:userID/promote
// Body: {"operator_id": "xxx", "target_username": "张三"}
func (h *APIHandler) PromoteUser(c *gin.Context) {
	targetUserID := c.Param("userID")

	var req struct {
		OperatorID     string `json:"operator_id" binding:"required"`
		TargetUsername string `json:"target_username"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "请求参数错误",
		})
		return
	}

	// 执行提升操作
	err := h.permService.PromoteToAdmin(
		c.Request.Context(),
		req.OperatorID,
		targetUserID,
		req.TargetUsername,
	)

	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "成功将用户提升为子管理员",
		"user_id": targetUserID,
	})
}

// DemoteUser 移除用户的子管理员权限
// POST /api/v1/admin/users/:userID/demote
// Body: {"operator_id": "xxx"}
func (h *APIHandler) DemoteUser(c *gin.Context) {
	targetUserID := c.Param("userID")

	var req struct {
		OperatorID string `json:"operator_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "请求参数错误",
		})
		return
	}

	// 执行降级操作
	err := h.permService.DemoteFromAdmin(
		c.Request.Context(),
		req.OperatorID,
		targetUserID,
	)

	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "成功移除用户的子管理员权限",
		"user_id": targetUserID,
	})
}

// ListAdmins 列出所有管理员
// GET /api/v1/admin/users/admins
func (h *APIHandler) ListAdmins(c *gin.Context) {
	// 获取所有子管理员
	admins, err := h.permService.ListUsersByRole(c.Request.Context(), models.RoleAdmin)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "查询管理员列表失败",
		})
		return
	}

	// 获取所有主管理员
	superAdmins, err := h.permService.ListUsersByRole(c.Request.Context(), models.RoleSuperAdmin)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "查询主管理员列表失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"super_admins": superAdmins,
		"admins":       admins,
	})
}

// ========================================
// 任务相关 API（带权限验证）
// ========================================

// CreateTaskAPI 创建任务 API
// POST /api/v1/tasks
// Header: X-Operator-ID (操作者ID，用于权限验证)
func (h *APIHandler) CreateTaskAPI(c *gin.Context) {
	operatorID := c.GetHeader("X-Operator-ID")
	if operatorID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "缺少操作者ID (X-Operator-ID header)",
		})
		return
	}

	// 二次权限验证（容错机制）
	allowed, _, reason, err := h.permService.CanExecuteCommand(
		c.Request.Context(),
		operatorID,
		models.PermCreateTask,
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "权限验证失败",
		})
		return
	}

	if !allowed {
		h.permService.LogPermissionCheck(c.Request.Context(), operatorID, models.PermCreateTask, false, reason)
		c.JSON(http.StatusForbidden, gin.H{
			"error":  "权限不足，无法创建任务",
			"reason": reason,
		})
		return
	}

	// 解析请求
	var task models.Task
	if err := c.ShouldBindJSON(&task); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "请求参数错误",
		})
		return
	}

	// 创建任务
	task.CreatorUserID = operatorID
	if err := h.taskService.CreateTask(&task); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	// 记录审计日志
	h.permService.LogPermissionCheck(c.Request.Context(), operatorID, models.PermCreateTask, true, "成功创建任务")

	c.JSON(http.StatusOK, gin.H{
		"message": "任务创建成功",
		"task":    task,
	})
}

// GetTasksAPI 获取任务列表 API
// GET /api/v1/tasks?group_chat_id={groupChatID}
// Header: X-Operator-ID (操作者ID，用于权限验证)
func (h *APIHandler) GetTasksAPI(c *gin.Context) {
	operatorID := c.GetHeader("X-Operator-ID")
	if operatorID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "缺少操作者ID (X-Operator-ID header)",
		})
		return
	}

	// 权限验证
	allowed, _, reason, err := h.permService.CanExecuteCommand(
		c.Request.Context(),
		operatorID,
		models.PermListTasks,
	)

	if err != nil || !allowed {
		c.JSON(http.StatusForbidden, gin.H{
			"error":  "权限不足，无法查看任务",
			"reason": reason,
		})
		return
	}

	groupChatID := c.Query("group_chat_id")
	if groupChatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "缺少群聊ID参数",
		})
		return
	}

	// 获取任务列表
	tasks, err := h.taskService.GetActiveTasksByGroup(groupChatID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"tasks": tasks,
	})
}

// DeleteTaskAPI 删除任务 API
// DELETE /api/v1/tasks/:taskID
// Header: X-Operator-ID (操作者ID，用于权限验证)
func (h *APIHandler) DeleteTaskAPI(c *gin.Context) {
	operatorID := c.GetHeader("X-Operator-ID")
	if operatorID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "缺少操作者ID (X-Operator-ID header)",
		})
		return
	}

	// 权限验证
	allowed, _, reason, err := h.permService.CanExecuteCommand(
		c.Request.Context(),
		operatorID,
		models.PermDeleteTask,
	)

	if err != nil || !allowed {
		h.permService.LogPermissionCheck(c.Request.Context(), operatorID, models.PermDeleteTask, false, reason)
		c.JSON(http.StatusForbidden, gin.H{
			"error":  "权限不足，无法删除任务",
			"reason": reason,
		})
		return
	}

	// 解析任务ID
	var taskID int
	if _, err := fmt.Sscanf(c.Param("taskID"), "%d", &taskID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "任务ID格式错误",
		})
		return
	}

	// 删除任务（实际是标记为 DELETED）
	err = h.taskService.DeleteTask(taskID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	// 记录审计日志
	h.permService.LogPermissionCheck(c.Request.Context(), operatorID, models.PermDeleteTask, true, "成功删除任务")

	c.JSON(http.StatusOK, gin.H{
		"message": "任务删除成功",
		"task_id": taskID,
	})
}

// CompleteTaskAPI 打卡完成任务 API
// POST /api/v1/tasks/:taskID/complete
// Header: X-Operator-ID (操作者ID，用于权限验证)
func (h *APIHandler) CompleteTaskAPI(c *gin.Context) {
	operatorID := c.GetHeader("X-Operator-ID")
	if operatorID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "缺少操作者ID (X-Operator-ID header)",
		})
		return
	}

	// 权限验证
	allowed, _, reason, err := h.permService.CanExecuteCommand(
		c.Request.Context(),
		operatorID,
		models.PermCompleteTask,
	)

	if err != nil || !allowed {
		c.JSON(http.StatusForbidden, gin.H{
			"error":  "权限不足，无法打卡",
			"reason": reason,
		})
		return
	}

	// 解析任务ID
	var taskID int
	if err := c.ShouldBindUri(&struct {
		TaskID int `uri:"taskID" binding:"required"`
	}{TaskID: taskID}); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "任务ID格式错误",
		})
		return
	}

	// 解析请求
	var req struct {
		GroupChatID string `json:"group_chat_id" binding:"required"`
		Username    string `json:"username"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "请求参数错误",
		})
		return
	}

	// 检查是否已打卡
	completed, err := h.taskService.HasCompletedToday(taskID, operatorID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "检查打卡状态失败",
		})
		return
	}

	if completed {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "今天已经打过卡了",
		})
		return
	}

	// 创建完成记录
	record := &models.CompletionRecord{
		TaskID:      taskID,
		UserID:      operatorID,
		UserName:    sql.NullString{String: req.Username, Valid: req.Username != ""},
		GroupChatID: req.GroupChatID,
		IsOnTime:    true, // 这里需要根据实际情况判断
	}

	if err := h.taskService.RecordCompletion(record); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "打卡失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "打卡成功",
		"record":  record,
	})
}

// GetStatsAPI 获取统计数据 API
// GET /api/v1/tasks/:taskID/stats
// Header: X-Operator-ID (操作者ID，用于权限验证)
func (h *APIHandler) GetStatsAPI(c *gin.Context) {
	operatorID := c.GetHeader("X-Operator-ID")
	if operatorID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "缺少操作者ID (X-Operator-ID header)",
		})
		return
	}

	// 权限验证
	allowed, _, reason, err := h.permService.CanExecuteCommand(
		c.Request.Context(),
		operatorID,
		models.PermViewStats,
	)

	if err != nil || !allowed {
		c.JSON(http.StatusForbidden, gin.H{
			"error":  "权限不足，无法查看统计",
			"reason": reason,
		})
		return
	}

	// 解析任务ID
	var taskID int
	if err := c.ShouldBindUri(&struct {
		TaskID int `uri:"taskID" binding:"required"`
	}{TaskID: taskID}); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "任务ID格式错误",
		})
		return
	}

	// 获取统计数据
	stats, err := h.statsService.GetTodayStats(taskID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "获取统计数据失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"stats": stats,
	})
}
