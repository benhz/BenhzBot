# DingTeam Bot MVP - 后端开发任务说明

## 一、项目概述

### 1.1 项目目标
构建一个基于钉钉的智能提醒机器人，实现团队任务提醒、打卡记录、统计查询的完整闭环。

### 1.2 核心价值
- **自动化提醒**：定时发送周报、会议等任务提醒
- **便捷打卡**：支持按钮点击或消息回复两种打卡方式
- **智能统计**：实时查看团队完成情况
- **灵活配置**：管理员可动态创建/修改提醒任务

### 1.3 技术栈
- **后端语言**：Go 1.21+
- **Web框架**：Gin
- **数据库**：PostgreSQL 15+
- **钉钉接入**：Stream API（事件订阅）
- **定时任务**：robfig/cron/v3
- **配置管理**：godotenv

---

## 二、功能需求详解

### 2.1 核心功能模块

#### 模块1：定时提醒系统
**功能描述**：
- 支持按周期（每周X、每月X日）或一次性（明天、某日期）创建提醒
- 区分"任务"和"通知"两种类型

**任务 vs 通知**：
```
【任务类型】
- 有明确的截止时间（deadline）
- 过期未完成会自动通报
- 例：15:00 前完成周报，超时则@未完成人员

【通知类型】
- 仅提醒，无强制要求
- 提前半小时通知即可
- 例：明天10:00开会（9:30提醒）
```

**支持的指令格式**：
```
@机器人 每周五 17:00 提醒写周报
@机器人 每周五 15:00 任务:提交周报
@机器人 明天 10:00 通知:开例会
@机器人 12月1日 14:00 任务:提交月报
```

#### 模块2：打卡记录系统
**触发方式**：
1. **ActionCard按钮**：点击"我已提交"按钮
2. **文本消息**：发送"@机器人 我已提交"

**记录内容**：
- 用户ID、用户名
- 任务ID
- 提交时间
- 是否超时

#### 模块3：统计查询系统
**支持的查询指令**：
```
@机器人 本周周报统计
@机器人 今日任务统计
@机器人 任务列表
```

**返回信息**：
- 已提交人员名单（含提交时间）
- 未提交人员名单
- 完成率（x/y，百分比）
- 超时提交人员（标红提示）

#### 模块4：管理功能
**权限控制**：
- 仅群主/管理员可创建/修改/删除任务
- 所有成员可打卡和查询

**管理指令**：
```
@机器人 删除任务 [任务名称]
@机器人 暂停任务 [任务名称]
@机器人 恢复任务 [任务名称]
@机器人 修改任务 [任务名称] 新时间 17:00
```

---

## 三、数据库设计

### 3.1 表结构

#### 3.1.1 groups（群组表）
```sql
CREATE TABLE groups (
    id BIGSERIAL PRIMARY KEY,
    group_id VARCHAR(64) UNIQUE NOT NULL,  -- 钉钉群ID
    group_name VARCHAR(255) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_groups_group_id ON groups(group_id);
```

#### 3.1.2 tasks（任务表）
```sql
CREATE TABLE tasks (
    id BIGSERIAL PRIMARY KEY,
    group_id VARCHAR(64) NOT NULL,         -- 关联群组
    task_name VARCHAR(255) NOT NULL,       -- 任务名称（如"写周报"）
    task_type VARCHAR(20) NOT NULL,        -- 'task' 或 'notice'
    cron_expr VARCHAR(100),                -- cron表达式（周期任务）
    one_time_date TIMESTAMP,               -- 一次性任务时间
    is_recurring BOOLEAN DEFAULT false,    -- 是否周期任务
    deadline_offset INTEGER DEFAULT 0,     -- 任务截止时间偏移（分钟，仅task类型）
    notice_offset INTEGER DEFAULT 30,      -- 通知提前时间（分钟，仅notice类型）
    status VARCHAR(20) DEFAULT 'active',   -- active/paused/deleted
    created_by VARCHAR(64) NOT NULL,       -- 创建者钉钉ID
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_tasks_group_id ON tasks(group_id);
CREATE INDEX idx_tasks_status ON tasks(status);

COMMENT ON COLUMN tasks.deadline_offset IS '任务类型：相对发送时间的截止时长（分钟），如15:00发送，offset=0表示15:00截止';
COMMENT ON COLUMN tasks.notice_offset IS '通知类型：提前通知时长（分钟），如10:00会议，offset=30表示9:30提醒';
```

