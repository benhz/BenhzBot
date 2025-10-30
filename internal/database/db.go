package database

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
)

type DB struct {
	*sql.DB
}

func NewDB(dsn string) (*DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("无法打开数据库连接: %w", err)
	}

	// 设置连接池参数
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// 测试连接
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("无法连接到数据库: %w", err)
	}

	log.Println("✓ 数据库连接成功")
	return &DB{db}, nil
}

// 执行迁移（创建表结构）
func (db *DB) RunMigrations() error {
	migrations := []string{
		`DO $$ 
	BEGIN
		IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'task_type') THEN
			CREATE TYPE task_type AS ENUM ('TASK', 'NOTIFICATION');
		END IF;
	END $$`,
		`DO $$ 
	BEGIN
		IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'task_status') THEN
			CREATE TYPE task_status AS ENUM ('ACTIVE', 'PAUSED', 'DELETED');
		END IF;
	END $$`,
		`CREATE TABLE IF NOT EXISTS tasks (
			id SERIAL PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			description TEXT,
			type task_type NOT NULL DEFAULT 'NOTIFICATION',
			cron_expr VARCHAR(100) NOT NULL,
			deadline_time TIME,
			advance_minutes INT DEFAULT 30,
			group_chat_id VARCHAR(100) NOT NULL,
			group_chat_name VARCHAR(255),
			creator_user_id VARCHAR(100) NOT NULL,
			creator_name VARCHAR(100),
			status task_status NOT NULL DEFAULT 'ACTIVE',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			last_run_at TIMESTAMP,
			next_run_at TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS completion_records (
			id SERIAL PRIMARY KEY,
			task_id INT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			user_id VARCHAR(100) NOT NULL,
			user_name VARCHAR(100),
			group_chat_id VARCHAR(100) NOT NULL,
			completed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			task_date DATE NOT NULL,
			is_on_time BOOLEAN DEFAULT TRUE,
			UNIQUE(task_id, user_id, task_date)
		)`,
		`CREATE TABLE IF NOT EXISTS reminder_logs (
			id SERIAL PRIMARY KEY,
			task_id INT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			group_chat_id VARCHAR(100) NOT NULL,
			reminder_type VARCHAR(50) NOT NULL,
			message_text TEXT,
			sent_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			member_count INT DEFAULT 0,
			completed_count INT DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_group_chat ON tasks(group_chat_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_completion_task_date ON completion_records(task_id, task_date)`,
	}

	for i, migration := range migrations {
		if _, err := db.Exec(migration); err != nil {
			return fmt.Errorf("迁移 #%d 失败: %w", i+1, err)
		}
	}

	log.Println("✓ 数据库迁移完成")
	return nil
}
