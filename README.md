# DingTeam Bot MVP

基于 Golang + PostgreSQL + 钉钉 Stream 的团队任务提醒与打卡机器人。

## 功能特性

### 核心功能
- ✅ 定时任务提醒（支持 Cron 表达式）
- ✅ 群内打卡记录
- ✅ 任务统计与报告
- ✅ 管理员权限管理
- ✅ 支持两种任务类型：
  - **任务型 (TASK)**: 设定截止时间，过期未完成则通报
  - **通知型 (NOTIFICATION)**: 提前 N 分钟提醒

### 技术栈
- **后端**: Go 1.21+
- **数据库**: PostgreSQL 15+
- **消息**: 钉钉 Stream SDK
- **调度**: robfig/cron/v3
- **部署**: Docker + Kubernetes

## 快速开始

### 1. 准备工作

#### 1.1 钉钉机器人配置
1. 登录钉钉开放平台：https://open.dingtalk.com
2. 创建企业内部应用
3. 开启机器人能力
4. 订阅群消息事件
5. 获取以下凭证：
   - AppKey
   - AppSecret
   - AgentID
   - RobotCode

#### 1.2 克隆项目
```bash
git clone <repository>
cd dingteam-bot
```

### 2. 本地开发

#### 2.1 配置环境变量
```bash
cp .env.example .env
# 编辑 .env 文件，填入钉钉凭证和数据库配置
```

#### 2.2 启动 PostgreSQL
```bash
docker run -d \
  --name postgres \
  -e POSTGRES_DB=dingteam_bot \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=postgres \
  -p 5432:5432 \
  postgres:15-alpine
```

#### 2.3 初始化数据库
```bash
psql -h localhost -U postgres -d dingteam_bot -f scripts/init.sql
```

#### 2.4 安装依赖
```bash
go mod download
```

#### 2.5 运行服务
```bash
go run cmd/server/main.go
```

### 3. Docker 部署

#### 3.1 构建镜像
```bash
docker build -t dingteam-bot:latest .
```

#### 3.2 运行容器
```bash
docker run -d \
  --name dingteam-bot \
  --env-file .env \
  -p 8080:8080 \
  dingteam-bot:latest
```

### 4. Kubernetes 部署

#### 4.1 更新配置
编辑 `deployments/k8s/secret.yaml`，填入实际的钉钉凭证：
```yaml
stringData:
  DINGTALK_APP_KEY: "your_app_key"
  DINGTALK_APP_SECRET: "your_app_secret"
  DINGTALK_AGENT_ID: "your_agent_id"
  DINGTALK_ROBOT_CODE: "your_robot_code"
  DB_PASSWORD: "your_secure_password"
  ADMIN_USERS: "user1,user2"
```

#### 4.2 部署到 K8s
```bash
# 部署 PostgreSQL（可选，生产环境建议使用外部数据库）
kubectl apply -f deployments/k8s/postgres.yaml

# 部署应用
kubectl apply -f deployments/k8s/configmap.yaml
kubectl apply -f deployments/k8s/secret.yaml
kubectl apply -f deployments/k8s/deployment.yaml
kubectl apply -f deployments/k8s/service.yaml
```

#### 4.3 检查部署状态
```bash
# 查看 Pod 状态
kubectl get pods

# 查看日志
kubectl logs -f deployment/dingteam-bot

# 健康检查
kubectl port-forward svc/dingteam-bot-service 8080:8080
curl http://localhost:8080/health
```

## 使用指南

### 基本命令

在钉钉群里 @ 机器人使用以下命令：

#### 打卡完成
```
@机器人 已完成
@机器人 我已提交
```

#### 查看统计
```
@机器人 统计
@机器人 本周报告
```

#### 任务列表
```
@机器人 任务列表
@机器人 查看任务
```

#### 帮助
```
@机器人 帮助
@机器人 ?
```

### 管理员命令

#### 创建任务
```
@机器人 创建任务 <名称> <cron表达式> [截止时间] [类型]

示例：
# 每周五17:00提醒写周报，15:00截止（任务型）
@机器人 创建任务 写周报 "0 17 * * 5" 15:00 TASK

# 每天9:30提前30分钟提醒开会（通知型）
@机器人 创建任务 早会提醒 "0 9 * * 1-5" "" NOTIFICATION
```

