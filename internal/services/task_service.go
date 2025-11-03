package services

import (
	"database/sql"
	"fmt"
	"time"

	"dingteam-bot/internal/models"
)

type TaskService struct {
	db                      *sql.DB
	onTaskCreatedCallback   func(models.Task) // 任务创建后的回调
}

func NewTaskService(db *sql.DB) *TaskService {
	return &TaskService{db: db}
}

// SetOnTaskCreatedCallback 设置任务创建后的回调函数
func (s *TaskService) SetOnTaskCreatedCallback(callback func(models.Task)) {
	s.onTaskCreatedCallback = callback
}

// 创建任务
func (s *TaskService) CreateTask(task *models.Task) error {
	query := `
		INSERT INTO tasks (
			name, description, type, cron_expr, deadline_time, advance_minutes,
			group_chat_id, group_chat_name, creator_user_id, creator_name, status
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, created_at, updated_at
	`

	err := s.db.QueryRow(
		query,
		task.Name,
		task.Description,
		task.Type,
		task.CronExpr,
		task.DeadlineTime,
		task.AdvanceMinutes,
		task.GroupChatID,
		task.GroupChatName,
		task.CreatorUserID,
		task.CreatorName,
		task.Status,
	).Scan(&task.ID, &task.CreatedAt, &task.UpdatedAt)

	if err != nil {
		return err
	}

	// 任务创建成功后，调用回调函数（如果已设置）
	if s.onTaskCreatedCallback != nil {
		s.onTaskCreatedCallback(*task)
	}

	return nil
}

