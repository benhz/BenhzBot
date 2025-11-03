package models

import (
	"database/sql"
	"time"
)

type TaskType string

const (
	TaskTypeTask         TaskType = "TASK"         // 任务类型：过点未完成则通报
	TaskTypeNotification TaskType = "NOTIFICATION" // 通知类型：提前提醒
)

type TaskStatus string

const (
	TaskStatusActive  TaskStatus = "ACTIVE"
	TaskStatusPaused  TaskStatus = "PAUSED"
	TaskStatusDeleted TaskStatus = "DELETED"
)

type ReminderType string

const (
	ReminderTypeMorning10AM   ReminderType = "MORNING_10AM"   // 早上10点提醒
	ReminderTypeAdvance1Hour  ReminderType = "ADVANCE_1HOUR"  // 提前1小时提醒
	ReminderTypeDeadline      ReminderType = "DEADLINE"       // 截止时间提醒
)

type Task struct {
	ID             int            `json:"id"`
	Name           string         `json:"name"`
	Description    sql.NullString `json:"description"`
	Type           TaskType       `json:"type"`
	CronExpr       string         `json:"cron_expr"`
	DeadlineTime   sql.NullTime   `json:"deadline_time"`    // 任务截止时间
	AdvanceMinutes int            `json:"advance_minutes"`  // 提前提醒分钟数
	GroupChatID    string         `json:"group_chat_id"`
	GroupChatName  sql.NullString `json:"group_chat_name"`
	CreatorUserID  string         `json:"creator_user_id"`
	CreatorName    sql.NullString `json:"creator_name"`
	Status         TaskStatus     `json:"status"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	LastRunAt      sql.NullTime   `json:"last_run_at"`
	NextRunAt      sql.NullTime   `json:"next_run_at"`
}

type CompletionRecord struct {
	ID          int            `json:"id"`
	TaskID      int            `json:"task_id"`
	UserID      string         `json:"user_id"`
	UserName    sql.NullString `json:"user_name"`
	GroupChatID string         `json:"group_chat_id"`
	CompletedAt time.Time      `json:"completed_at"`
	TaskDate    time.Time      `json:"task_date"`
	IsOnTime    bool           `json:"is_on_time"`
}

type ReminderLog struct {
	ID             int            `json:"id"`
	TaskID         int            `json:"task_id"`
	GroupChatID    string         `json:"group_chat_id"`
	ReminderType   string         `json:"reminder_type"`
	MessageText    sql.NullString `json:"message_text"`
	SentAt         time.Time      `json:"sent_at"`
	MemberCount    int            `json:"member_count"`
	CompletedCount int            `json:"completed_count"`
}

// 任务统计
type TaskStats struct {
	TaskID         int       `json:"task_id"`
	TaskName       string    `json:"task_name"`
	TaskType       TaskType  `json:"task_type"`
	TaskDate       time.Time `json:"task_date"`
	TotalMembers   int       `json:"total_members"`
	CompletedCount int       `json:"completed_count"`
	CompletionRate float64   `json:"completion_rate"`
	CompletedUsers []string  `json:"completed_users"`
	PendingUsers   []string  `json:"pending_users"`
}
