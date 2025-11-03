package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"dingteam-bot/internal/config"
	"dingteam-bot/internal/dingtalk"
	"dingteam-bot/internal/models"
	"dingteam-bot/internal/services"
)

type MessageHandler struct {
	cfg          *config.Config
	taskService  *services.TaskService
	statsService *services.StatsService
	permService  *services.PermissionService
	dtClient     *dingtalk.Client
	difyHandler  *DifyHandler
}

func NewMessageHandler(
	cfg *config.Config,
	taskService *services.TaskService,
	statsService *services.StatsService,
	permService *services.PermissionService,
	dtClient *dingtalk.Client,
	difyHandler *DifyHandler,
) *MessageHandler {
	return &MessageHandler{
		cfg:          cfg,
		taskService:  taskService,
		statsService: statsService,
		permService:  permService,
		dtClient:     dtClient,
		difyHandler:  difyHandler,
	}
}

// 处理群消息
func (h *MessageHandler) HandleMessage(ctx context.Context, msg *dingtalk.IncomingMessage) error {
	// 只处理 @ 机器人的消息
	if !msg.IsInAtList {
		return nil
	}

	// 注册会话信息（供 Dify 后续调用时使用）
	if h.difyHandler != nil {
		h.difyHandler.RegisterSession(
			msg.ConversationID,
			msg.SenderStaffID,
			msg.SenderNick,
			msg.ConversationID,
		)
	}

	// 提取纯文本内容（去除 @机器人 部分）
	content := h.extractContent(msg.Text.Content)
	content = strings.TrimSpace(content)

	log.Printf("处理指令: %s (来自 %s)", content, msg.SenderNick)

	// 如果启用了 Dify，则转发给 Dify 处理
	if h.cfg.Dify.Enabled {
		return h.forwardToDify(ctx, msg, content)
	}

	// 否则使用传统的命令匹配方式（兜底方案）
	return h.handleLegacyCommand(ctx, msg, content)
}