#### 3.1.3 task_executions（任务执行记录）
```sql
CREATE TABLE task_executions (
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL REFERENCES tasks(id),
    group_id VARCHAR(64) NOT NULL,
    execution_time TIMESTAMP NOT NULL,     -- 本次执行时间
    deadline_time TIMESTAMP,               -- 截止时间（task类型）
    message_id VARCHAR(128),               -- 钉钉消息ID
    status VARCHAR(20) DEFAULT 'pending',  -- pending/completed/overdue
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_executions_task_id ON task_executions(task_id);
CREATE INDEX idx_executions_status ON task_executions(status);
CREATE INDEX idx_executions_deadline ON task_executions(deadline_time);
```

#### 3.1.4 submissions（提交记录）
```sql
CREATE TABLE submissions (
    id BIGSERIAL PRIMARY KEY,
    execution_id BIGINT NOT NULL REFERENCES task_executions(id),
    user_id VARCHAR(64) NOT NULL,          -- 钉钉用户ID
    user_name VARCHAR(255) NOT NULL,
    submit_time TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    is_late BOOLEAN DEFAULT false,         -- 是否超时
    submit_method VARCHAR(20),             -- 'button' 或 'message'
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_submissions_execution_id ON submissions(execution_id);
CREATE INDEX idx_submissions_user_id ON submissions(user_id);
CREATE UNIQUE INDEX idx_submissions_unique ON submissions(execution_id, user_id);
```

#### 3.1.5 group_members（群成员缓存表）
```sql
CREATE TABLE group_members (
    id BIGSERIAL PRIMARY KEY,
    group_id VARCHAR(64) NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    user_name VARCHAR(255) NOT NULL,
    is_admin BOOLEAN DEFAULT false,        -- 是否管理员
    joined_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_active TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(group_id, user_id)
);

CREATE INDEX idx_members_group_id ON group_members(group_id);
CREATE INDEX idx_members_user_id ON group_members(user_id);
```

---

## 四、系统架构设计

### 4.1 目录结构
```
dingteam-bot/
├── cmd/
│   └── server/
│       └── main.go              # 入口文件
├── internal/
│   ├── config/
│   │   └── config.go            # 配置管理
│   ├── database/
│   │   ├── db.go                # 数据库连接
│   │   └── migrations/          # 数据库迁移脚本
│   ├── dingtalk/
│   │   ├── client.go            # 钉钉API客户端
│   │   ├── stream.go            # Stream事件处理
│   │   └── message.go           # 消息发送封装
│   ├── handler/
│   │   ├── command.go           # 命令解析器
│   │   ├── task.go              # 任务管理
│   │   ├── submission.go        # 打卡处理
│   │   └── stats.go             # 统计查询
│   ├── scheduler/
│   │   ├── cron.go              # 定时任务调度
│   │   └── executor.go          # 任务执行器
│   ├── model/
│   │   └── models.go            # 数据模型
│   └── service/
│       ├── task_service.go      # 任务服务
│       ├── submission_service.go # 提交服务
│       └── stats_service.go     # 统计服务
├── pkg/
│   ├── utils/
│   │   ├── time.go              # 时间解析工具
│   │   └── parser.go            # 命令解析工具
│   └── logger/
│       └── logger.go            # 日志组件
├── .env.example
├── go.mod
├── go.sum
└── README.md
```

### 4.2 核心组件说明

