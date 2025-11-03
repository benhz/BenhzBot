package scheduler

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"dingteam-bot/internal/dingtalk"
	"dingteam-bot/internal/models"
	"dingteam-bot/internal/services"

	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	cron        *cron.Cron
	taskService *services.TaskService
	dtClient    *dingtalk.Client
	location    *time.Location
}

func NewScheduler(taskService *services.TaskService, dtClient *dingtalk.Client, timezone string) (*Scheduler, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("加载时区失败: %w", err)
	}

	c := cron.New(cron.WithLocation(loc), cron.WithSeconds())

	return &Scheduler{
		cron:        c,
		taskService: taskService,
		dtClient:    dtClient,
		location:    loc,
	}, nil
}

// 启动调度器
func (s *Scheduler) Start(ctx context.Context) error {
	// 加载所有活跃任务
	tasks, err := s.taskService.GetPendingTasks()
	if err != nil {
		return fmt.Errorf("加载任务失败: %w", err)
	}

	// 注册每个任务
	for _, task := range tasks {
		if err := s.registerTask(task); err != nil {
			log.Printf("注册任务 [%s] 失败: %v", task.Name, err)
			continue
		}
	}

	// 启动 cron
	s.cron.Start()
	log.Printf("✓ 调度器已启动，共加载 %d 个任务", len(tasks))

	// 定期重新加载任务（每 5 分钟）
	go s.periodicReload(ctx)

	return nil
}

// 注册任务到 cron
func (s *Scheduler) registerTask(task models.Task) error {
	var cronExpr string

	// 根据任务类型调整 cron 表达式
	switch task.Type {
	case models.TaskTypeTask:
		// 任务类型：在截止时间执行检查
		cronExpr = task.CronExpr
	case models.TaskTypeNotification:
		// 通知类型：提前 N 分钟提醒
		cronExpr = task.CronExpr
	default:
		return fmt.Errorf("未知任务类型: %s", task.Type)
	}

	_, err := s.cron.AddFunc(cronExpr, func() {
		if err := s.executeTask(task); err != nil {
			log.Printf("执行任务 [%s] 失败: %v", task.Name, err)
		}
	})

	if err != nil {
		return fmt.Errorf("添加 cron 任务失败: %w", err)
	}

	log.Printf("✓ 注册任务: [%s] %s (类型: %s)", task.Name, task.CronExpr, task.Type)
	return nil
}

// 执行任务
func (s *Scheduler) executeTask(task models.Task) error {
	now := time.Now()
	log.Printf("执行任务: [%s] %s", task.Name, now.Format("2006-01-02 15:04:05"))

	var message string
	var reminderType string

	switch task.Type {
	case models.TaskTypeTask:
		// 任务类型：检查是否过期
		message, reminderType = s.buildTaskMessage(task)
	case models.TaskTypeNotification:
		// 通知类型：发送提醒
		message, reminderType = s.buildNotificationMessage(task)
	}

	// 发送群消息
	if err := s.dtClient.SendMarkdown(task.GroupChatID, task.Name, message); err != nil {
		return fmt.Errorf("发送消息失败: %w", err)
	}

	// 记录提醒日志
	log := &models.ReminderLog{
		TaskID:       task.ID,
		GroupChatID:  task.GroupChatID,
		ReminderType: reminderType,
		MessageText:  sql.NullString{String: message, Valid: true},
	}
	if err := s.taskService.LogReminder(log); err != nil {
		return fmt.Errorf("记录日志失败: %w", err)
	}

	// 更新任务运行时间
	nextRun := s.cron.Entry(cron.EntryID(task.ID)).Next
	if err := s.taskService.UpdateTaskRunTime(task.ID, now, nextRun); err != nil {
		return fmt.Errorf("更新运行时间失败: %w", err)
	}

	return nil
}

