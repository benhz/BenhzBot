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
		// 任务相关表
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

		// 权限管理相关表
		`CREATE TABLE IF NOT EXISTS users (
			id SERIAL PRIMARY KEY,
			dingtalk_user_id VARCHAR(255) UNIQUE NOT NULL,
			username VARCHAR(255),
			role VARCHAR(50) DEFAULT 'member' NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			CONSTRAINT check_role CHECK (role IN ('super_admin', 'admin', 'member'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_users_dingtalk_id ON users(dingtalk_user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_users_role ON users(role)`,
		`CREATE TABLE IF NOT EXISTS permissions (
			id SERIAL PRIMARY KEY,
			name VARCHAR(100) UNIQUE NOT NULL,
			description TEXT,
			command_pattern VARCHAR(255),
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS role_permissions (
			role VARCHAR(50) NOT NULL,
			permission_name VARCHAR(100) NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (role, permission_name),
			FOREIGN KEY (permission_name) REFERENCES permissions(name) ON DELETE CASCADE,
			CONSTRAINT check_role_permissions CHECK (role IN ('super_admin', 'admin', 'member'))
		)`,
		`CREATE TABLE IF NOT EXISTS permission_audit_logs (
			id SERIAL PRIMARY KEY,
			user_id VARCHAR(255) NOT NULL,
			action VARCHAR(100) NOT NULL,
			resource_type VARCHAR(50),
			resource_id VARCHAR(255),
			result VARCHAR(20) NOT NULL,
			reason TEXT,
			ip_address VARCHAR(45),
			user_agent TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			CONSTRAINT check_result CHECK (result IN ('granted', 'denied'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_user_id ON permission_audit_logs(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON permission_audit_logs(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_result ON permission_audit_logs(result)`,

		// 初始化权限数据
		`INSERT INTO permissions (name, description, command_pattern) VALUES
			('add_admin', '添加子管理员', '添加管理员'),
			('remove_admin', '移除子管理员', '移除管理员'),
			('create_task', '创建任务', '创建任务'),
			('update_task', '更新任务', '更新任务'),
			('delete_task', '删除任务', '删除任务'),
			('list_tasks', '查看任务列表', '任务列表'),
			('complete_task', '打卡完成任务', '已完成'),
			('view_stats', '查看统计', '统计')
		ON CONFLICT (name) DO NOTHING`,

		// 初始化角色权限映射
		`INSERT INTO role_permissions (role, permission_name) VALUES
			('super_admin', 'add_admin'),
			('super_admin', 'remove_admin'),
			('super_admin', 'create_task'),
			('super_admin', 'update_task'),
			('super_admin', 'delete_task'),
			('super_admin', 'list_tasks'),
			('super_admin', 'complete_task'),
			('super_admin', 'view_stats'),
			('admin', 'create_task'),
			('admin', 'update_task'),
			('admin', 'delete_task'),
			('admin', 'list_tasks'),
			('admin', 'complete_task'),
			('admin', 'view_stats'),
			('member', 'complete_task'),
			('member', 'view_stats'),
			('member', 'list_tasks')
		ON CONFLICT DO NOTHING`,

		// 创建触发器：自动更新 updated_at
		`CREATE OR REPLACE FUNCTION update_updated_at_column()
		RETURNS TRIGGER AS $$
		BEGIN
			NEW.updated_at = CURRENT_TIMESTAMP;
			RETURN NEW;
		END;
		$$ language 'plpgsql'`,
		`DO $$
		BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'update_users_updated_at') THEN
				CREATE TRIGGER update_users_updated_at BEFORE UPDATE ON users
					FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
			END IF;
		END $$`,
	}

	for i, migration := range migrations {
		if _, err := db.Exec(migration); err != nil {
			return fmt.Errorf("迁移 #%d 失败: %w", i+1, err)
		}
	}

	log.Println("✓ 数据库迁移完成")
	return nil
}