#### 4.2.1 Stream事件监听器
```go
// internal/dingtalk/stream.go
type StreamHandler struct {
    client    *StreamClient
    cmdParser *CommandParser
    taskSvc   *TaskService
    subSvc    *SubmissionService
}

// 监听群消息事件
func (h *StreamHandler) HandleMessage(msg *GroupMessage) error {
    // 1. 检查是否@机器人
    // 2. 提取命令内容
    // 3. 路由到对应处理器
}

// 监听ActionCard回调事件
func (h *StreamHandler) HandleCallback(callback *CardCallback) error {
    // 处理按钮点击事件
}
```

#### 4.2.2 命令解析器
```go
// internal/handler/command.go
type Command struct {
    Type    string   // create_task/submit/query/delete/pause/resume
    Params  map[string]interface{}
}

func ParseCommand(text string) (*Command, error) {
    // 使用正则表达式解析各类指令
    // 支持自然语言解析：明天/下周五/12月1日
}
```

#### 4.2.3 定时任务调度器
```go
// internal/scheduler/cron.go
type Scheduler struct {
    cron      *cron.Cron
    taskSvc   *TaskService
    executor  *TaskExecutor
}

func (s *Scheduler) Start() {
    // 1. 从数据库加载所有active任务
    // 2. 注册到cron
    // 3. 监听任务变更（新增/修改/删除）
}

func (s *Scheduler) AddTask(task *Task) error {
    // 动态添加任务到cron
}
```

#### 4.2.4 任务执行器
```go
// internal/scheduler/executor.go
type TaskExecutor struct {
    dingClient *DingTalkClient
    db         *gorm.DB
}

func (e *TaskExecutor) Execute(task *Task) error {
    // 1. 创建execution记录
    // 2. 发送钉钉消息（ActionCard或普通消息）
    // 3. 如果是任务类型，创建超时检查定时器
}

func (e *TaskExecutor) CheckOverdue(executionID int64) {
    // 检查超时未提交的人员，发送通报
}
```

---

## 五、核心流程设计

### 5.1 任务创建流程
```
1. 用户发送：@机器人 每周五 17:00 任务:提交周报
                ↓
2. StreamHandler 接收消息
                ↓
3. CommandParser 解析指令
   - 提取：type=task, cron="0 17 * * 5", name="提交周报"
                ↓
4. 权限校验（是否管理员）
                ↓
5. TaskService 创建任务记录
                ↓
6. Scheduler 注册到cron调度器
                ↓
7. 回复用户：✅ 已创建任务「提交周报」，每周五17:00执行
```

### 5.2 定时提醒流程
```
1. Cron 触发任务（周五 17:00）
                ↓
2. TaskExecutor 执行任务
   - 创建 task_execution 记录
   - 计算 deadline_time = 17:00（task类型）
                ↓
3. 发送钉钉消息到群
   - Task类型：带ActionCard（"我已提交"按钮）+ 截止时间提示
   - Notice类型：纯文本消息
                ↓
4. 如果是Task类型，创建超时检查任务
   - 在deadline_time触发 CheckOverdue
```

### 5.3 打卡提交流程
```
【方式1：点击按钮】
1. 用户点击"我已提交"
                ↓
2. StreamHandler 接收callback事件
                ↓
3. SubmissionService 记录提交
   - 检查是否已提交（去重）
   - 判断是否超时（submit_time > deadline_time）
   - 写入 submissions 表
                ↓
4. 回复用户私聊：✅ 已记录提交（或⚠️ 超时提交）

【方式2：文本消息】
1. 用户发送：@机器人 我已提交
                ↓
2. CommandParser 识别为submit命令
                ↓
3. 查找当前活跃的execution（最近一次未完成）
                ↓
4. 同上记录提交
```

### 5.4 统计查询流程
```
1. 用户发送：@机器人 本周周报统计
                ↓
2. StatsService 查询数据
   - 查找本周的周报任务execution
   - 统计已提交/未提交人员
   - 计算完成率
                ↓
3. 格式化消息
   ================
   📊 本周周报统计
   ================
   ✅ 已提交（5人）
   张三 - 周五 17:05
   李四 - 周五 16:58
   ...
   
   ❌ 未提交（2人）
   @王五 @赵六
   
   完成率：71% (5/7)
   ================
                ↓
4. 发送到群
```

