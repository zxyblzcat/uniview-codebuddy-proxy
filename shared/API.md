# CodeBuddy Proxy — 内部 API 契约与 SSE 翻译规范

本文档定义 macOS (Swift) 和 Windows (C#) 两个独立实现必须遵循的共同规范，确保行为一致。

---

## 1. 上游接口规范

### 1.1 基础 URL

```
BaseURL = https://unvcoding.copilot.qq.com
Domain  = unvcoding.copilot.qq.com
```

### 1.2 端点

| 端点 | 方法 | 用途 |
|------|------|------|
| `/v2/plugin/auth/state` | POST | 发起 OAuth2 Device Flow |
| `/v2/plugin/auth/token` | GET | 轮询 Token（query: `state`） |
| `/v2/plugin/auth/token/refresh` | POST | 刷新 Token |
| `/v2/chat/completions` | POST | 聊天补全（所有代理请求的统一上游端点） |
| `/v2/completions` | POST | 代码补全 |
| `/v2/embeddings` | POST | 嵌入 |
| `/v2/config` | GET | 获取上游模型配置 |
| `/v2/report` | POST | 遥测事件上报 |

### 1.3 CodeBuddy 版本常量

```
PRODUCT_VERSION = "2.92.0"
USER_AGENT = "CLI/{PRODUCT_VERSION} CodeBuddy/{PRODUCT_VERSION}"
```

---

## 2. 上游 Header 构建规范

### 2.1 Device Flow 发起头 (`authStartHeaders`)

```
Host: {Domain}
Accept: */*
Content-Type: application/x-www-form-urlencoded
Cache-Control: no-cache
Pragma: no-cache
Connection: close
X-Requested-With: XMLHttpRequest
X-Domain: {Domain}
X-No-Authorization: true
X-No-User-Id: true
X-No-Enterprise-Id: true
X-No-Department-Info: true
User-Agent: {USER_AGENT}
X-Product: SaaS
X-Request-ID: {randomUUID}
```

### 2.2 Token 轮询头 (`authPollHeaders`)

在 `authStartHeaders` 基础上追加 B3 追踪头：

```
b3: {traceId}-{spanId}-1
X-B3-TraceId: {traceId}
X-B3-ParentSpanId: {parentId}
X-B3-SpanId: {spanId}
X-B3-Sampled: 1
```

### 2.3 API 请求头 (`buildUpstreamHeaders`)

```
Accept: text/event-stream
Content-Type: application/json
b3: {traceId}-{spanId}-1
X-B3-TraceId: {traceId}
X-B3-ParentSpanId: {parentId}
X-B3-SpanId: {spanId}
X-B3-Sampled: 1
X-Agent-Intent: {intent}              # craft | CodeCompletion | embedding
X-Env-ID: production
X-Domain: {Domain}
X-Product: SaaS
X-User-Id: {userId}                   # 来自 Token 的 UserID
X-Machine-Id: {machineId}             # FNV-128 hash(hostname + homeDir)
X-Request-ID: {randomUUID}
X-Conversation-ID: {randomUUID}
X-Session-ID: {randomUUID}
X-IDE-Type: CLI
X-Product-Version: {PRODUCT_VERSION}
User-Agent: {USER_AGENT}
Authorization: Bearer {bearerToken}    # 由调用层追加
```

### 2.4 Claude Inject 头（当 `CLAUDE_INJECT=true` 时追加/合并）

```
X-Enterprise-Id: (空字符串)
X-Tenant-Id: (空字符串)
X-IDE-Name: claude-code
X-IDE-Version: 2.92.0
x-codebuddy-request: true
anthropic-beta: claude-code-20250219,interleaved-thinking-2025-05-14,prompt-caching-scope-2026-01-05,effort-2025-11-24,context-management-2025-06-27,extended-cache-ttl-2025-04-11
```

**注意**: `anthropic-beta` 头需要逗号合并，不可覆盖。

### 2.5 X-Machine-Id 生成算法

```
FNV-128 hash of (hostname + homeDirectoryPath)
输出为 32 位小写十六进制字符串
```

### 2.6 Extra Headers 合并规则

客户端可能传入额外头（如 `anthropic-beta`），合并时：
- 受保护头不可覆盖：`Authorization`, `X-Machine-Id`, `X-User-Id`, `Content-Type`, `Host`
- `anthropic-beta` 头逗号合并（不覆盖）
- 其余头直接设置

---

## 3. 代理请求处理流水线

### 3.1 通用处理步骤

1. 获取 Bearer Token（从 Token 池轮询选择）
2. 解析请求 Body
3. 图片检测：若包含图片 + `IMAGE_AUTO_SWITCH_MODEL=true`，将请求的 model 切换为视觉模型
4. 构建上游 Payload
5. 强制 `stream: true`（即使客户端请求非流式）
6. 合并 `stream_options: { include_usage: true }`（保留客户端传值）
7. `ensureMinMessages`：确保至少 2 条消息（不足时添加系统消息）
8. `sanitizeToolChoice`：上游只接受字符串 `auto/required/none`，对象形式降级为 `required`
9. 探针检测：`max_tokens == 1 && stream == true` → 返回合成响应，不接触上游
10. 上游请求 + 重试 + 熔断
11. 流式翻译或收集后返回

### 3.2 探针响应格式

**OpenAI 格式：**
```json
{"id":"chatcmpl-probe","object":"chat.completion","created":0,"model":"probe","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}
```

**Anthropic 格式（SSE）：**
```
event: message_start
data: {"type":"message_start","message":{"id":"msg_probe","type":"message","role":"assistant","content":[],"model":"probe","stop_reason":null,"usage":{"input_tokens":0,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}

event: message_stop
data: {"type":"message_stop"}
```

---

## 4. SSE 流式翻译规范

### 4.1 OpenAI → OpenAI (passthrough)

**行为：**
- 逐块转发，替换 `model` 和 `id` 字段
- `cleanChunkChoices`：仅保留 `role`, `content`, `tool_calls`, `reasoning_content`；规范化 `finish_reason`
- 提取 `usage.completion_tokens_details.reasoning_tokens`
- 非流式：收集所有 chunk 后组装完整响应

### 4.2 OpenAI → Anthropic Messages

**状态变量：**
```
nextBlockIdx: Int = 0          // 下一个 content block 的索引
thinkingBlockIdx: Int? = nil   // 当前 thinking block 索引
textBlockIdx: Int? = nil       // 当前 text block 索引
toolBlockIdxMap: [String: Int] = [:]  // tool_call.id → block index
started: Bool = false          // 是否已发送 message_start
finished: Bool = false         // 是否已发送 message_stop
inputTokens: Int = 0
outputTokens: Int = 0
cacheCreationTokens: Int = 0
```

**事件发射规则：**

| 触发条件 | 发射事件 |
|---------|---------|
| 首次收到任何内容 | `message_start` (含 usage) |
| `reasoning_content` 且无 thinking block | `content_block_start` (type: thinking, index: nextBlockIdx); `content_block_delta` (thinking_delta) |
| `reasoning_content` 且有 thinking block | `content_block_delta` (thinking_delta) |
| `content` 且有 thinking block | 关闭 thinking: `content_block_delta` (signature_delta); `content_block_stop` |
| `content` 且无 text block | `content_block_start` (type: text, index: nextBlockIdx); `content_block_delta` (text_delta) |
| `content` 且有 text block | `content_block_delta` (text_delta) |
| `tool_calls` 首次出现某 id | 关闭 thinking/text; `content_block_start` (type: tool_use, index: nextBlockIdx, id, name) |
| `tool_calls` 后续 chunk | `content_block_delta` (input_json_delta, index) |
| `finish_reason` 或 `[DONE]` | `closeOpenBlocks`; `message_delta` (stop_reason, usage); `message_stop` |
| 错误/扫描错误 | 关闭 open blocks; `message_delta` (stop_sequence: "\<stream_error\>") |

**finish_reason 映射：**
| OpenAI | Anthropic |
|--------|-----------|
| `stop` | `end_turn` |
| `tool_calls` | `tool_use` |
| `length` | `max_tokens` |
| `content_filter` | `end_turn` |

**关闭 open blocks 顺序：**
1. 如有 thinking block → `content_block_delta` (signature_delta) + `content_block_stop`
2. 如有 text block → `content_block_stop`
3. 如有 tool blocks → 逐个 `content_block_stop`

### 4.3 OpenAI → Responses API

**状态变量：**
```
started: Bool = false
finished: Bool = false
textStarted: Bool = false
contentIndex: Int = 0
promptTokens: Int = 0
completionTokens: Int = 0
finishReason: String? = nil
fullContent: StringBuilder = ""
toolCalls: [Int: ToolCallState] = [:]   // OpenAI tool_call index → state
toolCallOrder: [Int] = []               // 按 OpenAI 索引排序
```

**ToolCallState：**
```
id: String
name: String
arguments: StringBuilder
outputIndex: Int          // 在 Responses API 输出中的索引
started: Bool
```

**事件发射规则：**

| 触发条件 | 发射事件 |
|---------|---------|
| 首次收到任何内容 | `response.created` + `response.in_progress` |
| `content` 且 !textStarted | `response.output_item.added` (type: message); `response.content_part.added` (type: output_text); textStarted=true |
| `content` 且 textStarted | `response.output_text.delta` |
| `tool_calls` 首次出现某 id | `response.output_item.added` (type: function_call, id, name, call_id) |
| `tool_calls` 后续 chunk | `response.function_call_arguments.delta` |
| `finish_reason` 或 `[DONE]` | 关闭 text: `response.output_text.done` + `response.content_part.done` + `response.output_item.done`; 关闭 tool calls; `response.completed` |
| 错误无内容 | `response.created` + 截断的 `response.completed` |

**tool output index 计算：**
- 如 textStarted: text output=0, tools 从 1 开始
- 如 !textStarted: tools 从 0 开始
- 按 tool_calls 在上游首次出现的顺序分配

### 4.4 Completions 流式

与 Chat Completions passthrough 类似，但：
- 对象类型为 `text_completion`
- 使用 `cleanCompletionChunkChoices`（保留 `text`, `finish_reason`）
- 非流式：`CompletionResult` 包含 `Text`, `Model`, `FinishReason`

---

## 5. Token 池行为规范

### 5.1 轮询选择算法

```
1. 从上次返回的索引 + 1 开始轮询
2. 跳过以下状态的 Token：
   - Unavailable（401 标记，永久不可用）
   - Cooldown 中（429 冷却未到期）
   - 已过期（exp < now - 5s 时钟漂移容忍）
3. 如所有 Token 都不可用，回退到最早冷却结束的 Token
4. 如无任何 Token，触发自动重登录
```

### 5.2 Token 生命周期

```
[新 Token] → [活跃] ──429──→ [冷却(30s)] → [活跃]
                  │
                  └──401──→ [不可用(永久)]

[活跃] → [过期] → 自动尝试 Refresh
                    │
                    ├─成功→ [活跃]
                    └─失败→ 自动 Device Flow
```

### 5.3 自动重登录

```
1. 首先尝试 RefreshToken（POST /v2/plugin/auth/token/refresh）
2. 如 Refresh 失败，启动 Device Flow
3. Device Flow 轮询：60 次迭代，每次间隔 3 秒
4. 超时后放弃，下次请求再次触发
```

### 5.4 持久化

| 平台 | Token 存储 | 配置存储 |
|------|----------|---------|
| macOS | Keychain (kSecClassGenericPassword) | UserDefaults |
| Windows | Credential Manager | JSON 文件 |

Token 文件格式（兼容旧版）：
```json
{
  "bearer_token": "...",
  "access_token": "...",
  "refresh_token": "...",
  "token_type": "Bearer",
  "expires_in": 604800,
  "domain": "unvcoding.copilot.qq.com",
  "session_state": "...",
  "created_at": "2026-06-14T12:00:00Z",
  "expires_at": "2026-06-21T12:00:00Z",
  "user_id": "user_xxx"
}
```

---

## 6. 重试与熔断规范

### 6.1 重试

- 最大重试次数：可配置，默认 3（总尝试 = 4）
- 仅重试状态码：429, 502, 503, 504
- 退避算法：`100ms × 2^attempt + random(0, 500ms)`，上限 30s
- 尊重 `Retry-After` 头（上限 60s）
- 重试间刷新 Bearer Token

### 6.2 熔断器

三状态：Closed → Open → HalfOpen

| 状态 | 行为 |
|------|------|
| Closed | 正常放行，连续失败计数 ≥ max_failures → Open |
| Open | 直接拒绝，等待 reset_timeout → HalfOpen |
| HalfOpen | 放行最多 max_failures 个探测请求；成功 → Closed，失败 → Open |

默认值：max_failures=5, reset_timeout=30s

### 6.3 并发控制

- 加权信号量，默认上限 20（0 = 无限制）
- 非阻塞 TryAcquire，超出上限立即返回 429
- 429 响应格式根据 URL 路径自动适配 OpenAI 或 Anthropic 错误格式

---

## 7. 图片理解规范

### 7.1 检测逻辑

遍历 messages 和 system 字段：
- OpenAI 格式：`type: "image_url"` 的 content
- Anthropic 格式：`type: "image"` 且 `source.type: "base64"/"url"`

### 7.2 处理流程

1. 提取图片 URL 或 base64 数据
2. Base64 图片限制 10MB
3. 调用视觉模型（默认 `glm-4.6v`，可配置）生成中文描述
4. 成功：替换为 `[图片描述] {description}`
5. 失败：替换为 `[图片内容解析失败，原始图片已移除]`

---

## 8. 遥测上报规范

### 8.1 事件类型

| 事件 | 触发时机 |
|------|---------|
| `chat_request_send` | 聊天请求发送 |
| `chat_message_response` | 聊天响应返回 |
| `completion_trigger` | 代码补全触发 |
| `completion_response` | 补全响应 |
| `completion_action` | 补全采纳 |
| `responses_request_send` | Responses API 请求 |
| `responses_message_response` | Responses 响应 |
| `upstream_retry` | 上游重试 |
| `upstream_failure` | 上游失败 |

### 8.2 上报机制

- 批量上报：每 2 秒或批次 ≥ 50 条时发送
- POST `/v2/report`，body 为事件数组
- 每个事件计算 `report_delay = now - timestamp`
- 可通过 `TELEMETRY_ENABLED` 开关控制

---

## 9. 路由注册规范

### 9.1 代理路由

| 方法 | 路径 | 处理 |
|------|------|------|
| POST | `/v1/chat/completions` | OpenAI Chat |
| GET | `/v1/models` | 模型列表 |
| GET | `/v1/models/:id` | 单模型详情（Anthropic 格式） |
| POST | `/v1/completions` | 代码补全 |
| POST | `/v1/embeddings` | 嵌入 |
| POST | `/v1/messages` | Anthropic Messages |
| POST | `/v1/messages/count_tokens` | Token 计数估算 |
| POST | `/v1/responses` | Responses API |
| POST | `/v1/responses/compact` | 上下文压缩 |

**所有 `/v1/*` 路由同时注册在 `/v1/v1/*` 下。**

### 9.2 认证路由

| 方法 | 路径 |
|------|------|
| GET | `/auth/start` |
| GET | `/auth/poll` |
| POST | `/auth/manual` |
| GET | `/auth/status` |
| GET | `/auth/tokens` |
| DELETE | `/auth/tokens/:user_id` |
| POST | `/auth/tokens/:user_id/refresh` |

### 9.3 管理 API

| 方法 | 路径 |
|------|------|
| GET | `/api/config` |
| PUT | `/api/config` |
| GET | `/api/logs/stream` |
| DELETE | `/api/logs` |
| GET | `/api/locale` |
| PUT | `/api/locale` |

### 9.4 工具路由

| 方法 | 路径 |
|------|------|
| GET | `/health` |
| GET | `/` |
| HEAD | `/v1` |
| HEAD | `/` |

---

## 10. 上下文限制检测

检测上游返回的错误消息中是否包含以下模式（不区分大小写）：

1. "context length"
2. "prompt is too long"
3. "maximum context length"
4. "exceeds the maximum"
5. "too many tokens"
6. "reduce the length"
7. "token limit"
8. "context window"
9. "max_tokens"
10. "input is too long"

匹配时，返回 `invalid_request_error` 类型（而非 `rate_limit_error`），以便 Claude Code 触发自动压缩。
