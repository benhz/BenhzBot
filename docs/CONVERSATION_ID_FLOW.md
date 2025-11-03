# Conversation ID 传递机制说明

## 🎯 核心问题

**Q**: 如果只传递消息内容给 Dify，Dify 怎么知道 `conversation_id`？

**A**: 后台在调用 Dify API 时，将 `conversation_id` 作为参数显式传递给 Dify，Dify 在回调后台时再传回来。

---

## 🔄 完整数据流

```
┌─────────────────────────────────────────────────────┐
│  Step 1: 钉钉消息到达后台                            │
├─────────────────────────────────────────────────────┤
│  钉钉消息对象:                                       │
│  {                                                  │
│    "conversationId": "cid_abc123",  ← 钉钉提供      │
│    "senderStaffId": "user_zhang",                   │
│    "text": {                                        │
│      "content": "@机器人 每周五15点半前完成周报"      │
│    }                                                │
│  }                                                  │
└─────────────────────────────────────────────────────┘
                      ↓
┌─────────────────────────────────────────────────────┐
│  Step 2: 后台提取并注册会话                          │
├─────────────────────────────────────────────────────┤
│  conversation_id := msg.ConversationID              │
│  user_id := msg.SenderStaffID                       │
│                                                     │
│  sessionStore.Save(conversation_id, {               │
│    UserID: user_id,                                 │
│    Username: "张三"                                  │
│  })                                                 │
└─────────────────────────────────────────────────────┘
                      ↓
┌─────────────────────────────────────────────────────┐
│  Step 3: 后台调用 Dify API                           │
├─────────────────────────────────────────────────────┤
│  POST https://api.dify.ai/v1/chat-messages         │
│  Headers:                                           │
│    Authorization: Bearer {dify_api_key}             │
│  Body:                                              │
│  {                                                  │
│    "query": "每周五15点半前完成周报",                 │
│    "user": "cid_abc123",        ← 传递 ID           │
│    "conversation_id": "cid_abc123"  ← 明确传递      │
│  }                                                  │
└─────────────────────────────────────────────────────┘
                      ↓
┌─────────────────────────────────────────────────────┐
│  Step 4: Dify 接收并存储 conversation_id            │
├─────────────────────────────────────────────────────┤
│  Dify 内部:                                         │
│  - 接收到 conversation_id: "cid_abc123"            │
│  - 存储在当前会话上下文中                            │
│  - 理解用户意图并提取参数                            │
└─────────────────────────────────────────────────────┘
                      ↓
┌─────────────────────────────────────────────────────┐
│  Step 5: Dify 调用后台 execute API                   │
├─────────────────────────────────────────────────────┤
│  POST http://your-server/api/v1/dify/execute       │
│  Body:                                              │
│  {                                                  │
│    "conversation_id": "cid_abc123",  ← Dify 传回    │
│    "action": "create_task",                         │
│    "params": {                                      │
│      "name": "完成周报",                             │
│      "cron_expr": "0 15 * * 5"                      │
│    }                                                │
│  }                                                  │
│                                                     │
│  Dify 配置（HTTP 工具）:                             │
│  {                                                  │
│    "conversation_id": "{{sys.conversation_id}}"     │
│  }                                                  │
└─────────────────────────────────────────────────────┘
                      ↓
┌─────────────────────────────────────────────────────┐
│  Step 6: 后台从会话中查找 user_id                    │
├─────────────────────────────────────────────────────┤
│  session := sessionStore.Get("cid_abc123")          │
│  → UserID: "user_zhang"                             │
│  → Username: "张三"                                  │
│                                                     │
│  验证权限: user_zhang + create_task                  │
└─────────────────────────────────────────────────────┘
```

---

## 💻 代码实现

### 1. 配置文件

**`.env`**:
```bash
# Dify 配置
DIFY_API_URL=https://api.dify.ai/v1
DIFY_API_KEY=app-xxxxxxxxxxxxxxxxx
```

**`internal/config/config.go`**:
```go
type Config struct {
    Server struct {
        Port     string
        Timezone string
    }

    DingTalk struct {
        AppKey      string
        AppSecret   string
        AgentID     string
        RobotCode   string
    }

    // 新增 Dify 配置
    Dify struct {
        APIURL string
        APIKey string
    }

    Database struct {
        Host     string
        Port     string
        User     string
        Password string
        DBName   string
    }
}

func Load() (*Config, error) {
    // ... 其他配置加载

    cfg.Dify.APIURL = os.Getenv("DIFY_API_URL")
    cfg.Dify.APIKey = os.Getenv("DIFY_API_KEY")

    if cfg.Dify.APIURL == "" {
        return nil, fmt.Errorf("DIFY_API_URL is required")
    }

    return cfg, nil
}
```

### 2. 后台调用 Dify API

