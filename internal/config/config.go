package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	// 钉钉配置
	DingTalk DingTalkConfig
	
	// 数据库配置
	Database DatabaseConfig
	
	// 服务配置
	Server ServerConfig
	
	// 管理员配置
	AdminUsers []string
}

type DingTalkConfig struct {
	AppKey     string
	AppSecret  string
	AgentID    string
	RobotCode  string
}

type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
	SSLMode  string
}

type ServerConfig struct {
	Port     string
	Timezone string
}

func Load() (*Config, error) {
	// 加载 .env 文件（k8s 环境中可能不存在，忽略错误）
	_ = godotenv.Load()
	
	config := &Config{
		DingTalk: DingTalkConfig{
			AppKey:    getEnv("DINGTALK_APP_KEY", ""),
			AppSecret: getEnv("DINGTALK_APP_SECRET", ""),
			AgentID:   getEnv("DINGTALK_AGENT_ID", ""),
			RobotCode: getEnv("DINGTALK_ROBOT_CODE", ""),
		},
		Database: DatabaseConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     getEnv("DB_PORT", "5432"),
			User:     getEnv("DB_USER", "postgres"),
			Password: getEnv("DB_PASSWORD", "postgres"),
			DBName:   getEnv("DB_NAME", "dingteam_bot"),
			SSLMode:  getEnv("DB_SSLMODE", "disable"),
		},
		Server: ServerConfig{
			Port:     getEnv("SERVER_PORT", "8080"),
			Timezone: getEnv("TIMEZONE", "Asia/Shanghai"),
		},
		AdminUsers: parseAdminUsers(getEnv("ADMIN_USERS", "")),
	}
	
	// 验证必需配置
	if config.DingTalk.AppKey == "" || config.DingTalk.AppSecret == "" {
		return nil, fmt.Errorf("缺少钉钉配置：DINGTALK_APP_KEY 和 DINGTALK_APP_SECRET 必须设置")
	}
	
	return config, nil
}

func (c *Config) GetDSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		c.Database.Host,
		c.Database.Port,
		c.Database.User,
		c.Database.Password,
		c.Database.DBName,
		c.Database.SSLMode,
	)
}

func (c *Config) IsAdmin(userID string) bool {
	for _, adminID := range c.AdminUsers {
		if adminID == userID {
			return true
		}
	}
	return false
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func parseAdminUsers(adminStr string) []string {
	if adminStr == "" {
		return []string{}
	}
	users := strings.Split(adminStr, ",")
	result := make([]string, 0, len(users))
	for _, user := range users {
		if trimmed := strings.TrimSpace(user); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
