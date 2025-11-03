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

// 注册任务到 cron（为每个任务注册多个提醒时间点）
func (s *Scheduler) registerTask(task models.Task) error {
	switch task.Type {
	case models.TaskTypeTask:
		// 任务型：注册 3 个提醒
		// 1. 每天10点提醒
		if err := s.registerReminder(task, models.ReminderTypeMorning10AM, "0 0 10 * * *"); err != nil {
			log.Printf("注册10点提醒失败: %v", err)
		}

		// 2. 提前1小时提醒
		if task.DeadlineTime.Valid {
			cronExpr := s.calculateAdvanceReminderCron(task.DeadlineTime.Time, 60)
			if err := s.registerReminder(task, models.ReminderTypeAdvance1Hour, cronExpr); err != nil {
				log.Printf("注册提前1小时提醒失败: %v", err)
			}
		}

		// 3. 截止时间提醒
		if task.DeadlineTime.Valid {
			cronExpr := s.calculateDeadlineCron(task.DeadlineTime.Time)
			if err := s.registerReminder(task, models.ReminderTypeDeadline, cronExpr); err != nil {
				log.Printf("注册截止时间提醒失败: %v", err)
			}
		}

		log.Printf("✓ 注册任务型提醒: [%s] (10点 + 提前1小时 + 截止时间)", task.Name)

	case models.TaskTypeNotification:
		// 通知型：注册 3 个提醒
		// 1. 每天10点提醒
		if err := s.registerReminder(task, models.ReminderTypeMorning10AM, "0 0 10 * * *"); err != nil {
			log.Printf("注册10点提醒失败: %v", err)
		}

		// 2. 提前30分钟提醒（基于 cron 表达式计算）
		cronExpr30Min := s.calculateAdvanceReminderFromCron(task.CronExpr, 30)
		if cronExpr30Min != "" {
			if err := s.registerReminder(task, models.ReminderTypeAdvance30Min, cronExpr30Min); err != nil {
				log.Printf("注册提前30分钟提醒失败: %v", err)
			}
		}

		// 3. 触发时间提醒（使用原 cron 表达式）
		if err := s.registerReminder(task, models.ReminderTypeTrigger, task.CronExpr); err != nil {
			log.Printf("注册触发时间提醒失败: %v", err)
		}

		log.Printf("✓ 注册通知型提醒: [%s] (10点 + 提前30分钟 + 触发时间)", task.Name)

	default:
		return fmt.Errorf("未知任务类型: %s", task.Type)
	}

	return nil
}

// 注册单个提醒
func (s *Scheduler) registerReminder(task models.Task, reminderType models.ReminderType, cronExpr string) error {
	if cronExpr == "" {
		return fmt.Errorf("空的 cron 表达式")
	}

	_, err := s.cron.AddFunc(cronExpr, func() {
		if err := s.executeReminder(task, reminderType); err != nil {
			log.Printf("执行提醒 [%s - %s] 失败: %v", task.Name, reminderType, err)
		}
	})

	if err != nil {
		return fmt.Errorf("添加 cron 任务失败: %w", err)
	}

	return nil
}

// 计算截止时间的 cron 表达式
func (s *Scheduler) calculateDeadlineCron(deadline time.Time) string {
	// 将 deadline 转换为当前时区
	deadline = deadline.In(s.location)
	hour := deadline.Hour()
	minute := deadline.Minute()

	// 秒 分 时 日 月 周
	// 每天的指定时间执行
	return fmt.Sprintf("0 %d %d * * *", minute, hour)
}

// 计算提前提醒的 cron 表达式（基于截止时间）
func (s *Scheduler) calculateAdvanceReminderCron(deadline time.Time, advanceMinutes int) string {
	// 将 deadline 转换为当前时区
	deadline = deadline.In(s.location)

	// 计算提前时间
	reminderTime := deadline.Add(-time.Duration(advanceMinutes) * time.Minute)
	hour := reminderTime.Hour()
	minute := reminderTime.Minute()

	// 秒 分 时 日 月 周
	return fmt.Sprintf("0 %d %d * * *", minute, hour)
}

// 计算提前提醒的 cron 表达式（基于原 cron 表达式）
func (s *Scheduler) calculateAdvanceReminderFromCron(originalCron string, advanceMinutes int) string {
	// 解析原 cron 表达式提取时间
	// 假设格式为 "秒 分 时 日 月 周"
	// 简单实现：如果是固定时间的 cron，提前 N 分钟

	// 使用 cron 库解析
	schedule, err := cron.ParseStandard(originalCron)
	if err != nil {
		log.Printf("解析 cron 表达式失败: %v", err)
		return ""
	}

	// 获取下一次执行时间
	now := time.Now().In(s.location)
	nextTime := schedule.Next(now)

	// 提前 N 分钟
	reminderTime := nextTime.Add(-time.Duration(advanceMinutes) * time.Minute)
	hour := reminderTime.Hour()
	minute := reminderTime.Minute()

	// 生成新的 cron 表达式（每天同一时间）
	return fmt.Sprintf("0 %d %d * * *", minute, hour)
}