// 获取群组的活跃任务
func (s *TaskService) GetActiveTasksByGroup(groupChatID string) ([]models.Task, error) {
	query := `
		SELECT id, name, description, type, cron_expr, deadline_time, advance_minutes,
			   group_chat_id, group_chat_name, creator_user_id, creator_name, status,
			   created_at, updated_at, last_run_at, next_run_at
		FROM tasks
		WHERE group_chat_id = $1 AND status = 'ACTIVE'
		ORDER BY created_at DESC
	`

	rows, err := s.db.Query(query, groupChatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []models.Task
	for rows.Next() {
		var task models.Task
		if err := rows.Scan(
			&task.ID, &task.Name, &task.Description, &task.Type, &task.CronExpr,
			&task.DeadlineTime, &task.AdvanceMinutes, &task.GroupChatID, &task.GroupChatName,
			&task.CreatorUserID, &task.CreatorName, &task.Status,
			&task.CreatedAt, &task.UpdatedAt, &task.LastRunAt, &task.NextRunAt,
		); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}

	return tasks, nil
}

// 获取所有需要执行的任务
func (s *TaskService) GetPendingTasks() ([]models.Task, error) {
	query := `
		SELECT id, name, description, type, cron_expr, deadline_time, advance_minutes,
			   group_chat_id, group_chat_name, creator_user_id, creator_name, status,
			   created_at, updated_at, last_run_at, next_run_at
		FROM tasks
		WHERE status = 'ACTIVE'
		ORDER BY created_at DESC
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []models.Task
	for rows.Next() {
		var task models.Task
		if err := rows.Scan(
			&task.ID, &task.Name, &task.Description, &task.Type, &task.CronExpr,
			&task.DeadlineTime, &task.AdvanceMinutes, &task.GroupChatID, &task.GroupChatName,
			&task.CreatorUserID, &task.CreatorName, &task.Status,
			&task.CreatedAt, &task.UpdatedAt, &task.LastRunAt, &task.NextRunAt,
		); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}

	return tasks, nil
}

// 更新任务状态
func (s *TaskService) UpdateTaskStatus(taskID int, status models.TaskStatus) error {
	query := `UPDATE tasks SET status = $1 WHERE id = $2`
	_, err := s.db.Exec(query, status, taskID)
	return err
}

// 更新任务运行时间
func (s *TaskService) UpdateTaskRunTime(taskID int, lastRun, nextRun time.Time) error {
	query := `UPDATE tasks SET last_run_at = $1, next_run_at = $2 WHERE id = $3`
	_, err := s.db.Exec(query, lastRun, nextRun, taskID)
	return err
}

// 删除任务
func (s *TaskService) DeleteTask(taskID int) error {
	query := `UPDATE tasks SET status = 'DELETED' WHERE id = $1`
	_, err := s.db.Exec(query, taskID)
	return err
}

// 记录完成
func (s *TaskService) RecordCompletion(record *models.CompletionRecord) error {
	query := `
		INSERT INTO completion_records (
			task_id, user_id, user_name, group_chat_id, task_date, is_on_time
		) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (task_id, user_id, task_date) DO NOTHING
		RETURNING id, completed_at
	`

	err := s.db.QueryRow(
		query,
		record.TaskID,
		record.UserID,
		record.UserName,
		record.GroupChatID,
		record.TaskDate,
		record.IsOnTime,
	).Scan(&record.ID, &record.CompletedAt)

	if err == sql.ErrNoRows {
		return fmt.Errorf("今天已经打卡过了")
	}

	return err
}

// 检查今天是否已完成
func (s *TaskService) HasCompletedToday(taskID int, userID string) (bool, error) {
	today := time.Now().Format("2006-01-02")
	query := `
		SELECT EXISTS(
			SELECT 1 FROM completion_records
			WHERE task_id = $1 AND user_id = $2 AND task_date = $3
		)
	`

	var exists bool
	err := s.db.QueryRow(query, taskID, userID, today).Scan(&exists)
	return exists, err
}

// 记录提醒日志
func (s *TaskService) LogReminder(log *models.ReminderLog) error {
	query := `
		INSERT INTO reminder_logs (
			task_id, group_chat_id, reminder_type, message_text, member_count, completed_count
		) VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, sent_at
	`

	return s.db.QueryRow(
		query,
		log.TaskID,
		log.GroupChatID,
		log.ReminderType,
		log.MessageText,
		log.MemberCount,
		log.CompletedCount,
	).Scan(&log.ID, &log.SentAt)
}

// 获取今日未完成任务的用户列表
func (s *TaskService) GetIncompleteUsersToday(taskID int, groupChatID string) ([]string, error) {
	today := time.Now().Format("2006-01-02")

	// 获取群组中所有非领导用户（排除 super_admin）
	allUsersQuery := `
		SELECT DISTINCT u.dingtalk_user_id
		FROM users u
		WHERE u.role != 'super_admin'
	`

	rows, err := s.db.Query(allUsersQuery)
	if err != nil {
		return nil, fmt.Errorf("获取用户列表失败: %w", err)
	}
	defer rows.Close()

	var allUsers []string
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			continue
		}
		allUsers = append(allUsers, userID)
	}

	// 获取今日已完成的用户
	completedQuery := `
		SELECT user_id
		FROM completion_records
		WHERE task_id = $1 AND task_date = $2
	`

	completedRows, err := s.db.Query(completedQuery, taskID, today)
	if err != nil {
		return nil, fmt.Errorf("获取完成记录失败: %w", err)
	}
	defer completedRows.Close()

	completedMap := make(map[string]bool)
	for completedRows.Next() {
		var userID string
		if err := completedRows.Scan(&userID); err != nil {
			continue
		}
		completedMap[userID] = true
	}

	// 计算未完成的用户
	var incompleteUsers []string
	for _, userID := range allUsers {
		if !completedMap[userID] {
			incompleteUsers = append(incompleteUsers, userID)
		}
	}

	return incompleteUsers, nil
}

// 获取群组中所有非领导用户（用于@all，但排除领导）
func (s *TaskService) GetAllNonLeaderUsers() ([]string, error) {
	query := `
		SELECT dingtalk_user_id
		FROM users
		WHERE role != 'super_admin'
		ORDER BY created_at
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("获取用户列表失败: %w", err)
	}
	defer rows.Close()

	var users []string
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			continue
		}
		users = append(users, userID)
	}

	return users, nil
}
