package handlers

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"dingteam-bot/internal/models"
	"dingteam-bot/internal/services"

	"github.com/gin-gonic/gin"
)

// DifyHandler 处理 Dify 回调请求
type DifyHandler struct {
	permService  *services.PermissionService
	taskService  *services.TaskService
	statsService *services.StatsService
	sessionStore *SessionStore
	dtClient     interface {
		SendGroupMessage(chatID, content string) error
	}
}

// NewDifyHandler 创建 Dify 处理器
func NewDifyHandler(
	permService *services.PermissionService,
	taskService *services.TaskService,
	statsService *services.StatsService,
	dtClient interface {
		SendGroupMessage(chatID, content string) error
	},
) *DifyHandler {
	return &DifyHandler{
		permService:  permService,
		taskService:  taskService,
		statsService: statsService,
		sessionStore: NewSessionStore(),
		dtClient:     dtClient,
	}
}

// SessionStore 会话存储（conversation_id → user_id 映射）
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*SessionInfo
}

// SessionInfo 会话信息
type SessionInfo struct {
	UserID          string
	Username        string
	GroupChatID     string
	ConversationID  string
	LastActiveTime  time.Time
}

// NewSessionStore 创建会话存储
func NewSessionStore() *SessionStore {
	store := &SessionStore{
		sessions: make(map[string]*SessionInfo),
	}
	// 启动清理协程，定期清理过期会话（30分钟无活动）
	go store.cleanExpiredSessions()
	return store
}

// SaveSession 保存会话信息
func (s *SessionStore) SaveSession(conversationID string, info *SessionInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	info.LastActiveTime = time.Now()
	s.sessions[conversationID] = info
	log.Printf("会话已保存: %s → %s (%s)", conversationID, info.UserID, info.Username)
}

// GetSession 获取会话信息
func (s *SessionStore) GetSession(conversationID string) (*SessionInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	info, ok := s.sessions[conversationID]
	if ok {
		// 更新最后活跃时间
		info.LastActiveTime = time.Now()
	}
	return info, ok
}

// cleanExpiredSessions 清理过期会话
func (s *SessionStore) cleanExpiredSessions() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for id, info := range s.sessions {
			if now.Sub(info.LastActiveTime) > 30*time.Minute {
				delete(s.sessions, id)
				log.Printf("清理过期会话: %s", id)
			}
		}
		s.mu.Unlock()
	}
}

// ========================================
// Dify API 端点
// ========================================

// DifyExecuteRequest Dify 执行请求
type DifyExecuteRequest struct {
	ConversationID string                 `json:"conversation_id" binding:"required"`
	Action         string                 `json:"action" binding:"required"`
	Params         map[string]interface{} `json:"params"`
}

// DifyExecuteResponse Dify 执行响应
type DifyExecuteResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
	Reason  string      `json:"reason,omitempty"`
}

// Execute 统一执行端点（供 Dify 调用）
// POST /api/v1/dify/execute
func (h *DifyHandler) Execute(c *gin.Context) {
	var req DifyExecuteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, DifyExecuteResponse{
			Success: false,
			Message: "请求参数错误",
		})
		return
	}

	// 1. 从会话中获取用户信息
	session, ok := h.sessionStore.GetSession(req.ConversationID)
	if !ok {
		c.JSON(http.StatusUnauthorized, DifyExecuteResponse{
			Success: false,
			Message: "会话已过期或不存在",
			Reason:  "请重新发送消息",
		})
		return
	}

	log.Printf("Dify 请求: conversation=%s, user=%s, action=%s",
		req.ConversationID, session.UserID, req.Action)

	// 2. 验证权限
	allowed, _, reason, err := h.permService.CanExecuteCommand(
		c.Request.Context(),
		session.UserID,
		models.PermissionName(req.Action),
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, DifyExecuteResponse{
			Success: false,
			Message: "权限验证失败",
			Reason:  err.Error(),
		})
		return
	}

	if !allowed {
		// 记录审计日志
		h.permService.LogPermissionCheck(c.Request.Context(), session.UserID, models.PermissionName(req.Action), false, reason)

		c.JSON(http.StatusOK, DifyExecuteResponse{
			Success: false,
			Message: "权限不足",
			Reason:  reason,
		})
		return
	}

	// 3. 权限通过，执行操作
	h.permService.LogPermissionCheck(c.Request.Context(), session.UserID, models.PermissionName(req.Action), true, reason)

	// 根据 action 类型分发到具体处理函数
	switch req.Action {
	case "create_task":
		h.handleCreateTask(c, session, req)
	case "delete_task":
		h.handleDeleteTask(c, session, req)
	case "list_tasks":
		h.handleListTasks(c, session, req)
	case "complete_task":
		h.handleCompleteTask(c, session, req)
	case "view_stats":
		h.handleViewStats(c, session, req)
	case "add_admin":
		h.handleAddAdmin(c, session, req)
	case "remove_admin":
		h.handleRemoveAdmin(c, session, req)
	default:
		c.JSON(http.StatusBadRequest, DifyExecuteResponse{
			Success: false,
			Message: "未知的操作类型",
			Reason:  fmt.Sprintf("不支持的 action: %s", req.Action),
		})
	}
}

