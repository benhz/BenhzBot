# 快速启动指南

## 方式一：Docker Compose（推荐用于本地开发）

### 1. 配置环境变量
```bash
cp .env.example .env
# 编辑 .env 文件，填入钉钉凭证
```

### 2. 启动服务
```bash
docker-compose up -d
```

### 3. 查看日志
```bash
docker-compose logs -f dingteam-bot
```

### 4. 停止服务
```bash
docker-compose down
```

---

## 方式二：Makefile（本地开发）

### 1. 安装依赖
```bash
make deps
```

### 2. 启动 PostgreSQL
```bash
docker run -d \
  --name postgres \
  -e POSTGRES_DB=dingteam_bot \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=postgres \
  -p 5432:5432 \
  postgres:15-alpine
```

### 3. 初始化数据库
```bash
make db-init
```

### 4. 运行服务
```bash
make run
```

---

## 方式三：Kubernetes 生产环境

### 1. 配置 Secret
编辑 `deployments/k8s/secret.yaml`，填入实际凭证。

### 2. 部署
```bash
make k8s-deploy
```

### 3. 查看状态
```bash
make k8s-status
```

### 4. 查看日志
```bash
make k8s-logs
```

---

## 验证部署

### 健康检查
```bash
curl http://localhost:8080/health
```

期望返回：
```json
{
  "status": "ok",
  "service": "dingteam-bot"
}
```

### 测试钉钉连接
在钉钉群里 @ 机器人：
```
@机器人 帮助
```

应该收到帮助信息回复。

---

## 常用 Makefile 命令

```bash
make help           # 查看所有命令
make build          # 编译项目
make run            # 运行项目
make test           # 运行测试
make docker-build   # 构建 Docker 镜像
make docker-run     # 运行 Docker 容器
make k8s-deploy     # 部署到 K8s
make k8s-delete     # 从 K8s 删除
make k8s-logs       # 查看 K8s 日志
make clean          # 清理编译文件
```

---

## 创建第一个任务

在钉钉群里：

```
@机器人 创建任务 写周报 "0 17 * * 5" 15:00 TASK
```

这将创建一个每周五 17:00 提醒，15:00 截止的周报任务。

---

## 故障排查

### 1. 数据库连接失败
检查：
- PostgreSQL 是否运行
- 连接参数是否正确
- 防火墙是否允许连接

### 2. 钉钉消息发不出去
检查：
- AppKey/AppSecret 是否正确
- Access Token 是否获取成功
- 机器人是否在群里

### 3. 任务没有触发
检查：
- Cron 表达式是否正确
- 时区设置是否正确
- 调度器日志是否有错误

---

## 开发建议

### 推荐工具
- **Air**: Go 热重载工具
- **golangci-lint**: 代码检查
- **PostgreSQL客户端**: pgAdmin 或 DBeaver

### 日志级别
开发环境建议开启详细日志：
```go
log.SetFlags(log.LstdFlags | log.Lshortfile)
```

---

## 下一步

1. 阅读完整 [README.md](README.md)
2. 查看 API 文档
3. 自定义任务类型
4. 集成 Web 管理后台