// 执行提醒
func (s *Scheduler) executeReminder(task models.Task, reminderType models.ReminderType) error {
	now := time.Now()
	log.Printf("执行提醒: [%s - %s] %s", task.Name, reminderType, now.Format("2006-01-02 15:04:05"))

	var message string
	var atUserIDs []string
	var err error

	// 根据任务类型和提醒类型构建消息和@用户列表
	switch task.Type {
	case models.TaskTypeTask:
		// 任务型：@未完成的人（排除领导）
		atUserIDs, err = s.taskService.GetIncompleteUsersToday(task.ID, task.GroupChatID)
		if err != nil {
			log.Printf("获取未完成用户失败: %v", err)
			atUserIDs = []string{}
		}
		message = s.buildTaskReminderMessage(task, reminderType, len(atUserIDs))

	case models.TaskTypeNotification:
		// 通知型：@所有人（排除领导）
		atUserIDs, err = s.taskService.GetAllNonLeaderUsers()
		if err != nil {
			log.Printf("获取用户列表失败: %v", err)
			atUserIDs = []string{}
		}
		message = s.buildNotificationReminderMessage(task, reminderType)
	}

	// 发送群消息（带@）
	if err := s.dtClient.SendMarkdownWithMentions(task.GroupChatID, task.Name, message, atUserIDs); err != nil {
		return fmt.Errorf("发送消息失败: %w", err)
	}

	// 记录提醒日志
	reminderLog := &models.ReminderLog{
		TaskID:       task.ID,
		GroupChatID:  task.GroupChatID,
		ReminderType: string(reminderType),
		MessageText:  sql.NullString{String: message, Valid: true},
		MemberCount:  len(atUserIDs),
	}
	if err := s.taskService.LogReminder(reminderLog); err != nil {
		return fmt.Errorf("记录日志失败: %w", err)
	}

	log.Printf("✓ 提醒已发送: [%s - %s] @%d人", task.Name, reminderType, len(atUserIDs))
	return nil
}

// 构建任务型提醒消息
func (s *Scheduler) buildTaskReminderMessage(task models.Task, reminderType models.ReminderType, incompleteCount int) string {
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

	message := fmt.Sprintf(
		"### %s\n\n"+
			"📋 任务: **%s**\n"+
			"⏰ %s\n"+
			"👥 当前未完成人数: **%d 人**\n\n"+
			"%s\n\n"+
			"完成后请回复: @我 已完成",
		title,
		task.Name,
		status,
		incompleteCount,
		task.Description.String,
	)

	return message
}

// 构建通知型提醒消息
func (s *Scheduler) buildNotificationReminderMessage(task models.Task, reminderType models.ReminderType) string {
	var title, timeInfo string

	switch reminderType {
	case models.ReminderTypeMorning10AM:
		title = "🌅 早安通知"
		timeInfo = "今日待办事项提醒"

	case models.ReminderTypeAdvance30Min:
		title = "⏰ 提前30分钟提醒"
		timeInfo = "即将开始，请做好准备"

	case models.ReminderTypeTrigger:
		title = "🔔 事件提醒"
		timeInfo = "现在是触发时间"
	}

	message := fmt.Sprintf(
		"### %s\n\n"+
			"📢 通知: **%s**\n"+
			"⏰ %s\n\n"+
			"%s",
		title,
		task.Name,
		timeInfo,
		task.Description.String,
	)

	return message
}


// RegisterNewTask 注册新创建的任务到调度器
func (s *Scheduler) RegisterNewTask(task models.Task) error {
	return s.registerTask(task)
}

// SendImmediateReminderIfNeeded 如果当前时间超过10点，立即发送10点提醒
func (s *Scheduler) SendImmediateReminderIfNeeded(task models.Task) {
	now := time.Now().In(s.location)
	hour := now.Hour()

	// 如果当前时间超过10点，立即发送10点提醒
	if hour >= 10 {
		log.Printf("当前时间已超过10点，立即发送提醒: [%s]", task.Name)
		if err := s.executeReminder(task, models.ReminderTypeMorning10AM); err != nil {
			log.Printf("立即发送提醒失败: %v", err)
		}
	}
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