// forwardToDify 转发消息给 Dify 工作流处理
func (h *MessageHandler) forwardToDify(ctx context.Context, msg *dingtalk.IncomingMessage, content string) error {
	log.Printf("转发消息到 Dify: conversation_id=%s, user=%s, content=%s",
		msg.ConversationID, msg.SenderStaffID, content)

	// 构造 Dify 工作流 API 请求
	// 工作流 API 格式：{"inputs": {...}, "response_mode": "blocking", "user": "..."}
	payload := map[string]interface{}{
		"inputs": map[string]string{
			"user_input":      content,
			"conversation_id": msg.ConversationID,
		},
		"response_mode": "blocking",
		"user":          msg.SenderNick,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("序列化 Dify 请求失败: %v", err)
		return h.sendReply(msg, "❌ 消息处理失败")
	}

	// 发送请求到 Dify 工作流
	req, err := http.NewRequestWithContext(ctx, "POST", h.cfg.Dify.WebhookURL, bytes.NewBuffer(data))
	if err != nil {
		log.Printf("创建 Dify 请求失败: %v", err)
		return h.sendReply(msg, "❌ 消息处理失败")
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.cfg.Dify.APIKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("调用 Dify API 失败: %v", err)
		return h.sendReply(msg, "❌ 消息处理失败")
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		log.Printf("Dify API 返回错误状态码: %d, 响应: %s", resp.StatusCode, string(body))
		return h.sendReply(msg, "❌ 消息处理失败")
	}

	// 解析 Dify 工作流响应
	// 工作流响应格式：{"data": {"outputs": {"reply_polisher": "..."}}, ...}
	var difyResp struct {
		TaskID        string `json:"task_id"`
		WorkflowRunID string `json:"workflow_run_id"`
		Data          struct {
			ID      string                 `json:"id"`
			Status  string                 `json:"status"`
			Outputs map[string]interface{} `json:"outputs"`
			Error   string                 `json:"error"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &difyResp); err != nil {
		log.Printf("解析 Dify 响应失败: %v, 响应: %s", err, string(body))
		return h.sendReply(msg, "❌ 消息处理失败")
	}

	// 检查工作流执行状态
	if difyResp.Data.Status != "succeeded" {
		log.Printf("Dify 工作流执行失败: status=%s, error=%s", difyResp.Data.Status, difyResp.Data.Error)
		return h.sendReply(msg, "❌ 消息处理失败")
	}

	// 提取工作流输出中的回复内容
	// 尝试从常见的输出字段中提取回复
	var reply string
	for key, value := range difyResp.Data.Outputs {
		// 常见的回复字段名：reply, reply_polisher, text, answer 等
		if str, ok := value.(string); ok && str != "" {
			reply = str
			log.Printf("从工作流输出字段 '%s' 中提取到回复", key)
			break
		}
	}

	// 如果 Dify 返回了回复，则发送给用户
	if reply != "" {
		return h.sendReply(msg, reply)
	}

	// 如果没有回复，表示 Dify 可能已经通过工具调用处理了请求
	log.Printf("Dify 工作流处理完成，无直接回复")
	return nil
}

// handleLegacyCommand 处理传统命令（兜底方案）
func (h *MessageHandler) handleLegacyCommand(ctx context.Context, msg *dingtalk.IncomingMessage, content string) error {
	// 匹配不同的命令
	switch {
	case strings.Contains(content, "已完成") || strings.Contains(content, "我已提交"):
		return h.handleCompletion(msg)
	case strings.Contains(content, "统计") || strings.Contains(content, "报告"):
		return h.handleStats(msg, content)
	case strings.HasPrefix(content, "创建任务") || strings.HasPrefix(content, "新建任务"):
		return h.handleCreateTask(msg, content)
	case strings.Contains(content, "任务列表") || strings.Contains(content, "查看任务"):
		return h.handleListTasks(msg)
	case strings.HasPrefix(content, "添加管理员") || strings.HasPrefix(content, "提升管理员"):
		return h.handlePromoteAdmin(ctx, msg, content)
	case strings.HasPrefix(content, "移除管理员") || strings.HasPrefix(content, "降级管理员"):
		return h.handleDemoteAdmin(ctx, msg, content)
	case strings.Contains(content, "管理员列表"):
		return h.handleListAdmins(ctx, msg)
	case strings.Contains(content, "我的权限"):
		return h.handleMyPermissions(ctx, msg)
	case strings.Contains(content, "帮助") || content == "?":
		return h.handleHelp(msg)
	default:
		return h.sendReply(msg, "❓ 未识别的命令，发送「帮助」查看可用指令")
	}
}

// 处理打卡
func (h *MessageHandler) handleCompletion(msg *dingtalk.IncomingMessage) error {
	// 获取该群的活跃任务
	tasks, err := h.taskService.GetActiveTasksByGroup(msg.ConversationID)
	if err != nil {
		return h.sendReply(msg, fmt.Sprintf("❌ 查询任务失败: %v", err))
	}

	if len(tasks) == 0 {
		return h.sendReply(msg, "❌ 当前群没有活跃的任务")
	}

	// 默认打卡第一个任务（实际应该让用户选择）
	task := tasks[0]

	// 检查是否已打卡
	completed, err := h.taskService.HasCompletedToday(task.ID, msg.SenderStaffID)
	if err != nil {
		return h.sendReply(msg, fmt.Sprintf("❌ 检查打卡状态失败: %v", err))
	}

	if completed {
		return h.sendReply(msg, "✅ 您今天已经打过卡了！")
	}

	// 判断是否按时完成
	isOnTime := true
	if task.Type == models.TaskTypeTask && task.DeadlineTime.Valid {
		now := time.Now()
		deadline := task.DeadlineTime.Time
		if now.After(deadline) {
			isOnTime = false
		}
	}

	// 记录完成
	record := &models.CompletionRecord{
		TaskID:      task.ID,
		UserID:      msg.SenderStaffID,
		UserName:    sql.NullString{String: msg.SenderNick, Valid: true},
		GroupChatID: msg.ConversationID,
		TaskDate:    time.Now(),
		IsOnTime:    isOnTime,
	}

	if err := h.taskService.RecordCompletion(record); err != nil {
		return h.sendReply(msg, fmt.Sprintf("❌ 打卡失败: %v", err))
	}

	status := "✅"
	if !isOnTime {
		status = "⏰"
	}

	return h.sendReply(msg, fmt.Sprintf("%s 打卡成功！任务: %s", status, task.Name))
}

// 处理统计查询
func (h *MessageHandler) handleStats(msg *dingtalk.IncomingMessage, content string) error {
	// 获取该群的活跃任务
	tasks, err := h.taskService.GetActiveTasksByGroup(msg.ConversationID)
	if err != nil {
		return h.sendReply(msg, fmt.Sprintf("❌ 查询任务失败: %v", err))
	}

	if len(tasks) == 0 {
		return h.sendReply(msg, "❌ 当前群没有活跃的任务")
	}

	// 默认统计第一个任务
	task := tasks[0]

	stats, err := h.statsService.GetTodayStats(task.ID)
	if err != nil {
		return h.sendReply(msg, fmt.Sprintf("❌ 获取统计失败: %v", err))
	}

	report := h.statsService.FormatStatsReport(stats)
	return h.sendReply(msg, report)
}

// 处理创建任务
func (h *MessageHandler) handleCreateTask(msg *dingtalk.IncomingMessage, content string) error {
	// 检查权限
	if !h.cfg.IsAdmin(msg.SenderStaffID) {
		return h.sendReply(msg, "❌ 只有管理员可以创建任务")
	}

	// 解析命令
	// 格式: 创建任务 <名称> <cron表达式> [截止时间] [类型]
	// 例如: 创建任务 写周报 0 17 * * 5 15:00 TASK
	task, err := h.parseCreateTaskCommand(content, msg)
	if err != nil {
		return h.sendReply(msg, fmt.Sprintf("❌ 解析命令失败: %v\n\n格式: 创建任务 <名称> <cron表达式> [截止时间] [类型]", err))
	}

	if err := h.taskService.CreateTask(task); err != nil {
		return h.sendReply(msg, fmt.Sprintf("❌ 创建任务失败: %v", err))
	}

	return h.sendReply(msg, fmt.Sprintf("✅ 任务创建成功！\n\n📋 名称: %s\n⏰ Cron: %s\n📊 类型: %s", task.Name, task.CronExpr, task.Type))
}

// 处理任务列表
func (h *MessageHandler) handleListTasks(msg *dingtalk.IncomingMessage) error {
	tasks, err := h.taskService.GetActiveTasksByGroup(msg.ConversationID)
	if err != nil {
		return h.sendReply(msg, fmt.Sprintf("❌ 查询失败: %v", err))
	}

	if len(tasks) == 0 {
		return h.sendReply(msg, "当前群没有活跃的任务")
	}

	var list strings.Builder
	list.WriteString("📋 **当前任务列表**\n\n")
	for i, task := range tasks {
		list.WriteString(fmt.Sprintf("%d. %s\n", i+1, task.Name))
		list.WriteString(fmt.Sprintf("   - 类型: %s\n", task.Type))
		list.WriteString(fmt.Sprintf("   - Cron: %s\n", task.CronExpr))
		if task.DeadlineTime.Valid {
			list.WriteString(fmt.Sprintf("   - 截止: %s\n", task.DeadlineTime.Time.Format("15:04")))
		}
		list.WriteString("\n")
	}

	return h.sendReply(msg, list.String())
}

// 处理帮助
func (h *MessageHandler) handleHelp(msg *dingtalk.IncomingMessage) error {
	help := `📖 **DingTeam Bot 使用指南**

**基本命令：**
• @我 已完成 - 打卡完成任务
• @我 统计 - 查看今日完成统计
• @我 任务列表 - 查看所有任务
• @我 我的权限 - 查看我的权限

**子管理员命令：**
• @我 创建任务 <名称> <cron> [截止时间] [类型]
  例: 创建任务 写周报 0 17 * * 5 15:00 TASK

**主管理员命令：**
• @我 添加管理员 @用户 - 将用户提升为子管理员
• @我 移除管理员 @用户 - 移除用户的子管理员权限
• @我 管理员列表 - 查看所有管理员

**Cron 表达式示例：**
• 0 9 * * 1-5 (工作日上午9点)
• 0 17 * * 5 (每周五下午5点)
• 0 0 * * * (每天0点)

**任务类型：**
• TASK - 任务型（过期通报）
• NOTIFICATION - 通知型（提前提醒）`

	return h.sendReply(msg, help)
}

// 解析创建任务命令
func (h *MessageHandler) parseCreateTaskCommand(content string, msg *dingtalk.IncomingMessage) (*models.Task, error) {
	// 简化版解析（实际应该更严格）
	parts := strings.Fields(content)
	if len(parts) < 3 {
		return nil, fmt.Errorf("参数不足")
	}

	task := &models.Task{
		Name:           parts[1],
		Type:           models.TaskTypeNotification,
		CronExpr:       parts[2],
		GroupChatID:    msg.ConversationID,
		GroupChatName:  sql.NullString{String: msg.ConversationTitle, Valid: true},
		CreatorUserID:  msg.SenderStaffID,
		CreatorName:    sql.NullString{String: msg.SenderNick, Valid: true},
		Status:         models.TaskStatusActive,
		AdvanceMinutes: 30,
	}

	// 解析可选参数
	if len(parts) >= 4 {
		// 截止时间
		if deadline, err := time.Parse("15:04", parts[3]); err == nil {
			task.DeadlineTime = sql.NullTime{Time: deadline, Valid: true}
		}
	}

	if len(parts) >= 5 {
		// 任务类型
		if parts[4] == "TASK" {
			task.Type = models.TaskTypeTask
		}
	}

	return task, nil
}

// 提取内容（去除 @机器人）
func (h *MessageHandler) extractContent(rawContent string) string {
	// 去除 @用户ID 格式
	re := regexp.MustCompile(`@\S+\s*`)
	return re.ReplaceAllString(rawContent, "")
}

// 发送回复
func (h *MessageHandler) sendReply(msg *dingtalk.IncomingMessage, content string) error {
	return h.dtClient.SendGroupMessage(msg.ConversationID, content)
}

// 处理卡片回调
func (h *MessageHandler) HandleCardCallback(ctx context.Context, callback *dingtalk.CardCallback) error {
	// TODO: 实现卡片按钮回调处理
	log.Printf("收到卡片回调: %+v", callback)
	return nil
}

// ========================================
// 权限管理相关命令处理
// ========================================

// handlePromoteAdmin 处理添加管理员命令
// 格式: @机器人 添加管理员 @用户
func (h *MessageHandler) handlePromoteAdmin(ctx context.Context, msg *dingtalk.IncomingMessage, content string) error {
	// 提取被提升用户的ID
	// 钉钉的消息格式中，@用户的格式是 @{dingtalkId:xxx}
	targetUserID, targetUsername := h.extractMentionedUser(msg)
	if targetUserID == "" {
		return h.sendReply(msg, "❌ 请在命令中 @ 要添加为管理员的用户\n例如: @我 添加管理员 @张三")
	}

	// 执行提升操作
	err := h.permService.PromoteToAdmin(ctx, msg.SenderStaffID, targetUserID, targetUsername)
	if err != nil {
		return h.sendReply(msg, fmt.Sprintf("❌ 添加管理员失败: %v", err))
	}

	return h.sendReply(msg, fmt.Sprintf("✅ 成功将 %s 添加为子管理员！", targetUsername))
}

// handleDemoteAdmin 处理移除管理员命令
// 格式: @机器人 移除管理员 @用户
func (h *MessageHandler) handleDemoteAdmin(ctx context.Context, msg *dingtalk.IncomingMessage, content string) error {
	// 提取被降级用户的ID
	targetUserID, targetUsername := h.extractMentionedUser(msg)
	if targetUserID == "" {
		return h.sendReply(msg, "❌ 请在命令中 @ 要移除管理员权限的用户\n例如: @我 移除管理员 @张三")
	}

	// 执行降级操作
	err := h.permService.DemoteFromAdmin(ctx, msg.SenderStaffID, targetUserID)
	if err != nil {
		return h.sendReply(msg, fmt.Sprintf("❌ 移除管理员失败: %v", err))
	}

	return h.sendReply(msg, fmt.Sprintf("✅ 已移除 %s 的子管理员权限", targetUsername))
}

// handleListAdmins 处理查看管理员列表命令
func (h *MessageHandler) handleListAdmins(ctx context.Context, msg *dingtalk.IncomingMessage) error {
	// 获取所有主管理员
	superAdmins, err := h.permService.ListUsersByRole(ctx, models.RoleSuperAdmin)
	if err != nil {
		return h.sendReply(msg, fmt.Sprintf("❌ 查询管理员列表失败: %v", err))
	}

	// 获取所有子管理员
	admins, err := h.permService.ListUsersByRole(ctx, models.RoleAdmin)
	if err != nil {
		return h.sendReply(msg, fmt.Sprintf("❌ 查询管理员列表失败: %v", err))
	}

	var result strings.Builder
	result.WriteString("👥 **管理员列表**\n\n")

	// 主管理员
	if len(superAdmins) > 0 {
		result.WriteString("**主管理员：**\n")
		for i, admin := range superAdmins {
			result.WriteString(fmt.Sprintf("%d. %s (ID: %s)\n", i+1, admin.Username, admin.DingTalkUserID))
		}
		result.WriteString("\n")
	}

	// 子管理员
	if len(admins) > 0 {
		result.WriteString("**子管理员：**\n")
		for i, admin := range admins {
			result.WriteString(fmt.Sprintf("%d. %s (ID: %s)\n", i+1, admin.Username, admin.DingTalkUserID))
		}
	} else {
		result.WriteString("**子管理员：** 暂无\n")
	}

	return h.sendReply(msg, result.String())
}

// handleMyPermissions 处理查看我的权限命令
func (h *MessageHandler) handleMyPermissions(ctx context.Context, msg *dingtalk.IncomingMessage) error {
	// 获取或创建用户
	user, err := h.permService.GetOrCreateUser(ctx, msg.SenderStaffID, msg.SenderNick)
	if err != nil {
		return h.sendReply(msg, fmt.Sprintf("❌ 查询权限失败: %v", err))
	}

	// 获取权限列表
	permissions, err := h.permService.GetUserPermissions(ctx, msg.SenderStaffID)
	if err != nil {
		return h.sendReply(msg, fmt.Sprintf("❌ 查询权限失败: %v", err))
	}

	// 构建权限描述
	var result strings.Builder
	result.WriteString("🔐 **您的权限信息**\n\n")
	result.WriteString(fmt.Sprintf("**用户名：** %s\n", user.Username))
	result.WriteString(fmt.Sprintf("**角色：** %s\n\n", h.getRoleDisplayName(user.Role)))
	result.WriteString("**拥有的权限：**\n")

	for i, perm := range permissions {
		result.WriteString(fmt.Sprintf("%d. %s - %s\n", i+1, perm, h.getPermissionDisplayName(perm)))
	}

	return h.sendReply(msg, result.String())
}

// extractMentionedUser 从消息中提取被@的用户ID和用户名
func (h *MessageHandler) extractMentionedUser(msg *dingtalk.IncomingMessage) (string, string) {
	// 钉钉消息中，AtUsers 字段包含了所有被 @ 的用户
	// 我们需要排除机器人自己，取第一个被 @ 的用户
	// 注意：这里的逻辑可能需要根据实际的钉钉SDK结构调整

	// 从消息文本中提取 @{dingtalkId:xxx} 格式
	re := regexp.MustCompile(`dingtalkId:([a-zA-Z0-9]+)`)
	matches := re.FindAllStringSubmatch(msg.Text.Content, -1)

	if len(matches) >= 2 {
		// 第一个通常是机器人自己，第二个才是目标用户
		userID := matches[1][1]
		// 用户名可以从消息中解析，这里简单返回ID
		return userID, userID
	}

	return "", ""
}

// getRoleDisplayName 获取角色的显示名称
func (h *MessageHandler) getRoleDisplayName(role models.UserRole) string {
	switch role {
	case models.RoleSuperAdmin:
		return "主管理员 (拥有所有权限)"
	case models.RoleAdmin:
		return "子管理员 (可管理任务)"
	case models.RoleMember:
		return "普通成员 (可打卡和查看)"
	default:
		return string(role)
	}
}

// getPermissionDisplayName 获取权限的显示名称
func (h *MessageHandler) getPermissionDisplayName(perm string) string {
	permMap := map[string]string{
		"add_admin":     "添加子管理员",
		"remove_admin":  "移除子管理员",
		"create_task":   "创建任务",
		"update_task":   "更新任务",
		"delete_task":   "删除任务",
		"list_tasks":    "查看任务列表",
		"complete_task": "打卡完成任务",
		"view_stats":    "查看统计",
	}

	if display, ok := permMap[perm]; ok {
		return display
	}
	return perm
}
