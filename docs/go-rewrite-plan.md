# CodeBuddy OpenAI Proxy — Go 重写方案

## 背景

当前项目为单文件 Python 应用（`main.py`），依赖 Python 运行时。为了让半技术用户在 Windows/macOS 上无需安装 Python 环境即可使用，决定用 Go 语言重写，编译为单个可执行文件分发。

## 决策总览

| 决策 | 选择 | 理由 |
|------|------|------|
| 目标用户 | 半技术用户 | 需要配置 API_PASSWORD 和认证流程，有基本门槛 |
| 分发形式 | 单个可执行文件 | 下载即用，零依赖 |
| 语言 | Go | 编译为静态二进制，交叉编译零配置，天生适合代理服务 |
| 重写策略 | 渐进式 | 先跑通核心链路，再逐步加功能 |
| 项目结构 | 标准 Go 项目，扁平包划分 | 先写一起，够大了再拆 |
| HTTP 框架 | Gin | API 风格接近 FastAPI，中间件生态丰富 |
| 配置管理 | 环境变量 + `.env`（godotenv） | 和现有版本行为一致 |
| Token 持久化 | 文件（`token.json`） | 和现有版本行为一致 |
| SSE 转发 | 标准库手动解析（bufio.Scanner） | 需要逐事件做格式转换，手动解析灵活性更高 |

## 第一阶段功能清单 ✅ 已完成

核心功能，验证通过后再加 Anthropic/Responses API。

### 1. OAuth2 Device Flow ✅

| 端点 | 方法 | 说明 |
|------|------|------|
| `/auth/start` | GET | 获取 auth_state 和 auth_url |
| `/auth/poll?auth_state=xxx` | GET | 轮询直到获取 accessToken |
| `/auth/manual` | POST | 手动设置 Bearer Token |
| `/auth/status` | GET | 查看当前 Token 状态 |

上游接口：
- `https://unvcoding.copilot.qq.com/v2/plugin/auth/state`
- `https://unvcoding.copilot.qq.com/v2/plugin/auth/token`

实现文件：`internal/auth/handler.go`、`internal/auth/token.go`

### 2. Chat Completions ✅

| 端点 | 方法 | 说明 |
|------|------|------|
| `/v1/chat/completions` | POST | OpenAI 兼容格式，支持流式和非流式 |

上游接口：
- `https://unvcoding.copilot.qq.com/v2/chat/completions`

核心逻辑：
- 流式请求：直接透传上游 SSE 事件，替换 `model` 和 `id` 字段
- 非流式请求：也以流式请求上游，服务端收集所有 chunk 后组装完整响应返回
- **两种模式都强制向上游发送 `stream: true`**
- 清理上游返回的非标准字段（`reasoning_content`、`extra_fields`、`function_call: null`、`logprobs` 等）
- 探活请求检测：`max_tokens=1, stream=true` 时返回简单非流式响应

实现文件：`internal/proxy/handler.go`、`internal/proxy/stream.go`

### 3. 模型列表 ✅

| 端点 | 方法 | 说明 |
|------|------|------|
| `/v1/models` | GET | 动态获取模型列表 |

上游接口：
- `https://unvcoding.copilot.qq.com/v2/config`

逻辑：
- 从 `/v2/config` 提取 `requestBody.model` 和 `name`
- 合并额外模型列表（EXTRA_MODELS）
- 缓存 5 分钟

额外模型：
- `glm-5.1`、`glm-5.0`、`glm-4.7`、`glm-4.6`
- `minimax-m2.7`、`minimax-m2.5`
- `kimi-k2.5`
- `deepseek-r1`、`deepseek-v3-1-lkeap`
- `hunyuan-2.0-instruct`

实现文件：`internal/proxy/models.go`

### 4. API Key 认证中间件 ✅

- `API_PASSWORD` 非空时，所有 `/v1/*` 端点需 Bearer 认证
- 支持 `Authorization: Bearer <key>` 和 `x-api-key: <key>` 两种方式

实现文件：`internal/proxy/handler.go`

### 5. 健康检查 ✅

| 端点 | 方法 | 说明 |
|------|------|------|
| `/` | GET | 服务信息和端点列表 |
| `/health` | GET | 健康检查 |
| `HEAD /v1` | HEAD | 连通性检查 |

实现文件：`internal/proxy/handler.go`

## 第二阶段：Anthropic Messages API ✅ 已完成

| 端点 | 方法 | 说明 |
|------|------|------|
| `/v1/messages` | POST | Anthropic 兼容格式，支持流式和非流式 |

已实现：
- `extractAnthropicText()` — 从 Anthropic content 字段提取纯文本
- `convertAnthropicMessagesToOpenai()` — Anthropic 消息格式转 OpenAI（system、user、assistant、tool_result、tool_use）
- `convertToolsAnthropicToOpenai()` — `input_schema` 转 `parameters`
- `convertToolChoiceAnthropicToOpenai()` — tool_choice 格式映射（auto/any/none/tool）
- `finishReasonToStopReason()` — finish_reason 到 stop_reason 映射
- `convertOpenAIToAnthropicResponse()` — OpenAI 收集结果转 Anthropic 非流式响应
- `anthropicSSE()` — Anthropic SSE 事件格式化
- `StreamAnthropicMessages()` — 流式转换状态机（OpenAI SSE → Anthropic SSE）
- 探活请求检测：`max_tokens=1, stream=true` 时返回 Anthropic 格式简单响应