### 5.5 超时通报流程
```
1. 到达deadline_time，触发CheckOverdue
                ↓
2. 查询该execution下的提交情况
                ↓
3. 找出未提交人员列表
                ↓
4. 发送群消息通报
   ⚠️ 以下人员未按时提交周报：
   @王五 @赵六
   
   请尽快补交！
                ↓
5. 更新execution状态为overdue
```

---

## 六、钉钉接入实现

### 6.1 Stream模式接入
```go
// 使用钉钉官方SDK：github.com/open-dingtalk/dingtalk-stream-sdk-go

import (
    "github.com/open-dingtalk/dingtalk-stream-sdk-go/client"
    "github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"
)

type DingTalkClient struct {
    client    *client.StreamClient
    chatbot   *chatbot.ChatbotHandler
    appKey    string
    appSecret string
}

func NewDingTalkClient(appKey, appSecret string) *DingTalkClient {
    cli := client.NewStreamClient(
        client.WithAppCredential(client.NewAppCredentialConfig(appKey, appSecret)),
    )
    
    return &DingTalkClient{
        client:    cli,
        appKey:    appKey,
        appSecret: appSecret,
    }
}

func (c *DingTalkClient) Start() error {
    // 注册消息回调
    c.chatbot = chatbot.NewChatbotHandler(c.HandleMessage)
    c.client.RegisterChatbotHandler(c.chatbot)
    
    return c.client.Start(context.Background())
}
```

### 6.2 发送ActionCard消息
```go
func (c *DingTalkClient) SendActionCard(groupID, title, text string, btns []Button) error {
    msg := &ActionCardMessage{
        MsgType: "actionCard",
        ActionCard: ActionCard{
            Title:          title,
            Text:           text,
            SingleTitle:    "查看详情",
            SingleURL:      "dingtalk://...",
            BtnOrientation: "0",
            Btns:           btns,
        },
    }
    
    return c.sendGroupMessage(groupID, msg)
}

// 按钮示例
type Button struct {
    Title     string `json:"title"`
    ActionURL string `json:"actionURL"` // dingtalk://dingtalkclient/action/openapp?...
}
```

### 6.3 消息类型设计
```go
// 任务提醒消息（带按钮）
func BuildTaskReminderCard(taskName, deadline string) *ActionCardMessage {
    return &ActionCardMessage{
        Title: fmt.Sprintf("📝 提醒：%s", taskName),
        Text: fmt.Sprintf(
            "### 请完成任务：%s\n\n" +
            "⏰ 截止时间：%s\n\n" +
            "点击下方按钮提交，或回复 `@机器人 我已提交`",
            taskName, deadline,
        ),
        Btns: []Button{
            {Title: "✅ 我已提交", ActionURL: "..."},
        },
    }
}

// 通知消息（纯文本）
func BuildNoticeMessage(content, time string) string {
    return fmt.Sprintf("📢 通知提醒\n\n%s\n\n时间：%s", content, time)
}
```

---

## 七、关键代码实现