// buildTaskReminderMessage 构建任务提醒消息（卡片格式）
func (s *Scheduler) buildTaskReminderMessage(task models.Task, reminderType models.ReminderType, incompleteCount int) (string, string, []dingtalk.ActionButton) {
	now := time.Now()
	deadline := task.DeadlineTime.Time.In(s.location)

	var title, status string

	switch reminderType {
	case models.ReminderTypeMorning10AM:
		title = "🌅 早安提醒"
		status = fmt.Sprintf("今日需完成，截止时间: %s", deadline.Format("15:04"))

	case models.ReminderTypeAdvance1Hour:
		title = "⏰ 提前1小时提醒"
		status = fmt.Sprintf("距离截止时间还有1小时，截止时间: %s", deadline.Format("15:04"))

	case models.ReminderTypeDeadline:
		if now.After(deadline) {
			title = "🔴 超时通报"
			status = "**任务已超时，请尽快完成！**"
		} else {
			title = "⏰ 截止时间提醒"
			status = fmt.Sprintf("现在是截止时间: %s", deadline.Format("15:04"))
		}
	}

	// 构建卡片文本内容
	text := fmt.Sprintf(
		"### %s\n\n"+
			"📋 任务: **%s**\n"+
			"⏰ %s\n"+
			"👥 当前未完成人数: **%d 人**\n\n"+
			"%s\n\n"+
			"完成后请点击下方按钮或回复: @我 已完成 #%d",
		title,
		task.Name,
		status,
		incompleteCount,
		task.Description.String,
		task.ID,
	)

	// 构建按钮（暂时使用占位 URL，后续可以改为实际的回调 API）
	buttons := []dingtalk.ActionButton{
		{
			Title:     "👀 我已收到",
			ActionURL: fmt.Sprintf("dingtalk://dingtalkclient/action/sendmsg?content=@机器人 已收到 #%d", task.ID),
		},
		{
			Title:     "✅ 我已完成",
			ActionURL: fmt.Sprintf("dingtalk://dingtalkclient/action/sendmsg?content=@机器人 已完成 #%d", task.ID),
		},
	}

	return title, text, buttons
}

// 构建任务消息（过期检查）
func (s *Scheduler) buildTaskMessage(task models.Task) (string, string) {
	now := time.Now()
	deadline := task.DeadlineTime.Time

	// 检查是否过期
	if now.After(deadline) {
		// 过期了，获取未完成名单
		// TODO: 集成群成员列表
		message := fmt.Sprintf(
			"⏰ **任务超时通报**\n\n"+
				"📋 任务: %s (ID: #%d)\n"+
				"⏰ 截止时间: %s\n"+
				"🔴 当前状态: **已超时**\n\n"+
				"请尽快完成任务！\n\n"+
				"完成后回复: @我 已完成 #%d",
			task.Name,
			task.ID,
			deadline.Format("15:04"),
			task.ID,
		)
		return message, "OVERDUE"
	}

	// 未过期，发送普通提醒
	message := fmt.Sprintf(
		"⏰ **任务提醒**\n\n"+
			"📋 任务: %s (ID: #%d)\n"+
			"⏰ 截止时间: %s\n"+
			"📝 请记得及时完成任务\n\n"+
			"完成后回复: @我 已完成 #%d",
		task.Name,
		task.ID,
		deadline.Format("15:04"),
		task.ID,
	)
	return message, "NORMAL"
}

// 构建通知消息
func (s *Scheduler) buildNotificationMessage(task models.Task) (string, string) {
	message := fmt.Sprintf(
		"🔔 **提醒通知**\n\n"+
			"📋 %s (ID: #%d)\n"+
			"⏰ 时间: %s\n\n"+
			"%s",
		task.Name,
		task.ID,
		time.Now().Add(time.Duration(task.AdvanceMinutes)*time.Minute).Format("15:04"),
		task.Description.String,
	)
	return message, "ADVANCE"
}

// 定期重新加载任务
func (s *Scheduler) periodicReload(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			log.Println("重新加载任务...")
			// TODO: 实现增量更新逻辑
		}
	}
}

// 停止调度器
func (s *Scheduler) Stop() {
	if s.cron != nil {
		ctx := s.cron.Stop()
		<-ctx.Done()
		log.Println("✓ 调度器已停止")
	}
}
