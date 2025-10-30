package main

import (
	"context"
	"dingteam-bot/internal/config"
	"dingteam-bot/internal/database"
	"dingteam-bot/internal/dingtalk"
	"dingteam-bot/internal/handlers"
	"dingteam-bot/internal/scheduler"
	"dingteam-bot/internal/services"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"
)

func main() {
	log.Println("🚀 DingTeam Bot 启动中...")

	// 1. 加载配置
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("❌ 加载配置失败: %v", err)
	}
	log.Println("✓ 配置加载完成")

	// 2. 连接数据库
	db, err := database.NewDB(cfg.GetDSN())
	if err != nil {
		log.Fatalf("❌ 数据库连接失败: %v", err)
	}
	defer db.Close()

	// 3. 运行数据库迁移
	if err := db.RunMigrations(); err != nil {
		log.Fatalf("❌ 数据库迁移失败: %v", err)
	}

	// 4. 初始化服务
	taskService := services.NewTaskService(db.DB)
	statsService := services.NewStatsService(db.DB)

	// 5. 初始化钉钉客户端
	dtClient := dingtalk.NewClient(
		cfg.DingTalk.AppKey,
		cfg.DingTalk.AppSecret,
		cfg.DingTalk.AgentID,
		cfg.DingTalk.RobotCode,
	)

	// 测试连接
	if _, err := dtClient.GetAccessToken(); err != nil {
		log.Fatalf("❌ 钉钉连接失败: %v", err)
	}
	log.Println("✓ 钉钉连接成功")

	// 6. 初始化消息处理器
	messageHandler := handlers.NewMessageHandler(cfg, taskService, statsService, dtClient)

	// 7. 启动调度器
	sched, err := scheduler.NewScheduler(taskService, dtClient, cfg.Server.Timezone)
	if err != nil {
		log.Fatalf("❌ 创建调度器失败: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := sched.Start(ctx); err != nil {
		log.Fatalf("❌ 启动调度器失败: %v", err)
	}
	defer sched.Stop()

	// 8. 启动钉钉 Stream 客户端
	streamClient := dingtalk.NewStreamClient(cfg.DingTalk.AppKey, cfg.DingTalk.AppSecret, messageHandler)
	go func() {
		if err := streamClient.Start(ctx); err != nil {
			log.Fatalf("❌ 启动 Stream 客户端失败: %v", err)
		}
	}()
	defer streamClient.Stop()

	// 9. 启动 HTTP 服务器（健康检查）
	router := setupRouter()
	go func() {
		addr := ":" + cfg.Server.Port
		log.Printf("✓ HTTP 服务器启动在 %s", addr)
		if err := router.Run(addr); err != nil {
			log.Fatalf("❌ HTTP 服务器启动失败: %v", err)
		}
	}()

	// 10. 等待退出信号
	log.Println("✅ DingTeam Bot 运行中...")
	log.Println("按 Ctrl+C 退出")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("\n👋 正在关闭服务...")
	cancel()
	log.Println("✅ 服务已停止")
}

func setupRouter() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())

	// 健康检查
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":  "ok",
			"service": "dingteam-bot",
		})
	})

	// 就绪检查
	router.GET("/ready", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status": "ready",
		})
	})

	return router
}
