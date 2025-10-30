.PHONY: help build run docker-build docker-run k8s-deploy clean test

help: ## 显示帮助信息
	@echo "DingTeam Bot - 可用命令："
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

build: ## 编译项目
	@echo "📦 编译项目..."
	go build -o bin/dingteam-bot ./cmd/server

run: ## 运行项目
	@echo "🚀 启动服务..."
	go run cmd/server/main.go

test: ## 运行测试
	@echo "🧪 运行测试..."
	go test -v ./...

docker-build: ## 构建 Docker 镜像
	@echo "🐳 构建 Docker 镜像..."
	docker build -t dingteam-bot:latest .

docker-run: docker-build ## 运行 Docker 容器
	@echo "🐳 启动 Docker 容器..."
	docker run -d \
		--name dingteam-bot \
		--env-file .env \
		-p 8080:8080 \
		dingteam-bot:latest
	@echo "✅ 容器已启动，访问 http://localhost:8080/health 查看状态"

k8s-deploy: ## 部署到 Kubernetes
	@echo "☸️  部署到 Kubernetes..."
	kubectl apply -f deployments/k8s/configmap.yaml
	kubectl apply -f deployments/k8s/secret.yaml
	kubectl apply -f deployments/k8s/postgres.yaml
	kubectl apply -f deployments/k8s/deployment.yaml
	kubectl apply -f deployments/k8s/service.yaml
	@echo "✅ 部署完成！"
	@echo "查看状态: kubectl get pods"
	@echo "查看日志: kubectl logs -f deployment/dingteam-bot"

k8s-delete: ## 从 Kubernetes 删除
	@echo "🗑️  删除 Kubernetes 资源..."
	kubectl delete -f deployments/k8s/service.yaml --ignore-not-found
	kubectl delete -f deployments/k8s/deployment.yaml --ignore-not-found
	kubectl delete -f deployments/k8s/postgres.yaml --ignore-not-found
	kubectl delete -f deployments/k8s/secret.yaml --ignore-not-found
	kubectl delete -f deployments/k8s/configmap.yaml --ignore-not-found

k8s-logs: ## 查看 Kubernetes 日志
	kubectl logs -f deployment/dingteam-bot

k8s-status: ## 查看 Kubernetes 状态
	@echo "📊 Pod 状态："
	@kubectl get pods -l app=dingteam-bot
	@echo ""
	@echo "📊 Service 状态："
	@kubectl get svc dingteam-bot-service

db-init: ## 初始化数据库
	@echo "🗄️  初始化数据库..."
	psql -h localhost -U postgres -d dingteam_bot -f scripts/init.sql

db-reset: ## 重置数据库
	@echo "⚠️  重置数据库..."
	dropdb -h localhost -U postgres dingteam_bot --if-exists
	createdb -h localhost -U postgres dingteam_bot
	psql -h localhost -U postgres -d dingteam_bot -f scripts/init.sql

clean: ## 清理编译文件
	@echo "🧹 清理..."
	rm -rf bin/
	docker rm -f dingteam-bot 2>/dev/null || true

deps: ## 安装依赖
	@echo "📥 安装依赖..."
	go mod download
	go mod tidy

lint: ## 代码检查
	@echo "🔍 代码检查..."
	golangci-lint run ./...

dev: ## 开发模式（自动重载）
	@echo "🔧 开发模式..."
	air

.DEFAULT_GOAL := help