### 7.1 时间解析器
```go
// pkg/utils/time.go
package utils

import (
    "regexp"
    "time"
)

// 解析自然语言时间
func ParseNaturalTime(input string, baseTime time.Time) (time.Time, error) {
    // 明天
    if matched, _ := regexp.MatchString(`明天`, input); matched {
        return baseTime.AddDate(0, 0, 1), nil
    }
    
    // 下周五
    if matched, _ := regexp.MatchString(`下?周[一二三四五六日]`, input); matched {
        // 解析星期
        weekday := parseWeekday(input)
        return nextWeekday(baseTime, weekday), nil
    }
    
    // 12月1日
    dateRegex := regexp.MustCompile(`(\d{1,2})月(\d{1,2})日`)
    if matches := dateRegex.FindStringSubmatch(input); len(matches) == 3 {
        month, _ := strconv.Atoi(matches[1])
        day, _ := strconv.Atoi(matches[2])
        year := baseTime.Year()
        
        target := time.Date(year, time.Month(month), day, 0, 0, 0, 0, baseTime.Location())
        if target.Before(baseTime) {
            target = target.AddDate(1, 0, 0)
        }
        return target, nil
    }
    
    return time.Time{}, fmt.Errorf("无法解析时间")
}

// 转换为Cron表达式
func TimeToCron(t time.Time, recurring bool) string {
    if !recurring {
        return "" // 一次性任务不需要cron
    }
    
    // 每周X -> cron表达式
    // 例：每周五17:00 -> "0 17 * * 5"
    minute := t.Minute()
    hour := t.Hour()
    weekday := t.Weekday()
    
    return fmt.Sprintf("%d %d * * %d", minute, hour, weekday)
}
```

### 7.2 命令解析器
```go
// internal/handler/command.go
type CommandParser struct{}

func (p *CommandParser) Parse(text string) (*Command, error) {
    text = strings.TrimSpace(text)
    
    // 创建任务：每周五 17:00 任务:提交周报
    createRegex := regexp.MustCompile(`(每周[一二三四五六日]|明天|[\d]+月[\d]+日)\s+(\d{1,2}):(\d{2})\s+(任务|通知)[::](.+)`)
    if matches := createRegex.FindStringSubmatch(text); len(matches) == 6 {
        return &Command{
            Type: "create_task",
            Params: map[string]interface{}{
                "time_expr":  matches[1],
                "hour":       matches[2],
                "minute":     matches[3],
                "task_type":  matches[4], // "任务" or "通知"
                "task_name":  matches[5],
            },
        }, nil
    }
    
    // 提交打卡：我已提交
    if matched, _ := regexp.MatchString(`我已提交|已提交|打卡`, text); matched {
        return &Command{Type: "submit"}, nil
    }
    
    // 统计查询：本周周报统计 / 今日任务统计
    statsRegex := regexp.MustCompile(`(本周|今日)(.*)统计`)
    if matches := statsRegex.FindStringSubmatch(text); len(matches) == 3 {
        return &Command{
            Type: "query_stats",
            Params: map[string]interface{}{
                "period": matches[1],
                "task":   matches[2],
            },
        }, nil
    }
    
    // 任务列表
    if matched, _ := regexp.MatchString(`任务列表|查看任务`, text); matched {
        return &Command{Type: "list_tasks"}, nil
    }
    
    // 删除任务：删除任务 写周报
    deleteRegex := regexp.MustCompile(`删除任务\s+(.+)`)
    if matches := deleteRegex.FindStringSubmatch(text); len(matches) == 2 {
        return &Command{
            Type: "delete_task",
            Params: map[string]interface{}{
                "task_name": matches[1],
            },
        }, nil
    }
    
    return nil, fmt.Errorf("无法识别的指令")
}
```

