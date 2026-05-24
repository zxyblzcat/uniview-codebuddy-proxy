# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

codebuddy-proxy is a Go API gateway that translates Tencent Cloud's CodeBuddy proprietary chat API into two standard formats: OpenAI Chat Completions and Anthropic Messages. This lets any compatible client (ChatGPT-Next-Web, LobeChat, Cherry Studio, Cursor, Claude Desktop) talk to CodeBuddy without knowing its protocol.

## Build & Run Commands

```bash
make build              # Build binary → ./codebuddy-proxy
make run                # Run via go run ./cmd/proxy
make build-all          # Cross-compile: mac-arm, mac-intel, windows-amd64
make clean              # Remove built binaries
go vet ./...            # Static analysis (no linter configured)
```

No test suite exists. Go 1.25.6, dependencies: `gin v1.12.0`, `godotenv v1.5.1`.

## Architecture

### Hub-and-Spoke Translation

All inbound requests (2 API formats) are converted to a single upstream format: CodeBuddy's OpenAI-shaped `/v2/chat/completions` with `stream: true`. The proxy then translates the upstream SSE response back into the client's requested format.

```
OpenAI Chat  (/v1/chat/completions)  ─┐
Anthropic    (/v1/messages)           ─┘─→ Proxy ─→ CodeBuddy /v2/chat/completions
```

### Package Layout

- `cmd/proxy/main.go` — Entry point; wires routes and triggers initial auth
- `internal/config/` — Env-based config loaded via `init()` (PORT, API_PASSWORD, CODEBUDDY_API_KEY)
- `internal/auth/` — OAuth2 Device Flow + token 缓存（内存+文件持久化）+ upstream header builder
- `internal/proxy/` — Route handlers, format converters, SSE stream translators

### Key Design Decisions

- **Forced upstream streaming**: The proxy always sends `stream: true` to upstream, even for non-streaming client requests. Non-streaming responses are assembled by collecting all SSE chunks first (`CollectUpstreamChunks`).
- **Single HTTP client**: `httpClient` (no total timeout, 30min response header timeout) for all upstream requests. Supports long-thinking models that may take minutes before first token. `FetchModels` uses `context.WithTimeout(15s)` for its short-lived config request.
- **State-machine SSE translator**: `anthropic_stream.go` implements a state machine tracking text/tool-call block indices to emit format-correct events. This is the most complex function in the codebase (200-340 lines). Key state: `textBlockIdx`/`textStarted` for open text blocks; `toolBlockIdxMap`/`toolCallsStarted` mapping OpenAI tool-call indices to target-format block indices. A new content block is opened on first appearance of text or a tool call ID; blocks are closed when switching from text→tool, tool→tool, or on stream end.
- **Upstream header construction**: `BuildUpstreamHeaders()` and `authPollHeaders()` assemble 12-20 fixed headers (including `X-Machine-Id`, `X-User-Id`, B3 tracing headers). These are critical for upstream acceptance — changing them will break auth or chat requests.
- **Route duplication**: All `/v1/` routes are also registered under `/v1/v1/` to handle clients that double-prepend the path prefix.
- **Liveness probe detection**: When `max_tokens == 1 && stream == true`, a canned response is returned immediately without contacting upstream (for Cursor compatibility).
- **Minimum message guarantee**: Every upstream payload has at least 2 messages; a system message is prepended if the client sends only one.
- **Chunk sanitization**: `cleanChunkChoices()` strips non-standard upstream fields from SSE chunks in the OpenAI streaming path.

### Configuration

| Variable | Default | Purpose |
|----------|---------|---------|
| `PORT` | `1026` | Listen port |
| `API_PASSWORD` | empty | When non-empty, requires auth on `/v1/*` endpoints |
| `CODEBUDDY_API_KEY` | — | Loaded but currently unused |
| `TOKEN_FILE_PATH` | `~/.codebuddy-proxy/token.json` | Token 文件存储路径 |

The upstream base URL (`https://unvcoding.copilot.qq.com`) is hardcoded, not configurable.

### Model List

`FetchModels()` merges a hardcoded `extraModels` list (10 models: glm-5.1, glm-5.0, glm-4.7, glm-4.6, minimax-m2.7, minimax-m2.5, kimi-k2.5, deepseek-r1, deepseek-v3-1-lkeap, hunyuan-2.0-instruct) with models fetched from upstream `/v2/config`. Results are cached for 5 minutes (`modelsCacheTTL = 300`). `inferOwnedBy()` maps model name prefixes to provider names for the `owned_by` field.

### Authentication Flow

1. OAuth2 Device Flow via `/auth/start` → browser login → `/auth/poll` captures token
2. Token 持久化到文件（默认 `~/.codebuddy-proxy/token.json`），进程重启后自动加载
3. Auto-relogin: expired tokens trigger a background goroutine that re-runs the Device Flow
4. Manual token entry via `/auth/manual`
5. On startup, if no cached token exists, the Device Flow starts automatically in the background
6. 在无 GUI 环境（`DISPLAY` 和 `WAYLAND_DISPLAY` 均未设置）下，`OpenBrowser` 会在终端输出登录 URL 而不是打开浏览器
7. Token 过期时清除文件，触发 Device Flow 终端提示重新登录

### Model Defaults

- `handleChatCompletions` defaults model to `auto-chat`
- `handleAnthropicMessages` defaults to `deepseek-v3`
