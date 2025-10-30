-- 创建数据库（如果不存在）
-- CREATE DATABASE dingteam_bot;

-- 任务类型枚举
-- 'TASK': 任务类型，过点未完成则通报
-- 'NOTIFICATION': 通知类型，提前半小时提醒
CREATE TYPE task_type AS ENUM ('TASK', 'NOTIFICATION');

-- 任务状态枚举
CREATE TYPE task_status AS ENUM ('ACTIVE', 'PAUSED', 'DELETED');

-- 定时任务表
CREATE TABLE IF NOT EXISTS tasks (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    type task_type NOT NULL DEFAULT 'NOTIFICATION',
    cron_expr VARCHAR(100) NOT NULL,
    deadline_time TIME,  -- 任务截止时间（仅 TASK 类型）
    advance_minutes INT DEFAULT 30,  -- 提前提醒时间（仅 NOTIFICATION 类型）
    group_chat_id VARCHAR(100) NOT NULL,
    group_chat_name VARCHAR(255),
    creator_user_id VARCHAR(100) NOT NULL,
    creator_name VARCHAR(100),
    status task_status NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_run_at TIMESTAMP,
    next_run_at TIMESTAMP
);

-- 打卡记录表
CREATE TABLE IF NOT EXISTS completion_records (
    id SERIAL PRIMARY KEY,
    task_id INT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    user_id VARCHAR(100) NOT NULL,
    user_name VARCHAR(100),
    group_chat_id VARCHAR(100) NOT NULL,
    completed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    task_date DATE NOT NULL,  -- 任务所属日期（用于统计）
    is_on_time BOOLEAN DEFAULT TRUE,  -- 是否按时完成
    UNIQUE(task_id, user_id, task_date)
);

-- 提醒记录表（用于追踪每次提醒）
CREATE TABLE IF NOT EXISTS reminder_logs (
    id SERIAL PRIMARY KEY,
    task_id INT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    group_chat_id VARCHAR(100) NOT NULL,
    reminder_type VARCHAR(50) NOT NULL,  -- 'NORMAL', 'OVERDUE', 'ADVANCE'
    message_text TEXT,
    sent_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    member_count INT DEFAULT 0,
    completed_count INT DEFAULT 0
);

-- 索引优化
CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_group_chat ON tasks(group_chat_id, status);
CREATE INDEX idx_tasks_next_run ON tasks(next_run_at) WHERE status = 'ACTIVE';
CREATE INDEX idx_completion_task_date ON completion_records(task_id, task_date);
CREATE INDEX idx_completion_user ON completion_records(user_id, task_date);
CREATE INDEX idx_reminder_logs_task ON reminder_logs(task_id, sent_at);

-- 更新 updated_at 触发器
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER update_tasks_updated_at BEFORE UPDATE ON tasks
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- 插入示例数据（可选，用于测试）
-- INSERT INTO tasks (name, description, type, cron_expr, deadline_time, group_chat_id, creator_user_id)
-- VALUES 
--     ('写周报', '每周五提醒写周报', 'TASK', '0 17 * * 5', '15:00', 'chat123', 'user123'),
--     ('例会提醒', '每周一早会提醒', 'NOTIFICATION', '0 9 * * 1', NULL, 'chat123', 'user123');
