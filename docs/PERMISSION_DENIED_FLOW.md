# 权限不足时的处理流程说明

## 🎯 核心问题

当后台查询到用户权限不足时，会发生什么？消息如何最终回复给钉钉用户？

---

## 📊 完整流程图

```
┌─────────────┐
│  钉钉用户    │  @机器人 创建任务 写周报
│  (member)   │
└──────┬──────┘
       │ ① 发送消息
       ↓
┌─────────────────────────────────────┐
│  后台 Message Handler                │
│  - 提取 user_id: "zhang_san"        │
│  - 提取 conversation_id: "cid123"   │
│  - 注册会话映射                      │
│  - 转发给 Dify                      │
└──────┬──────────────────────────────┘
       │ ② 只传消息内容
       ↓
┌─────────────────────────────────────┐
│  Dify 大模型                         │
│  - 理解意图: create_task            │
│  - 提取参数: name, cron_expr        │
└──────┬──────────────────────────────┘
       │ ③ 调用后台 API
       │ POST /api/v1/dify/execute
       │ {
       │   "conversation_id": "cid123",
       │   "action": "create_task",
       │   "params": {...}
       │ }
       ↓
┌─────────────────────────────────────┐
│  后台 Dify Handler                   │
│  ④ 查询用户信息                      │
│     conversation_id → user_id       │
│                                     │
│  ⑤ 查询权限                         │
│     user_id + action → allowed?     │
│                                     │
│  ⑥ 权限检查结果：❌ 权限不足         │
│     - 用户角色：member              │
│     - 需要角色：admin               │
│                                     │
│  ⑦ 记录审计日志                     │
│     INSERT INTO audit_logs (        │
│       user_id: "zhang_san",         │
│       action: "create_task",        │
│       result: "denied",             │
│       reason: "权限不足"             │
│     )                               │
└──────┬──────────────────────────────┘
       │ ⑧ 返回拒绝响应
       │ {
       │   "success": false,
       │   "message": "权限不足",
       │   "reason": "用户角色为 member，无权限执行 create_task"
       │ }
       ↓
┌─────────────────────────────────────┐
│  Dify 大模型                         │
│  ⑨ 接收响应                         │
│  ⑩ 生成友好的回复消息                │
│     "❌ 抱歉，您当前没有创建任务的权限" │
└──────┬──────────────────────────────┘
       │ ⑪ 回复用户（两种方案）
       ↓
┌─────────────────────────────────────┐
│  方案 A: 调用后台发送消息 API        │
│  POST /api/v1/dify/send_message     │
│  {                                  │
│    "conversation_id": "cid123",     │
│    "message": "❌ 抱歉..."          │
│  }                                  │
│                                     │
│  方案 B: Dify 直接调用钉钉 API       │
│  （需要钉钉 access token）           │
└──────┬──────────────────────────────┘
       │ ⑫ 发送消息
       ↓
┌─────────────────────────────────────┐
│  钉钉群聊                            │
│  显示：                              │
│  "❌ 抱歉，您当前没有创建任务的权限。  │
│   原因：您的角色是普通成员。          │
│   请联系管理员开通权限。"             │
└─────────────────────────────────────┘
```

---

## 🔍 详细步骤说明

### 步骤 1-2：消息接收和转发

**代码位置**: `internal/handlers/message_handler.go:46-60`

```go
func (h *MessageHandler) HandleMessage(ctx context.Context, msg *dingtalk.IncomingMessage) error {
    if !msg.IsInAtList {
        return nil
    }

    // 注册会话
    if h.difyHandler != nil {
        h.difyHandler.RegisterSession(
            msg.ConversationID,  // "cid123"
            msg.SenderStaffID,   // "zhang_san"
            msg.SenderNick,      // "张三"
            msg.ConversationID,
        )
    }

    // 转发给 Dify（需要集成 Dify Webhook）
    // forwardToDify(msg.Text.Content)
}
```

### 步骤 3：Dify 调用后台

**Dify HTTP 请求**:
```http
POST http://your-server:8080/api/v1/dify/execute
Content-Type: application/json

{
  "conversation_id": "cid123",
  "action": "create_task",
  "params": {
    "name": "写周报",
    "cron_expr": "0 17 * * 5"
  }
}
```

### 步骤 4-7：后台权限验证

**代码位置**: `internal/handlers/dify_handler.go:148-172`