**`internal/handlers/message_handler.go`**:
```go
package handlers

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io/ioutil"
    "net/http"
    "log"

    "dingteam-bot/internal/config"
    "dingteam-bot/internal/dingtalk"
)

type MessageHandler struct {
    cfg          *config.Config
    taskService  *services.TaskService
    statsService *services.StatsService
    permService  *services.PermissionService
    dtClient     *dingtalk.Client
    difyHandler  *DifyHandler
    httpClient   *http.Client
}

func NewMessageHandler(
    cfg *config.Config,
    taskService *services.TaskService,
    statsService *services.StatsService,
    permService *services.PermissionService,
    dtClient *dingtalk.Client,
    difyHandler *DifyHandler,
) *MessageHandler {
    return &MessageHandler{
        cfg:          cfg,
        taskService:  taskService,
        statsService: statsService,
        permService:  permService,
        dtClient:     dtClient,
        difyHandler:  difyHandler,
        httpClient:   &http.Client{Timeout: 30 * time.Second},
    }
}

// HandleMessage 处理钉钉消息
func (h *MessageHandler) HandleMessage(ctx context.Context, msg *dingtalk.IncomingMessage) error {
    // 只处理 @ 机器人的消息
    if !msg.IsInAtList {
        return nil
    }

    // ① 注册会话（保存 conversation_id → user_id 映射）
    if h.difyHandler != nil {
        h.difyHandler.RegisterSession(
            msg.ConversationID,
            msg.SenderStaffID,
            msg.SenderNick,
            msg.ConversationID,
        )
    }

    // ② 提取消息内容（去除 @机器人 部分）
    content := h.extractContent(msg.Text.Content)
    content = strings.TrimSpace(content)

    log.Printf("收到消息: conversation_id=%s, user_id=%s, content=%s",
        msg.ConversationID, msg.SenderStaffID, content)

    // ③ 调用 Dify API
    err := h.callDifyAPI(ctx, msg.ConversationID, content)
    if err != nil {
        log.Printf("调用 Dify API 失败: %v", err)
        return h.sendReply(msg, "❌ 处理失败，请稍后重试")
    }

    // 注意：不在这里回复，由 Dify 处理完后通过 send_message API 回复
    return nil
}

// DifyChatRequest Dify API 请求结构
type DifyChatRequest struct {
    Query          string                 `json:"query"`
    User           string                 `json:"user"`
    ConversationID string                 `json:"conversation_id,omitempty"`
    ResponseMode   string                 `json:"response_mode"`
    Inputs         map[string]interface{} `json:"inputs,omitempty"`
}

// DifyChatResponse Dify API 响应结构
type DifyChatResponse struct {
    ConversationID string `json:"conversation_id"`
    Answer         string `json:"answer"`
    MessageID      string `json:"message_id"`
}

// callDifyAPI 调用 Dify API
func (h *MessageHandler) callDifyAPI(ctx context.Context, conversationID, query string) error {
    // 构造请求
    requestBody := DifyChatRequest{
        Query:          query,
        User:           conversationID,  // 使用 conversation_id 作为 user 标识
        ConversationID: conversationID,  // 显式传递会话ID
        ResponseMode:   "blocking",      // 阻塞模式，等待完整响应
    }

    jsonData, err := json.Marshal(requestBody)
    if err != nil {
        return fmt.Errorf("序列化请求失败: %w", err)
    }

    // 创建 HTTP 请求
    url := h.cfg.Dify.APIURL + "/chat-messages"
    req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
    if err != nil {
        return fmt.Errorf("创建请求失败: %w", err)
    }

    // 设置请求头
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+h.cfg.Dify.APIKey)

    // 发送请求
    log.Printf("调用 Dify API: url=%s, conversation_id=%s", url, conversationID)
    resp, err := h.httpClient.Do(req)
    if err != nil {
        return fmt.Errorf("调用 Dify API 失败: %w", err)
    }
    defer resp.Body.Close()

    // 读取响应
    body, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        return fmt.Errorf("读取响应失败: %w", err)
    }

    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("Dify API 返回错误: status=%d, body=%s", resp.StatusCode, string(body))
    }

    // 解析响应
    var difyResp DifyChatResponse
    if err := json.Unmarshal(body, &difyResp); err != nil {
        log.Printf("Dify 响应解析失败: %v, body=%s", err, string(body))
        // 不返回错误，因为 Dify 可能通过工具回调来回复
    }

    log.Printf("Dify API 调用成功: conversation_id=%s, message_id=%s",
        difyResp.ConversationID, difyResp.MessageID)

    return nil
}
```

### 3. Dify 配置

在 Dify 平台上配置 HTTP 工具：

**工具名称**: `execute_bot_action`

**请求配置**:
- **Method**: POST
- **URL**: `http://your-server:8080/api/v1/dify/execute`
- **Headers**:
  ```
  Content-Type: application/json
  ```
- **Body**:
  ```json
  {
    "conversation_id": "{{sys.conversation_id}}",
    "action": "{{action}}",
    "params": {{params}}
  }
  ```

**变量说明**:
- `{{sys.conversation_id}}`: Dify 系统变量，自动填充为当前会话ID
- `{{action}}`: 提示词中提取的操作类型
- `{{params}}`: 提示词中提取的参数（JSON 对象）

### 4. Dify 提示词示例

