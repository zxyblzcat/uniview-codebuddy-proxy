# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

codebuddy-proxy is a Go API gateway that translates Tencent Cloud's CodeBuddy proprietary chat API into three standard formats: OpenAI Chat Completions, Anthropic Messages, and OpenAI Responses API. This lets any compatible client (ChatGPT-Next-Web, LobeChat, Cherry Studio, Cursor, Claude Desktop) talk to CodeBuddy without knowing its protocol.

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

All inbound requests (3 API formats) are converted to a single upstream format: CodeBuddy's OpenAI-shaped `/v2/chat/completions` with `stream: true`. The proxy then translates the upstream SSE response back into the client's requested format.

```
OpenAI Chat  (/v1/chat/completions)  ─┐
Anthropic    (/v1/messages)           ─┼─→ Proxy ─→ CodeBuddy /v2/chat/completions
Responses    (/v1/responses)          ─┘
```

### Package Layout

- `cmd/proxy/main.go` — Entry point; wires routes and triggers initial auth
- `internal/config/` — Env-based config loaded via `init()` (PORT, API_PASSWORD, CODEBUDDY_API_KEY)
- `internal/auth/` — OAuth2 Device Flow + in-memory token cache + upstream header builder
- `internal/proxy/` — Route handlers, format converters, SSE stream translators

### Key Design Decisions

- **Forced upstream streaming**: The proxy always sends `stream: true` to upstream, even for non-streaming client requests. Non-streaming responses are assembled by collecting all SSE chunks first (`CollectUpstreamChunks`).
- **Single HTTP client**: `httpClient` (no total timeout, 30min response header timeout) for all upstream requests. Supports long-thinking models that may take minutes before first token. `FetchModels` uses `context.WithTimeout(15s)` for its short-lived config request.
- **State-machine SSE translators**: `anthropic_stream.go` and `responses_stream.go` each implement a state machine tracking text/tool-call block indices to emit format-correct events. These are the most complex functions in the codebase (200-340 lines each). Key state: `textBlockIdx`/`textStarted` for open text blocks; `toolBlockIdxMap`/`toolCallsStarted` mapping OpenAI tool-call indices to target-format block indices. A new content block is opened on first appearance of text or a tool call ID; blocks are closed when switching from text→tool, tool→tool, or on stream end.
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

The upstream base URL (`https://unvcoding.copilot.qq.com`) is hardcoded, not configurable.

### Model List

`FetchModels()` merges a hardcoded `extraModels` list (10 models: glm-5.1, glm-5.0, glm-4.7, glm-4.6, minimax-m2.7, minimax-m2.5, kimi-k2.5, deepseek-r1, deepseek-v3-1-lkeap, hunyuan-2.0-instruct) with models fetched from upstream `/v2/config`. Results are cached for 5 minutes (`modelsCacheTTL = 300`). `inferOwnedBy()` maps model name prefixes to provider names for the `owned_by` field.

### Authentication Flow

1. OAuth2 Device Flow via `/auth/start` → browser login → `/auth/poll` captures token
2. Token stored in-memory only (no disk persistence; restarts require re-auth)
3. Auto-relogin: expired tokens trigger a background goroutine that re-runs the Device Flow
4. Manual token entry via `/auth/manual`
5. On startup, if no cached token exists, the Device Flow starts automatically in the background

### Model Defaults

- `handleChatCompletions` defaults model to `auto-chat`
- `handleAnthropicMessages` and `handleResponses` default to `deepseek-v3`