```go
// ④ 查询用户信息
session, ok := h.sessionStore.GetSession(req.ConversationID)
// → session.UserID = "zhang_san"

// ⑤ 查询权限
allowed, role, reason, err := h.permService.CanExecuteCommand(
    ctx,
    session.UserID,  // "zhang_san"
    "create_task",
)
// → allowed = false
// → role = "member"
// → reason = "用户角色为 member，无权限执行 create_task"

// ⑥⑦ 权限不足：记录日志
if !allowed {
    h.permService.LogPermissionCheck(ctx, session.UserID, action, false, reason)

    // ⑧ 返回拒绝响应
    c.JSON(http.StatusOK, DifyExecuteResponse{
        Success: false,
        Message: "权限不足",
        Reason:  reason,
    })
    return
}
```

### 步骤 8：后台返回的响应

```json
{
  "success": false,
  "message": "权限不足",
  "reason": "用户角色为 member，无权限执行 create_task"
}
```

**HTTP 状态码**: `200 OK`（注意不是 403，因为请求本身是成功的，只是业务逻辑上权限不足）

### 步骤 9-10：Dify 处理响应

Dify 应该在提示词中配置如何处理权限不足的情况：

```
如果后台返回 success=false 且 message="权限不足"，你应该：

1. 理解 reason 字段的含义
2. 将其转换为用户友好的语言
3. 回复用户

示例：
- reason: "用户角色为 member，无权限执行 create_task"
- 转换为: "❌ 抱歉，您当前没有创建任务的权限。\n原因：您的角色是普通成员，只有管理员才能创建任务。\n\n如需创建任务，请联系管理员为您开通权限。"
```

### 步骤 11-12：回复用户

这里有**两种实现方案**：

---

## 🔧 方案 A：调用后台发送消息 API（推荐）

### 实现代码

在 `dify_handler.go` 中添加发送消息 API：

```go
import (
    "dingteam-bot/internal/dingtalk"
)

type DifyHandler struct {
    permService  *services.PermissionService
    taskService  *services.TaskService
    statsService *services.StatsService
    sessionStore *SessionStore
    dtClient     *dingtalk.Client  // 新增：钉钉客户端
}

func NewDifyHandler(
    permService *services.PermissionService,
    taskService *services.TaskService,
    statsService *services.StatsService,
    dtClient *dingtalk.Client,  // 新增参数
) *DifyHandler {
    return &DifyHandler{
        permService:  permService,
        taskService:  taskService,
        statsService: statsService,
        dtClient:     dtClient,
        sessionStore: NewSessionStore(),
    }
}

// SendMessage 发送消息给用户（供 Dify 调用）
// POST /api/v1/dify/send_message
func (h *DifyHandler) SendMessage(c *gin.Context) {
    var req struct {
        ConversationID string `json:"conversation_id" binding:"required"`
        Message        string `json:"message" binding:"required"`
    }

    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{
            "error": "请求参数错误",
        })
        return
    }

    // 从会话中获取群聊ID
    session, ok := h.sessionStore.GetSession(req.ConversationID)
    if !ok {
        c.JSON(http.StatusNotFound, gin.H{
            "error": "会话不存在",
        })
        return
    }

    // 发送消息到钉钉群
    err := h.dtClient.SendGroupMessage(session.GroupChatID, req.Message)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{
            "error": "发送消息失败",
            "detail": err.Error(),
        })
        return
    }

    c.JSON(http.StatusOK, gin.H{
        "success": true,
        "message": "消息已发送",
    })
}
```

### Dify 调用方式

在 Dify 的 Workflow 中添加另一个 HTTP 工具：

**工具名称**: `send_message_to_user`

**请求**:
```http
POST http://your-server:8080/api/v1/dify/send_message
Content-Type: application/json

{
  "conversation_id": "{{conversation_id}}",
  "message": "{{generated_message}}"
}
```

**Dify Workflow 流程**:
```
1. 调用 execute_bot_action
2. 检查响应
3. 如果 success=false:
   - 生成友好的错误消息
   - 调用 send_message_to_user 发送
4. 如果 success=true:
   - 生成成功消息
   - 调用 send_message_to_user 发送
```

---

## 🔧 方案 B：Dify 直接调用钉钉 API

如果 Dify 有钉钉的 `access_token`，可以直接调用钉钉的发送消息 API。

但这种方式不推荐，因为：
- 需要在 Dify 中管理钉钉 token
- 增加了 Dify 的复杂度
- 不符合职责分离原则

---

## 📋 审计日志记录

无论权限是否通过，后台都会记录审计日志：

### 数据库记录

```sql
INSERT INTO permission_audit_logs (
    user_id,
    action,
    resource_type,
    resource_id,
    result,
    reason,
    created_at
) VALUES (
    'zhang_san',
    'create_task',
    'command',
    '',
    'denied',  -- 权限被拒绝
    '用户角色为 member，无权限执行 create_task',
    '2025-01-01 10:00:00'
);
```