### 7.3 任务服务
```go
// internal/service/task_service.go
type TaskService struct {
    db *gorm.DB
}

func (s *TaskService) CreateTask(req *CreateTaskRequest) (*Task, error) {
    // 1. 解析时间
    baseTime := time.Now()
    targetTime, err := utils.ParseNaturalTime(req.TimeExpr, baseTime)
    if err != nil {
        return nil, err
    }
    
    // 2. 设置时分
    targetTime = time.Date(
        targetTime.Year(), targetTime.Month(), targetTime.Day(),
        req.Hour, req.Minute, 0, 0, targetTime.Location(),
    )
    
    // 3. 判断是否周期任务
    isRecurring := strings.HasPrefix(req.TimeExpr, "每周")
    
    // 4. 生成cron表达式或一次性时间
    var cronExpr string
    var oneTimeDate *time.Time
    
    if isRecurring {
        cronExpr = utils.TimeToCron(targetTime, true)
    } else {
        oneTimeDate = &targetTime
    }
    
    // 5. 计算偏移量
    var deadlineOffset, noticeOffset int
    if req.TaskType == "任务" {
        deadlineOffset = 0 // 发送时间即截止时间
    } else {
        noticeOffset = 30 // 提前30分钟通知
    }
    
    // 6. 创建任务记录
    task := &Task{
        GroupID:        req.GroupID,
        TaskName:       req.TaskName,
        TaskType:       map[string]string{"任务": "task", "通知": "notice"}[req.TaskType],
        CronExpr:       cronExpr,
        OneTimeDate:    oneTimeDate,
        IsRecurring:    isRecurring,
        DeadlineOffset: deadlineOffset,
        NoticeOffset:   noticeOffset,
        Status:         "active",
        CreatedBy:      req.CreatorID,
    }
    
    if err := s.db.Create(task).Error; err != nil {
        return nil, err
    }
    
    return task, nil
}

func (s *TaskService) GetActiveTasks(groupID string) ([]*Task, error) {
    var tasks []*Task
    err := s.db.Where("group_id = ? AND status = ?", groupID, "active").Find(&tasks).Error
    return tasks, err
}
```

### 7.4 提交服务
```go
// internal/service/submission_service.go
type SubmissionService struct {
    db *gorm.DB
}

func (s *SubmissionService) Submit(userID, userName string, executionID int64) error {
    // 1. 检查是否已提交
    var count int64
    s.db.Model(&Submission{}).Where(
        "execution_id = ? AND user_id = ?",
        executionID, userID,
    ).Count(&count)
    
    if count > 0 {
        return fmt.Errorf("您已提交过了")
    }
    
    // 2. 查询execution信息（获取deadline）
    var execution TaskExecution
    if err := s.db.First(&execution, executionID).Error; err != nil {
        return err
    }
    
    // 3. 判断是否超时
    now := time.Now()
    isLate := false
    if execution.DeadlineTime != nil && now.After(*execution.DeadlineTime) {
        isLate = true
    }
    
    // 4. 创建提交记录
    submission := &Submission{
        ExecutionID:  executionID,
        UserID:       userID,
        UserName:     userName,
        SubmitTime:   now,
        IsLate:       isLate,
        SubmitMethod: "message", // or "button"
    }
    
    return s.db.Create(submission).Error
}
```

---

## 八、开发步骤（MVP版本）

### Phase 1：基础框架搭建（1-2天）
1. **初始化项目**
   ```bash
   mkdir dingteam-bot && cd dingteam-bot
   go mod init github.com/yourname/dingteam-bot
   ```

2. **安装依赖**
   ```bash
   go get github.com/gin-gonic/gin
   go get gorm.io/gorm
   go get gorm.io/driver/postgres
   go get github.com/robfig/cron/v3
   go get github.com/joho/godotenv
   go get github.com/open-dingtalk/dingtalk-stream-sdk-go
   ```

3. **配置管理**
   - 创建 `.env` 文件
   - 实现 `config.go` 读取配置

4. **数据库连接**
   - 实现 `database/db.go`
   - 编写数据库迁移脚本
   - 执行建表

### Phase 2：钉钉接入（2-3天）
1. **Stream客户端封装**
   - 实现 `dingtalk/client.go`
   - 实现 `dingtalk/stream.go`

2. **消息处理**
   - 监听群消息事件
   - 监听ActionCard回调事件
   - 测试消息收发

3. **消息发送**
   - 实现文本消息发送
   - 实现ActionCard消息发送

### Phase 3：核心功能实现（3-4天）
1. **命令解析**
   - 实现 `handler/command.go`
   - 实现 `pkg/utils/time.go`
   - 单元测试

2. **任务管理**
   - 实现 `service/task_service.go`
   - 实现任务创建/查询/删除/暂停/恢复

3. **提交打卡**
   - 实现 `service/submission_service.go`
   - 处理按钮点击和文本提交

