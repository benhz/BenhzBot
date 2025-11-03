-- ================================================
-- 权限管理系统数据库迁移脚本
-- 版本: 001
-- 描述: 创建用户、权限、角色相关表
-- ================================================

-- 1. 用户表
CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    dingtalk_user_id VARCHAR(255) UNIQUE NOT NULL,
    username VARCHAR(255),
    role VARCHAR(50) DEFAULT 'member' NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT check_role CHECK (role IN ('super_admin', 'admin', 'member'))
);

-- 创建索引
CREATE INDEX idx_users_dingtalk_id ON users(dingtalk_user_id);
CREATE INDEX idx_users_role ON users(role);

-- 2. 权限表
CREATE TABLE IF NOT EXISTS permissions (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) UNIQUE NOT NULL,
    description TEXT,
    command_pattern VARCHAR(255),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 3. 角色权限映射表
CREATE TABLE IF NOT EXISTS role_permissions (
    role VARCHAR(50) NOT NULL,
    permission_name VARCHAR(100) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (role, permission_name),
    FOREIGN KEY (permission_name) REFERENCES permissions(name) ON DELETE CASCADE,
    CONSTRAINT check_role_permissions CHECK (role IN ('super_admin', 'admin', 'member'))
);

-- 4. 权限审计日志表
CREATE TABLE IF NOT EXISTS permission_audit_logs (
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
);

-- 创建索引
CREATE INDEX idx_audit_logs_user_id ON permission_audit_logs(user_id);
CREATE INDEX idx_audit_logs_created_at ON permission_audit_logs(created_at);
CREATE INDEX idx_audit_logs_result ON permission_audit_logs(result);

-- ================================================
-- 初始化权限数据
-- ================================================

-- 插入权限定义
INSERT INTO permissions (name, description, command_pattern) VALUES
    ('add_admin', '添加子管理员', '添加管理员'),
    ('remove_admin', '移除子管理员', '移除管理员'),
    ('create_task', '创建任务', '创建任务'),
    ('update_task', '更新任务', '更新任务'),
    ('delete_task', '删除任务', '删除任务'),
    ('list_tasks', '查看任务列表', '任务列表'),
    ('complete_task', '打卡完成任务', '已完成'),
    ('view_stats', '查看统计', '统计')
ON CONFLICT (name) DO NOTHING;

-- ================================================
-- 初始化角色权限映射
-- ================================================

-- super_admin 拥有所有权限
INSERT INTO role_permissions (role, permission_name) VALUES
    ('super_admin', 'add_admin'),
    ('super_admin', 'remove_admin'),
    ('super_admin', 'create_task'),
    ('super_admin', 'update_task'),
    ('super_admin', 'delete_task'),
    ('super_admin', 'list_tasks'),
    ('super_admin', 'complete_task'),
    ('super_admin', 'view_stats')
ON CONFLICT DO NOTHING;

-- admin 可以管理任务
INSERT INTO role_permissions (role, permission_name) VALUES
    ('admin', 'create_task'),
    ('admin', 'update_task'),
    ('admin', 'delete_task'),
    ('admin', 'list_tasks'),
    ('admin', 'complete_task'),
    ('admin', 'view_stats')
ON CONFLICT DO NOTHING;

-- member 只能打卡和查看
INSERT INTO role_permissions (role, permission_name) VALUES
    ('member', 'complete_task'),
    ('member', 'view_stats'),
    ('member', 'list_tasks')
ON CONFLICT DO NOTHING;

-- ================================================
-- 创建触发器：自动更新 updated_at
-- ================================================

CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER update_users_updated_at BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ================================================
-- 注释
-- ================================================

COMMENT ON TABLE users IS '用户表，存储钉钉用户ID和角色信息';
COMMENT ON TABLE permissions IS '权限定义表';
COMMENT ON TABLE role_permissions IS '角色权限映射表';
COMMENT ON TABLE permission_audit_logs IS '权限操作审计日志';