Anthropic SSE 事件序列：
- `message_start` → `content_block_start` → `content_block_delta`（多个）→ `content_block_stop` → `message_delta` → `message_stop`

实现文件：`internal/proxy/anthropic.go`、`internal/proxy/anthropic_stream.go`

## 第三阶段：OpenAI Responses API ✅ 已完成

| 端点 | 方法 | 说明 |
|------|------|------|
| `/v1/responses` | POST | OpenAI Responses API 格式，支持流式和非流式 |

已实现：
- `convertResponsesToChat()` — input 数组转 messages 数组（支持 string 和 content block 数组）
- `convertChatToResponsesResult()` — Chat 响应转 Responses 格式（含 tool_calls 转换）
- `responsesSSE()` — Responses API SSE 事件格式化
- `StreamResponsesSSE()` — 流式转换（OpenAI Chat SSE → Responses API SSE）

Responses API SSE 事件序列：
- `response.created` → `response.in_progress` → `response.output_item.added` → `response.content_part.added` → `response.output_text.delta`（多个）→ `response.output_text.done` → `response.content_part.done` → `response.output_item.done` → `response.completed`

实现文件：`internal/proxy/responses.go`、`internal/proxy/responses_stream.go`

## 项目结构（实际实现）

```
uniview-codebuddy-proxy/
├── cmd/proxy/main.go                  # 入口：加载配置、初始化 Gin、注册路由、启动服务
├── internal/
│   ├── config/config.go               # 配置（环境变量 + .env）
│   ├── auth/
│   │   ├── token.go                   # Token 持久化（load/save/JWT 解析）
│   │   └── handler.go                 # OAuth2 Device Flow 路由 + 上游请求头构建
│   └── proxy/
│       ├── handler.go                 # 路由注册 + 请求处理（Chat Completions、Anthropic Messages、Responses、Models、Health）
│       ├── stream.go                  # SSE 流式转发（OpenAI Chat 流式 + 非流式收集 + 字段清理）
│       ├── models.go                   # 模型列表（动态获取 + 缓存 + EXTRA_MODELS）
│       ├── anthropic.go               # Anthropic 格式转换（请求/响应/SSE 格式化）
│       ├── anthropic_stream.go        # Anthropic 流式响应转换（OpenAI SSE → Anthropic SSE 状态机）
│       ├── responses.go               # Responses API 格式转换（input→messages、Chat→Responses、SSE 格式化）
│       └── responses_stream.go        # Responses API 流式响应转换（Chat SSE → Responses SSE）
├── go.mod
├── go.sum
├── .env                               # 环境变量
├── .codebuddy_creds/                  # Token 存储
│   └── token.json
└── Makefile                           # 编译命令（交叉编译 Windows/macOS）
```

## 环境变量

| 变量 | 默认值 | 用途 |
|------|--------|------|
| `PORT` | `8000` | 服务监听端口 |
| `API_PASSWORD` | 空 | 非空时所有 `/v1/*` 端点需认证 |
| `CODEBUDDY_API_KEY` | 空 | 预留字段 |
| `CREDS_DIR` | `.codebuddy_creds` | Token 存储目录 |

## 交叉编译

```bash
# macOS ARM
GOOS=darwin GOARCH=arm64 go build -o codebuddy-proxy-mac ./cmd/proxy

# macOS Intel
GOOS=darwin GOARCH=amd64 go build -o codebuddy-proxy-mac-intel ./cmd/proxy

# Windows 64-bit
GOOS=windows GOARCH=amd64 go build -o codebuddy-proxy.exe ./cmd/proxy
```

编译产物约 30MB。

## 技术栈

| 组件 | 选择 |
|------|------|
| 语言 | Go |
| Web 框架 | Gin |
| 环境变量 | godotenv |
| HTTP 客户端 | 标准库 `net/http` |
| SSE 解析 | 标准库 `bufio.Scanner` |
| JSON 处理 | 标准库 `encoding/json` |

## 端点总览

| 端点 | 方法 | 格式 | 说明 |
|------|------|------|------|
| `/` | GET | — | 服务信息和端点列表 |
| `/health` | GET | — | 健康检查 |
| `/v1` | HEAD | — | 连通性检查 |
| `/auth/start` | GET | — | OAuth2 Device Flow 发起 |
| `/auth/poll` | GET | — | OAuth2 Token 轮询 |
| `/auth/manual` | POST | — | 手动设置 Bearer Token |
| `/auth/status` | GET | — | Token 状态查询 |
| `/v1/chat/completions` | POST | OpenAI Chat | 聊天补全（流式+非流式） |
| `/v1/messages` | POST | Anthropic Messages | Anthropic 格式聊天（流式+非流式） |
| `/v1/responses` | POST | OpenAI Responses | Responses API 格式（流式+非流式） |
| `/v1/models` | GET | OpenAI Models | 动态模型列表 |