4. **统计查询**
   - 实现 `service/stats_service.go`
   - 格式化统计消息

### Phase 4：定时任务系统（2-3天）
1. **调度器实现**
   - 实现 `scheduler/cron.go`
   - 加载数据库任务到cron
   - 动态添加/删除任务

2. **执行器实现**
   - 实现 `scheduler/executor.go`
   - 执行任务发送消息
   - 创建execution记录

3. **超时检查**
   - 实现deadline检查逻辑
   - 发送超时通报消息

### Phase 5：测试与优化（2天）
1. **集成测试**
   - 创建测试群
   - 测试各类指令
   - 测试定时任务
   - 测试打卡流程

2. **边界情况处理**
   - 权限校验
   - 并发控制
   - 错误处理

3. **性能优化**
   - 数据库索引
   - 缓存群成员列表
   - 日志记录

---

## 九、配置文件示例

### 9.1 .env
```bash
# 数据库配置
DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=your_password
DB_NAME=dingteam_bot

# 钉钉配置
DING_APP_KEY=your_app_key
DING_APP_SECRET=your_app_secret
DING_ROBOT_CODE=your_robot_code

# 服务配置
PORT=8080
ENV=development

# 时区配置
TIMEZONE=Asia/Shanghai
```

### 9.2 main.go 示例
```go
package main

import (
    "context"
    "log"
    
    "github.com/yourname/dingteam-bot/internal/config"
    "github.com/yourname/dingteam-bot/internal/database"
    "github.com/yourname/dingteam-bot/internal/dingtalk"
    "github.com/yourname/dingteam-bot/internal/handler"
    "github.com/yourname/dingteam-bot/internal/scheduler"
)

func main() {
    // 1. 加载配置
    cfg := config.Load()
    
    // 2. 初始化数据库
    db, err := database.NewDB(cfg)
    if err != nil {
        log.Fatalf("Failed to connect database: %v", err)
    }
    
    // 3. 初始化钉钉客户端
    dingClient := dingtalk.NewClient(cfg.DingAppKey, cfg.DingAppSecret)
    
    // 4. 初始化服务层
    taskSvc := service.NewTaskService(db)
    subSvc := service.NewSubmissionService(db)
    statsSvc := service.NewStatsService(db)
    
    // 5. 初始化处理器
    cmdHandler := handler.NewCommandHandler(
        taskSvc, subSvc, statsSvc, dingClient,
    )
    
    // 6. 初始化调度器
    scheduler := scheduler.NewScheduler(db, dingClient, taskSvc)
    scheduler.Start()
    
    // 7. 启动Stream监听
    streamHandler := dingtalk.NewStreamHandler(dingClient, cmdHandler)
    if err := streamHandler.Start(context.Background()); err != nil {
        log.Fatalf("Failed to start stream: %v", err)
    }
    
    log.Println("DingTeam Bot started successfully!")
    select {} // 保持运行
}
```

---

## 十、测试用例

### 10.1 功能测试清单

#### 任务创建测试
- [ ] @机器人 每周五 17:00 任务:提交周报
- [ ] @机器人 明天 10:00 通知:开例会
- [ ] @机器人 12月1日 14:00 任务:提交月报
- [ ] 非管理员创建任务（应拒绝）

#### 打卡测试
- [ ] 点击ActionCard按钮提交
- [ ] 发送 @机器人 我已提交
- [ ] 重复提交（应提示已提交）
- [ ] 超时提交（应标记为late）

#### 统计查询测试
- [ ] @机器人 本周周报统计
- [ ] @机器人 今日任务统计
- [ ] @机器人 任务列表

#### 定时任务测试
- [ ] 周期任务定时触发
- [ ] 一次性任务定时触发
- [ ] 超时自动通报

#### 任务管理测试
- [ ] @机器人 删除任务 写周报
- [ ] @机器人 暂停任务 写周报
- [ ] @机器人 恢复任务 写周报

---

## 十一、后续优化方向（MVP之后）

