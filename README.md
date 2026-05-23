# Uniview CodeBuddy Proxy

将腾讯云 CodeBuddy CN 的聊天 API 代理为标准 OpenAI / Anthropic 兼容格式，使 ChatGPT-Next-Web、LobeChat、Cherry Studio、Cursor、Claude Desktop 等客户端可直接对接。

Go 实现，编译为单个可执行文件，无需安装任何运行时。

## 快速上手（Claude Code / Codex / Open Code）

1. 从 [GitHub Releases](../../releases) 下载对应平台的可执行文件
2. 启动代理，浏览器扫码登录：

```bash
./uniview-codebuddy-proxy
```

3. 在客户端中切换模型提供商，配置代理地址：

```
API Base URL: http://localhost:1026/v1
```

配置完成后即可在 Claude Code、Codex、Open Code 等工具中使用 CodeBuddy 提供的模型。

## 功能

- **OpenAI Chat Completions** — `/v1/chat/completions`，流式 + 非流式，支持 tool_calls
- **Anthropic Messages** — `/v1/messages`，流式 + 非流式，支持 tool_use / tool_result
- **OpenAI Responses** — `/v1/responses`，流式 + 非流式，支持 function_call
- **动态模型列表** — `/v1/models`，从上游 `/v2/config` 获取并缓存
- **OAuth2 Device Flow** — 浏览器扫码登录，Token 持久化到文件，过期自动重登录
- **API Key 认证** — 可选，设置 `API_PASSWORD` 后所有 `/v1/*` 端点需 `Authorization: Bearer <password>`
- **系统托盘** — 桌面应用模式，带状态图标、登录/退出菜单、日志查看、重启代理、开机自启
- **macOS App Bundle** — 原生 .app 包，支持系统托盘和登录项自启
- **Windows GUI** — 带托盘图标的 Windows GUI 程序，支持注册表自启
- **端口冲突自动解决** — 启动时检测端口占用，自动终止旧进程并重试
- **Liveness Probe** — `max_tokens=1 & stream=true` 时立即返回固定响应（Cursor 兼容）
- **最小消息保证** — 上游请求至少包含 2 条消息，不足时自动补系统消息
- **Chunk 清理** — 清洗上游 SSE 中非标准字段，保证客户端兼容性
- **空闲超时** — 上游流 2 分钟无数据时自动断开，避免连接挂起
- **请求体限制** — 10 MB 请求体上限，防止异常大请求
- **跨平台** — 支持 macOS / Windows / Linux（amd64 + arm64）
- **自动发布** — 通过 GitHub Actions 自动构建和发布

## 快速开始

### 编译

```bash
make build              # 构建二进制 → ./uniview-codebuddy-proxy
make run                # 通过 go run 运行
make clean              # 清理构建产物
```

### 配置

编辑 `.env` 或设置环境变量：

```
PORT=1026
API_PASSWORD=
TOKEN_FILE_PATH=~/.codebuddy-proxy/token.json
```

### 登录

首次使用需通过 OAuth2 获取 Token：

```bash
# 1. 发起认证（自动打开浏览器，无 GUI 时在终端输出登录 URL）
curl http://localhost:1026/auth/start
# 返回 auth_url，在浏览器中打开并登录

# 2. 轮询 Token（使用返回的 auth_state）
curl "http://localhost:1026/auth/poll?auth_state=xxx"
```

也可以手动设置 Token：

```bash
curl -X POST http://localhost:1026/auth/manual \
  -H "Content-Type: application/json" \
  -d '{"bearer_token": "your-token-here"}'
```

Token 会持久化到文件（默认 `~/.codebuddy-proxy/token.json`），进程重启后自动加载。Token 过期后会自动触发重新登录。

### 使用

```bash
# OpenAI 格式
curl http://localhost:1026/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"auto-chat","messages":[{"role":"user","content":"hello"}]}'

# Anthropic 格式
curl http://localhost:1026/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"model":"auto-chat","max_tokens":100,"messages":[{"role":"user","content":"hello"}]}'

# Responses 格式
curl http://localhost:1026/v1/responses \
  -H "Content-Type: application/json" \
  -d '{"model":"auto-chat","input":[{"role":"user","content":"hello"}]}'
```

## API 端点

| 端点 | 方法 | 格式 | 说明 |
|------|------|------|------|
| `/v1/chat/completions` | POST | OpenAI Chat | 聊天补全（流式 + 非流式） |
| `/v1/messages` | POST | Anthropic Messages | Anthropic 格式聊天（流式 + 非流式） |
| `/v1/messages/count_tokens` | POST | Anthropic | 估算 Token 数量 |
| `/v1/responses` | POST | OpenAI Responses | Responses API 格式（流式 + 非流式） |
| `/v1/models` | GET | OpenAI Models | 动态模型列表 |
| `/auth/start` | GET | — | OAuth2 发起认证 |
| `/auth/poll` | GET | — | OAuth2 轮询 Token |
| `/auth/manual` | POST | — | 手动设置 Token |
| `/auth/status` | GET | — | 查看 Token 状态 |
| `/` | GET | — | 服务信息 |
| `/` | HEAD | — | 连通性检查 |
| `/v1` | HEAD | — | 连通性检查 |
| `/_logs` | GET | — | Web 日志查看器 |
| `/health` | GET | — | 健康检查 |

> 所有 `/v1/` 路由同时注册在 `/v1/v1/` 下，兼容会双重拼接路径前缀的客户端。

## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `PORT` | `1026` | 服务监听端口 |
| `API_PASSWORD` | 空 | 非空时所有 `/v1/*` 端点需 Bearer 认证 |
| `TOKEN_FILE_PATH` | `~/.codebuddy-proxy/token.json` | Token 文件存储路径 |
| `CODEBUDDY_API_KEY` | 空 | 已加载但未使用 |

## 可用模型

代理内置以下模型，同时从上游动态获取更多模型：

| 模型 | 提供方 |
|------|--------|
| glm-5.1, glm-5.0, glm-4.7, glm-4.6 | 智谱 |
| minimax-m2.7, minimax-m2.5 | MiniMax |
| kimi-k2.5 | Moonshot |
| deepseek-r1, deepseek-v3-1-lkeap | DeepSeek |
| hunyuan-2.0-instruct | 腾讯 |

所有格式的默认模型均为 `auto-chat`。

## 桌面应用

代理支持系统托盘模式，提供原生桌面体验：

### macOS

```bash
# 构建 .app bundle（ARM64）
make build-mac-app

# 构建 .app bundle（Intel）
make build-mac-app-intel
```

构建产物为 `UniviewCodeBuddyProxy.app`，可拖入 `/Applications` 使用。

- 系统托盘图标随状态变化（正常/灰色/错误）
- 菜单：登录 / 退出 / 查看日志 / 重启代理 / 开机自启 / 退出
- 支持通过系统设置登录项实现开机自启

### Windows

```bash
# 构建 Windows GUI 程序（AMD64）
make build-windows-gui

# 构建 Windows GUI 程序（ARM64）
make build-windows-gui-arm64
```

构建产物为带托盘图标的 GUI 程序，通过注册表实现开机自启。

### 日志

桌面模式下可通过菜单或浏览器访问 `http://localhost:1026/_logs` 查看实时日志。

日志同时写入内存环形缓冲区（1000 行）和文件（`~/.codebuddy-proxy/proxy.log`）。

## 架构

```
OpenAI Chat  (/v1/chat/completions)  ─┐
Anthropic    (/v1/messages)           ─┼─→ Proxy ─→ CodeBuddy /v2/chat/completions
Responses    (/v1/responses)          ─┘
```

所有入站请求转换为上游统一格式（`stream: true`），再由状态机 SSE 翻译器将上游响应转回客户端所需的格式。

## 构建与测试

```bash
make build              # 构建当前平台
make run                # 通过 go run 运行
make clean              # 清理构建产物
make build-all          # 交叉编译所有平台
go test ./...           # 运行测试
go vet ./...            # 静态分析
```

### 交叉编译 Makefile 目标

| 目标 | 平台 |
|------|------|
| `build-darwin-arm64` | macOS ARM64 |
| `build-darwin-amd64` | macOS Intel |
| `build-linux-amd64` | Linux AMD64 |
| `build-linux-arm64` | Linux ARM64 |
| `build-windows-amd64` | Windows AMD64 |
| `build-windows-arm64` | Windows ARM64 |
| `build-mac-app` | macOS .app bundle（ARM64） |
| `build-mac-app-intel` | macOS .app bundle（Intel） |
| `build-windows-gui` | Windows GUI .exe（AMD64） |
| `build-windows-gui-arm64` | Windows GUI .exe（ARM64） |

## 项目结构

```
├── cmd/proxy/main.go               # 入口，注册路由，触发初始认证，--login-item 模式
├── internal/
│   ├── config/config.go            # 环境变量配置
│   ├── version/version.go          # 版本号（构建时注入）
│   ├── logbuf/logbuf.go            # 环形缓冲区 + 文件日志 Writer
│   ├── auth/
│   │   ├── token.go                # Token 缓存 + 文件持久化 + JWT 解析 + 过期自动重登录
│   │   └── handler.go              # OAuth2 路由 + 上游请求头构建
│   ├── proxy/
│   │   ├── handler.go              # 路由注册 + 请求处理 + 认证中间件
│   │   ├── stream.go               # OpenAI Chat SSE 流式转发 + HTTP Client + 空闲超时
│   │   ├── models.go               # 动态模型列表 + 缓存
│   │   ├── anthropic.go            # Anthropic 格式转换
│   │   ├── anthropic_stream.go     # Anthropic 流式转换状态机
│   │   ├── responses.go            # Responses API 格式转换
│   │   └── responses_stream.go     # Responses 流式转换状态机
│   └── systrayapp/
│       ├── app.go                  # 系统托盘应用主逻辑 + 端口冲突处理
│       ├── autostart_darwin.go     # macOS 开机自启（AppleScript）
│       ├── autostart_linux.go      # Linux 开机自启（占位）
│       ├── autostart_windows.go    # Windows 开机自启（注册表）
│       ├── logview.go             # Web 日志查看器路由
│       ├── logview.html           # 日志查看器 HTML 模板
│       └── icons.go               # 嵌入式托盘图标
├── scripts/
│   ├── build-mac.sh               # macOS .app bundle 构建脚本
│   └── build-windows.sh           # Windows GUI 构建脚本
├── assets/icons/                  # 应用图标（png, icns, ico）
├── .github/workflows/release.yml  # GitHub Actions 发布流程
├── .goreleaser.yml                # GoReleaser 配置（仅仓库元数据）
├── Makefile
├── go.mod
└── .env
```

## 技术栈

Go 1.25 + Gin + godotenv + fyne.io/systray + GitHub Actions

## License

[MIT](./LICENSE)
