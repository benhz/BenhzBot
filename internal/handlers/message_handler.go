package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"log"
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
	dtClient     *dingtalk.Client
}

func NewMessageHandler(
	cfg *config.Config,
	taskService *services.TaskService,
	statsService *services.StatsService,
	dtClient *dingtalk.Client,
) *MessageHandler {
	return &MessageHandler{
		cfg:          cfg,
		taskService:  taskService,
		statsService: statsService,
		dtClient:     dtClient,
	}
}

// 处理群消息
func (h *MessageHandler) HandleMessage(ctx context.Context, msg *dingtalk.IncomingMessage) error {
	// 只处理 @ 机器人的消息
	if !msg.IsInAtList {
		return nil
	}

	// 提取纯文本内容（去除 @机器人 部分）
	content := h.extractContent(msg.Text.Content)
	content = strings.TrimSpace(content)

	log.Printf("处理指令: %s (来自 %s)", content, msg.SenderNick)

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

**管理员命令：**
• @我 创建任务 <名称> <cron> [截止时间] [类型]
  例: 创建任务 写周报 0 17 * * 5 15:00 TASK

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
