package services

import (
	"database/sql"
	"fmt"
	"time"

	"dingteam-bot/internal/models"
)

type StatsService struct {
	db *sql.DB
}

func NewStatsService(db *sql.DB) *StatsService {
	return &StatsService{db: db}
}

// 获取任务今日统计
func (s *StatsService) GetTodayStats(taskID int) (*models.TaskStats, error) {
	today := time.Now().Format("2006-01-02")
	
	// 获取任务信息
	var stats models.TaskStats
	query := `
		SELECT id, name, type
		FROM tasks
		WHERE id = $1
	`
	if err := s.db.QueryRow(query, taskID).Scan(&stats.TaskID, &stats.TaskName, &stats.TaskType); err != nil {
		return nil, err
	}

	stats.TaskDate, _ = time.Parse("2006-01-02", today)

	// 获取今日完成人数
	completedQuery := `
		SELECT COUNT(DISTINCT user_id), 
		       COALESCE(array_agg(DISTINCT user_name), ARRAY[]::varchar[])
		FROM completion_records
		WHERE task_id = $1 AND task_date = $2
	`
	
	var userNames []sql.NullString
	if err := s.db.QueryRow(completedQuery, taskID, today).Scan(&stats.CompletedCount, &userNames); err != nil {
		return nil, err
	}

	// 转换用户名数组
	for _, name := range userNames {
		if name.Valid {
			stats.CompletedUsers = append(stats.CompletedUsers, name.String)
		}
	}

	// 这里假设群成员数需要从其他地方获取，暂时设为固定值
	// 实际场景需要调用钉钉 API 获取群成员列表
	stats.TotalMembers = 10 // TODO: 从钉钉 API 获取实际人数

	if stats.TotalMembers > 0 {
		stats.CompletionRate = float64(stats.CompletedCount) / float64(stats.TotalMembers) * 100
	}

	return &stats, nil
}

// 获取本周统计
func (s *StatsService) GetWeeklyStats(taskID int) ([]*models.TaskStats, error) {
	// 获取本周一到今天的日期范围
	now := time.Now()
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7 // 周日转为 7
	}
	mondayOffset := weekday - 1
	monday := now.AddDate(0, 0, -mondayOffset)
	
	startDate := monday.Format("2006-01-02")
	endDate := now.Format("2006-01-02")

	query := `
		SELECT 
			task_date,
			COUNT(DISTINCT user_id) as completed_count,
			COALESCE(array_agg(DISTINCT user_name), ARRAY[]::varchar[]) as user_names
		FROM completion_records
		WHERE task_id = $1 
		  AND task_date >= $2 
		  AND task_date <= $3
		GROUP BY task_date
		ORDER BY task_date
	`

	rows, err := s.db.Query(query, taskID, startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var statsList []*models.TaskStats
	for rows.Next() {
		var stats models.TaskStats
		var userNames []sql.NullString
		
		if err := rows.Scan(&stats.TaskDate, &stats.CompletedCount, &userNames); err != nil {
			return nil, err
		}

		stats.TaskID = taskID
		stats.TotalMembers = 10 // TODO: 从钉钉 API 获取

		for _, name := range userNames {
			if name.Valid {
				stats.CompletedUsers = append(stats.CompletedUsers, name.String)
			}
		}

		if stats.TotalMembers > 0 {
			stats.CompletionRate = float64(stats.CompletedCount) / float64(stats.TotalMembers) * 100
		}

		statsList = append(statsList, &stats)
	}

	return statsList, nil
}

// 获取未完成名单
func (s *StatsService) GetPendingUsers(taskID int, allUserIDs []string) ([]string, error) {
	today := time.Now().Format("2006-01-02")
	
	// 获取已完成的用户 ID
	query := `
		SELECT user_id
		FROM completion_records
		WHERE task_id = $1 AND task_date = $2
	`
	
	rows, err := s.db.Query(query, taskID, today)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	completedMap := make(map[string]bool)
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			continue
		}
		completedMap[userID] = true
	}

	// 找出未完成的用户
	var pendingUsers []string
	for _, userID := range allUserIDs {
		if !completedMap[userID] {
			pendingUsers = append(pendingUsers, userID)
		}
	}

	return pendingUsers, nil
}

// 格式化统计报告
func (s *StatsService) FormatStatsReport(stats *models.TaskStats) string {
	report := fmt.Sprintf("📊 **%s 统计报告**\n\n", stats.TaskName)
	report += fmt.Sprintf("📅 日期: %s\n", stats.TaskDate.Format("2006-01-02"))
	report += fmt.Sprintf("👥 总人数: %d\n", stats.TotalMembers)
	report += fmt.Sprintf("✅ 已完成: %d 人\n", stats.CompletedCount)
	report += fmt.Sprintf("📈 完成率: %.1f%%\n\n", stats.CompletionRate)

	if len(stats.CompletedUsers) > 0 {
		report += "**已完成成员：**\n"
		for _, user := range stats.CompletedUsers {
			report += fmt.Sprintf("- %s\n", user)
		}
		report += "\n"
	}

	if len(stats.PendingUsers) > 0 {
		report += "**待完成成员：**\n"
		for _, user := range stats.PendingUsers {
			report += fmt.Sprintf("- %s\n", user)
		}
	}

	return report
}