### Cron 表达式示例

```
0 9 * * 1-5      # 工作日上午9点
0 17 * * 5       # 每周五下午5点
0 0 * * *        # 每天午夜
30 14 * * *      # 每天下午2:30
0 10 1 * *       # 每月1号上午10点
*/30 * * * *     # 每30分钟
```

## 项目结构

```
dingteam-bot/
├── cmd/
│   └── server/           # 主程序入口
├── internal/
│   ├── config/           # 配置管理
│   ├── database/         # 数据库连接
│   ├── models/           # 数据模型
│   ├── handlers/         # 消息处理器
│   ├── services/         # 业务逻辑
│   ├── dingtalk/         # 钉钉客户端
│   └── scheduler/        # 任务调度器
├── deployments/
│   └── k8s/              # Kubernetes 配置
├── scripts/
│   └── init.sql          # 数据库初始化脚本
├── Dockerfile
├── go.mod
└── README.md
```

## 数据库设计

### 核心表

#### tasks - 任务表
- 存储定时任务配置
- 支持 TASK 和 NOTIFICATION 两种类型
- 记录 Cron 表达式、截止时间等

#### completion_records - 完成记录表
- 记录成员打卡信息
- 支持按日期统计
- 标记是否按时完成

#### reminder_logs - 提醒日志表
- 记录每次提醒的发送情况
- 统计完成人数和总人数

## 配置说明

### 环境变量

| 变量名 | 说明 | 默认值 |
|--------|------|--------|
| DINGTALK_APP_KEY | 钉钉应用 Key | - |
| DINGTALK_APP_SECRET | 钉钉应用 Secret | - |
| DINGTALK_AGENT_ID | 钉钉 Agent ID | - |
| DINGTALK_ROBOT_CODE | 钉钉机器人 Code | - |
| DB_HOST | 数据库地址 | localhost |
| DB_PORT | 数据库端口 | 5432 |
| DB_USER | 数据库用户名 | postgres |
| DB_PASSWORD | 数据库密码 | - |
| DB_NAME | 数据库名 | dingteam_bot |
| SERVER_PORT | HTTP 服务端口 | 8080 |
| TIMEZONE | 时区 | Asia/Shanghai |
| ADMIN_USERS | 管理员 ID（逗号分隔） | - |

## 监控与维护

### 健康检查
```bash
# 存活检查
curl http://localhost:8080/health

# 就绪检查
curl http://localhost:8080/ready
```

### 日志查看
```bash
# Docker
docker logs -f dingteam-bot

# Kubernetes
kubectl logs -f deployment/dingteam-bot
```

### 数据库备份
```bash
# 备份
pg_dump -h localhost -U postgres dingteam_bot > backup.sql

# 恢复
psql -h localhost -U postgres dingteam_bot < backup.sql
```

## 常见问题

### Q: 任务没有按时触发？
A: 检查以下几点：
1. Cron 表达式是否正确
2. 时区配置是否正确
3. 调度器是否正常运行
4. 查看日志确认执行情况

### Q: 收不到消息？
A: 检查：
1. 钉钉 Stream 连接是否正常
2. 机器人是否在群里
3. 是否正确订阅了群消息事件
4. Access Token 是否过期

### Q: 打卡失败？
A: 确认：
1. 群里是否有活跃任务
2. 今天是否已经打过卡
3. 数据库连接是否正常

## 开发路线图

### MVP 阶段 ✅
- [x] 基础任务管理
- [x] 打卡记录
- [x] 统计报告
- [x] K8s 部署支持

### 下一阶段
- [ ] ActionCard 交互式卡片
- [ ] 群成员管理（从钉钉 API 获取）
- [ ] 多任务打卡选择
- [ ] 个人统计查询
- [ ] 周报/月报自动生成
- [ ] Web 管理后台
- [ ] 数据可视化
- [ ] 提醒规则更灵活配置

## 贡献指南

欢迎提交 Issue 和 Pull Request！

## 许可证

MIT License

## 联系方式

- GitHub: [Your GitHub]
- Email: [Your Email]