### 11.1 功能增强
- [ ] 支持任务模板（快速创建常用任务）
- [ ] 支持多群同步任务
- [ ] 支持任务提醒对象配置（指定人员）
- [ ] 支持附件上传（提交时上传周报文件）
- [ ] Web管理后台（可视化管理任务）

### 11.2 性能优化
- [ ] 使用Redis缓存群成员信息
- [ ] 异步处理消息回调
- [ ] 数据库连接池优化
- [ ] 批量发送消息（减少API调用）

### 11.3 运维监控
- [ ] Prometheus指标采集
- [ ] 任务执行日志
- [ ] 错误告警（钉钉通知）
- [ ] 健康检查接口

---

## 十二、注意事项

### 12.1 钉钉限流
- **消息发送**：每个机器人每分钟最多发送20条消息
- **API调用**：每个应用每分钟最多1500次
- **解决方案**：实现消息队列 + 限流器

### 12.2 时区处理
- 所有时间统一使用 `Asia/Shanghai` 时区
- 数据库存储使用 UTC，展示时转换为本地时区

### 12.3 权限校验
- 每次处理管理指令前校验权限
- 缓存群成员角色信息（定期刷新）

### 12.4 异常处理
- 所有外部调用（钉钉API、数据库）必须有重试机制
- 记录详细错误日志
- 用户友好的错误提示

### 12.5 数据安全
- 敏感配置（AppSecret）使用环境变量
- 数据库连接使用SSL
- 日志脱敏处理

---

## 十三、快速启动指南

### 13.1 环境准备
```bash
# 1. 安装PostgreSQL
# 2. 安装Go 1.21+

# 3. 克隆项目
git clone https://github.com/yourname/dingteam-bot.git
cd dingteam-bot

# 4. 安装依赖
go mod download

# 5. 配置环境变量
cp .env.example .env
# 编辑 .env，填入钉钉AppKey、AppSecret等
```

### 13.2 数据库初始化
```bash
# 创建数据库
createdb dingteam_bot

# 执行迁移脚本
psql -U postgres -d dingteam_bot -f internal/database/migrations/001_init.sql
```

### 13.3 启动服务
```bash
go run cmd/server/main.go
```

### 13.4 钉钉配置
1. 登录钉钉开发者后台
2. 创建企业内部应用
3. 开启Stream推送
4. 配置机器人权限（发送消息、接收消息）
5. 添加机器人到测试群

---

## 十四、FAQ

### Q1: 如何处理用户改名？
A: 每次收到消息时更新 `group_members` 表的 `user_name` 字段。

### Q2: 如何处理任务时间冲突？
A: 同一时间可以有多个任务，分别发送消息即可。

### Q3: 如何支持@指定人员？
A: 在消息文本中使用 `@用户ID` 格式，钉钉会自动识别。

### Q4: 一次性任务执行后如何处理？
A: 执行后自动将 `status` 改为 `completed`，不再触发。

### Q5: 如何防止重复打卡？
A: 数据库 `submissions` 表设置 `(execution_id, user_id)` 唯一索引。

---

## 十五、总结

本文档详细描述了 DingTeam Bot MVP 的完整技术方案，包括：
- ✅ 清晰的需求定义（任务 vs 通知）
- ✅ 完整的数据库设计（5张表）
- ✅ 详细的系统架构（目录结构 + 核心组件）
- ✅ 关键代码实现（解析器、服务层、调度器）
- ✅ 分阶段开发计划（10-12天完成MVP）
- ✅ 测试用例与注意事项

**预计开发时间**：10-12个工作日（单人）

**关键技术点**：
1. 钉钉Stream API接入
2. Cron定时任务调度
3. 自然语言时间解析
4. 任务 vs 通知的区分设计
5. 超时检查机制

**下一步行动**：
1. 搭建开发环境
2. 按照 Phase 1-5 逐步实现
3. 在测试群验证功能
4. 收集用户反馈
5. 迭代优化

祝开发顺利！🚀