// ========================================
// 具体操作处理函数
// ========================================

func (h *DifyHandler) handleCreateTask(c *gin.Context, session *SessionInfo, req DifyExecuteRequest) {
	// 解析参数
	name, _ := req.Params["name"].(string)
	cronExpr, _ := req.Params["cron_expr"].(string)
	taskType, _ := req.Params["type"].(string)

	if name == "" || cronExpr == "" {
		c.JSON(http.StatusBadRequest, DifyExecuteResponse{
			Success: false,
			Message: "缺少必要参数: name 或 cron_expr",
		})
		return
	}

	// 创建任务
	task := &models.Task{
		Name:          name,
		CronExpr:      cronExpr,
		Type:          models.TaskType(taskType),
		GroupChatID:   session.GroupChatID,
		CreatorUserID: session.UserID,
		Status:        models.TaskStatusActive,
	}

	if err := h.taskService.CreateTask(task); err != nil {
		c.JSON(http.StatusInternalServerError, DifyExecuteResponse{
			Success: false,
			Message: "创建任务失败",
			Reason:  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, DifyExecuteResponse{
		Success: true,
		Message: fmt.Sprintf("✅ 任务创建成功！\n\n📋 名称: %s\n⏰ Cron: %s", task.Name, task.CronExpr),
		Data:    task,
	})
}

func (h *DifyHandler) handleDeleteTask(c *gin.Context, session *SessionInfo, req DifyExecuteRequest) {
	taskID, ok := req.Params["task_id"].(float64)
	if !ok {
		c.JSON(http.StatusBadRequest, DifyExecuteResponse{
			Success: false,
			Message: "缺少参数: task_id",
		})
		return
	}

	if err := h.taskService.DeleteTask(int(taskID)); err != nil {
		c.JSON(http.StatusInternalServerError, DifyExecuteResponse{
			Success: false,
			Message: "删除任务失败",
			Reason:  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, DifyExecuteResponse{
		Success: true,
		Message: "✅ 任务已删除",
	})
}

func (h *DifyHandler) handleListTasks(c *gin.Context, session *SessionInfo, req DifyExecuteRequest) {
	tasks, err := h.taskService.GetActiveTasksByGroup(session.GroupChatID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, DifyExecuteResponse{
			Success: false,
			Message: "查询任务失败",
			Reason:  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, DifyExecuteResponse{
		Success: true,
		Message: fmt.Sprintf("📋 找到 %d 个活跃任务", len(tasks)),
		Data:    tasks,
	})
}

func (h *DifyHandler) handleCompleteTask(c *gin.Context, session *SessionInfo, req DifyExecuteRequest) {
	taskID, ok := req.Params["task_id"].(float64)
	if !ok {
		c.JSON(http.StatusBadRequest, DifyExecuteResponse{
			Success: false,
			Message: "缺少参数: task_id",
		})
		return
	}

	// 检查是否已打卡
	completed, err := h.taskService.HasCompletedToday(int(taskID), session.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, DifyExecuteResponse{
			Success: false,
			Message: "检查打卡状态失败",
		})
		return
	}

	if completed {
		c.JSON(http.StatusOK, DifyExecuteResponse{
			Success: false,
			Message: "✅ 您今天已经打过卡了！",
		})
		return
	}

	// 记录打卡
	record := &models.CompletionRecord{
		TaskID:      int(taskID),
		UserID:      session.UserID,
		GroupChatID: session.GroupChatID,
		IsOnTime:    true,
	}

	if err := h.taskService.RecordCompletion(record); err != nil {
		c.JSON(http.StatusInternalServerError, DifyExecuteResponse{
			Success: false,
			Message: "打卡失败",
		})
		return
	}

	c.JSON(http.StatusOK, DifyExecuteResponse{
		Success: true,
		Message: "✅ 打卡成功！",
		Data:    record,
	})
}

func (h *DifyHandler) handleViewStats(c *gin.Context, session *SessionInfo, req DifyExecuteRequest) {
	taskID, ok := req.Params["task_id"].(float64)
	if !ok {
		c.JSON(http.StatusBadRequest, DifyExecuteResponse{
			Success: false,
			Message: "缺少参数: task_id",
		})
		return
	}

	stats, err := h.statsService.GetTodayStats(int(taskID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, DifyExecuteResponse{
			Success: false,
			Message: "获取统计失败",
		})
		return
	}

	c.JSON(http.StatusOK, DifyExecuteResponse{
		Success: true,
		Message: "📊 统计数据",
		Data:    stats,
	})
}

func (h *DifyHandler) handleAddAdmin(c *gin.Context, session *SessionInfo, req DifyExecuteRequest) {
	targetUserID, _ := req.Params["target_user_id"].(string)
	targetUsername, _ := req.Params["target_username"].(string)

	if targetUserID == "" {
		c.JSON(http.StatusBadRequest, DifyExecuteResponse{
			Success: false,
			Message: "缺少参数: target_user_id",
		})
		return
	}

	err := h.permService.PromoteToAdmin(c.Request.Context(), session.UserID, targetUserID, targetUsername)
	if err != nil {
		c.JSON(http.StatusForbidden, DifyExecuteResponse{
			Success: false,
			Message: "添加管理员失败",
			Reason:  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, DifyExecuteResponse{
		Success: true,
		Message: fmt.Sprintf("✅ 成功将 %s 添加为子管理员", targetUsername),
	})
}

func (h *DifyHandler) handleRemoveAdmin(c *gin.Context, session *SessionInfo, req DifyExecuteRequest) {
	targetUserID, _ := req.Params["target_user_id"].(string)

	if targetUserID == "" {
		c.JSON(http.StatusBadRequest, DifyExecuteResponse{
			Success: false,
			Message: "缺少参数: target_user_id",
		})
		return
	}

	err := h.permService.DemoteFromAdmin(c.Request.Context(), session.UserID, targetUserID)
	if err != nil {
		c.JSON(http.StatusForbidden, DifyExecuteResponse{
			Success: false,
			Message: "移除管理员失败",
			Reason:  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, DifyExecuteResponse{
		Success: true,
		Message: "✅ 已移除管理员权限",
	})
}

// ========================================
// 会话管理 API（供后台内部调用）
// ========================================

// RegisterSession 注册会话（由 message_handler 调用）
func (h *DifyHandler) RegisterSession(conversationID, userID, username, groupChatID string) {
	h.sessionStore.SaveSession(conversationID, &SessionInfo{
		UserID:         userID,
		Username:       username,
		GroupChatID:    groupChatID,
		ConversationID: conversationID,
	})
}

// GetSessionStore 获取会话存储（供其他模块使用）
func (h *DifyHandler) GetSessionStore() *SessionStore {
	return h.sessionStore
}

// ========================================
// 发送消息 API（供 Dify 调用）
// ========================================

// SendMessageRequest 发送消息请求
type SendMessageRequest struct {
	ConversationID string `json:"conversation_id" binding:"required"`
	Message        string `json:"message" binding:"required"`
}

// SendMessageResponse 发送消息响应
type SendMessageResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// SendMessage 发送消息给钉钉群聊
// POST /api/v1/dify/send_message
func (h *DifyHandler) SendMessage(c *gin.Context) {
	var req SendMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, SendMessageResponse{
			Success: false,
			Message: "请求参数错误",
		})
		return
	}

	log.Printf("Dify 请求发送消息: conversation_id=%s, message=%s",
		req.ConversationID, req.Message)

	// 发送消息到钉钉群聊
	if err := h.dtClient.SendGroupMessage(req.ConversationID, req.Message); err != nil {
		log.Printf("发送钉钉消息失败: %v", err)
		c.JSON(http.StatusInternalServerError, SendMessageResponse{
			Success: false,
			Message: "发送消息失败",
		})
		return
	}

	c.JSON(http.StatusOK, SendMessageResponse{
		Success: true,
		Message: "消息发送成功",
	})
}