```
你是一个钉钉群助手机器人。

## 当前会话信息

- 会话ID: {{sys.conversation_id}}

## 工作流程

1. 理解用户意图，提取操作类型和参数
2. 调用 execute_bot_action 工具执行操作
3. 根据返回结果回复用户

## 操作类型

| 用户说法 | action | params |
|---------|--------|--------|
| "创建任务 写周报 每周五下午5点" | create_task | {"name": "写周报", "cron_expr": "0 17 * * 5"} |
| "删除任务 1" | delete_task | {"task_id": 1} |
| "我已完成" | complete_task | {"task_id": 1} |

## 示例

用户: "每周五下午3点半前完成周报"

思考过程:
1. 操作类型: create_task
2. 参数:
   - name: "完成周报"
   - cron_expr: "0 15 * * 5"  (周五下午3点)

调用工具:
execute_bot_action(
  conversation_id: "{{sys.conversation_id}}",
  action: "create_task",
  params: {
    "name": "完成周报",
    "cron_expr": "0 15 * * 5"
  }
)

根据返回结果回复用户。
```

---

## 🔑 关键点总结

### 1. conversation_id 的来源

```
钉钉消息 → msg.ConversationID → 后台提取 → 传给 Dify
```

### 2. conversation_id 的流转

```
后台 → Dify (作为请求参数)
     ↓
   Dify 存储在会话上下文
     ↓
   Dify → 后台 (通过 {{sys.conversation_id}} 传回)
```

### 3. 为什么使用 conversation_id

- ✅ 钉钉原生提供，无需额外生成
- ✅ 群聊级别的唯一标识
- ✅ 同一用户在不同群有不同 ID，便于区分上下文
- ✅ 符合钉钉的会话模型

### 4. user_id vs conversation_id

| 项目 | user_id | conversation_id |
|------|---------|----------------|
| **含义** | 用户的唯一标识 | 会话的唯一标识 |
| **作用域** | 全局唯一 | 群聊级别唯一 |
| **用途** | 权限验证、审计日志 | 会话管理、消息路由 |
| **传递给 Dify** | ❌ 不传递（后台保密） | ✅ 传递（会话标识） |

---

## 🚀 部署步骤

### 1. 配置环境变量

```bash
# .env
DIFY_API_URL=https://api.dify.ai/v1
DIFY_API_KEY=app-your-dify-api-key
```

### 2. 更新代码

- 更新 `config.go` 添加 Dify 配置
- 更新 `message_handler.go` 添加 Dify API 调用
- 确保 `dify_handler.go` 正确处理回调

### 3. 在 Dify 配置工具

- 添加 `execute_bot_action` 工具
- 使用 `{{sys.conversation_id}}` 系统变量
- 配置提示词

### 4. 测试流程

```bash
# 1. 启动后台
go run cmd/server/main.go

# 2. 发送钉钉消息
@机器人 每周五下午3点半前完成周报

# 3. 检查日志
# 后台日志应该显示:
# - 收到消息: conversation_id=xxx
# - 调用 Dify API: conversation_id=xxx
# - Dify 回调: conversation_id=xxx
```

---

## 📊 完整示例

### 输入

钉钉消息：
```
@机器人 每周五下午3点半前完成周报
```

### 数据流

```json
// Step 1: 钉钉消息
{
  "conversationId": "cid_abc123",
  "senderStaffId": "user_zhang",
  "text": {
    "content": "@机器人 每周五下午3点半前完成周报"
  }
}

// Step 2: 后台调用 Dify
POST https://api.dify.ai/v1/chat-messages
{
  "query": "每周五下午3点半前完成周报",
  "user": "cid_abc123",
  "conversation_id": "cid_abc123"
}

// Step 3: Dify 回调后台
POST http://your-server/api/v1/dify/execute
{
  "conversation_id": "cid_abc123",
  "action": "create_task",
  "params": {
    "name": "完成周报",
    "cron_expr": "0 15 * * 5"
  }
}

// Step 4: 后台查找用户
sessionStore.Get("cid_abc123")
// → {UserID: "user_zhang", Username: "张三"}

// Step 5: 验证权限并执行
```

---

## 🎓 常见问题

### Q1: 为什么不直接用 user_id 作为会话标识？

**A**:
- user_id 是用户级别的，同一用户在多个群聊中是同一个 ID
- conversation_id 是会话级别的，可以区分不同上下文
- 安全性：user_id 是敏感信息，不应该暴露给 Dify

### Q2: Dify 的 conversation_id 和钉钉的 conversation_id 是同一个吗？

**A**: 是的！我们明确传递钉钉的 `conversation_id` 给 Dify，并要求 Dify 原样传回。

### Q3: 如果 Dify 没有传回 conversation_id 怎么办？

**A**: 后台会返回错误"会话不存在"，并记录日志。这时需要检查 Dify 工具配置是否正确。

### Q4: 会话过期后怎么办？

**A**: 用户重新发送 @ 机器人的消息，后台会重新注册会话。

---

## 📚 相关文档

- [Dify 集成指南](./DIFY_INTEGRATION_GUIDE.md)
- [权限不足处理流程](./PERMISSION_DENIED_FLOW.md)
- [API 文档](./API_DOCUMENTATION.md)