### 查询审计日志

管理员可以查询谁尝试执行了什么操作：

```sql
-- 查询某用户的权限被拒绝记录
SELECT * FROM permission_audit_logs
WHERE user_id = 'zhang_san'
  AND result = 'denied'
ORDER BY created_at DESC;

-- 查询最近被拒绝的操作
SELECT user_id, action, reason, created_at
FROM permission_audit_logs
WHERE result = 'denied'
ORDER BY created_at DESC
LIMIT 100;
```

---

## 🎭 示例对话

### 场景 1：权限不足

```
👤 张三（member）: @机器人 创建任务 写周报

🤖 机器人: ❌ 抱歉，您当前没有创建任务的权限。

原因：您的角色是普通成员，只有管理员才能创建任务。

如需创建任务，请联系管理员为您开通权限。
```

**后台日志**:
```
[INFO] Dify 请求: conversation=cid123, user=zhang_san, action=create_task
[WARN] 权限拒绝: user=zhang_san, action=create_task, role=member
[INFO] 审计日志已记录: zhang_san tried create_task - DENIED
```

### 场景 2：权限通过

```
👤 李四（admin）: @机器人 创建任务 写周报

🤖 机器人: ✅ 任务创建成功！

📋 名称: 写周报
⏰ Cron: 0 17 * * 5
📊 类型: TASK
```

**后台日志**:
```
[INFO] Dify 请求: conversation=cid456, user=li_si, action=create_task
[INFO] 权限通过: user=li_si, action=create_task, role=admin
[INFO] 任务创建成功: task_id=1, creator=li_si
```

---

## 🛡️ 安全机制

### 1. 双重验证

即使 Dify 跳过权限检查，后台也会强制验证：

```go
// Dify 可能的错误：直接调用 execute 而不检查
// 但后台仍会验证
if !allowed {
    // 拒绝执行
    return "权限不足"
}
```

### 2. 会话隔离

不同用户的会话是隔离的：

```
user_a: conversation_id_1 → user_id_a
user_b: conversation_id_2 → user_id_b
```

Dify 无法通过 `conversation_id_1` 访问 `user_id_b` 的权限。

### 3. 会话过期

30分钟无活动后会话自动过期，防止滥用：

```go
func (s *SessionStore) cleanExpiredSessions() {
    ticker := time.NewTicker(5 * time.Minute)
    for range ticker.C {
        now := time.Now()
        for id, info := range s.sessions {
            if now.Sub(info.LastActiveTime) > 30*time.Minute {
                delete(s.sessions, id)  // 清理过期会话
            }
        }
    }
}
```

---

## 🚀 部署建议

### 更新后台代码

需要修改 `dify_handler.go` 和 `main.go`，添加：
1. `dtClient` 字段
2. `SendMessage` API
3. 路由注册

### Dify 配置

1. **添加两个 HTTP 工具**:
   - `execute_bot_action`: 执行操作
   - `send_message_to_user`: 发送消息

2. **配置 Workflow**:
   ```
   用户消息 → 理解意图 → execute_bot_action → 检查结果
              ↓
          生成回复 → send_message_to_user
   ```

3. **配置提示词**:
   - 处理 `success=false` 的情况
   - 生成友好的错误消息

---

## 📊 对比总结

| 场景 | 后台行为 | 返回给 Dify | Dify 行为 | 用户看到的 |
|------|---------|------------|-----------|-----------|
| **权限通过** | 执行操作，记录日志 | `success: true` | 发送成功消息 | ✅ 操作成功 |
| **权限不足** | 拒绝执行，记录日志 | `success: false` | 发送拒绝消息 | ❌ 权限不足 |
| **会话过期** | 返回错误 | `error: session expired` | 提示重新发送 | ⚠️ 请重新发送 |
| **参数错误** | 返回错误 | `error: invalid params` | 提示格式错误 | ⚠️ 参数格式错误 |

---

## 💡 总结

当后台查询到权限不足时：

1. ✅ **记录审计日志**：记录用户尝试执行的操作
2. ✅ **返回拒绝响应**：告诉 Dify 权限不足及原因
3. ✅ **Dify 生成友好消息**：将技术性响应转换为用户可读的消息
4. ✅ **回复用户**：通过后台 API 或直接发送消息

核心原则：**后台负责权限验证，Dify 负责用户体验优化**。

---

## 📚 相关文档

- [Dify 集成指南](./DIFY_INTEGRATION_GUIDE.md)
- [完整 API 文档](./API_DOCUMENTATION.md)
- [权限系统说明](./API_DOCUMENTATION.md#权限系统说明)
