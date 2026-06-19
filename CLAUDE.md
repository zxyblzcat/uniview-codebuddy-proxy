# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

codebuddy-proxy is an API gateway that translates Tencent Cloud's CodeBuddy proprietary chat API into standard formats: OpenAI Chat Completions, Anthropic Messages, and OpenAI Responses API. This lets any compatible client (ChatGPT-Next-Web, LobeChat, Cherry Studio, Cursor, Claude Desktop) talk to CodeBuddy without knowing its protocol.

The project is being rewritten as two independent native platform applications — no backend code is shared, each platform implements everything natively:

- **macOS** (`macos/`) — Pure Swift + SwiftUI + Hummingbird HTTP server
- **Windows** (`windows/`) — Pure C# + WinUI 3 + ASP.NET Core Minimal API
- **Linux** — CLI only (deferred)

Shared API contract lives in `shared/API.md`.

## Directory Structure

```
macos/CodeBuddyProxy/    — Swift/SwiftUI macOS app
windows/CodeBuddyProxy/  — C#/WinUI 3 Windows app
shared/                  — Shared API contract documentation
assets/icons/            — App icons (icns, ico, png)
```

## Build Commands

### macOS (from `macos/CodeBuddyProxy/`)
```bash
xcodebuild -scheme CodeBuddyProxy -configuration Release build
# or open in Xcode and build
```

### Windows (from `windows/`, requires Windows)
```bash
dotnet build -c Release -p:Platform=x64
dotnet publish -c Release -p:Platform=x64 --self-contained
```

> ⚠️ WinUI 3 builds require Windows — the XAML compiler (XamlCompiler.exe) is a Windows-only binary and cannot run on macOS/Linux.

## Architecture

### Hub-and-Spoke Translation

All inbound requests (3 API formats) are converted to a single upstream format: CodeBuddy's OpenAI-shaped `/v2/chat/completions` with `stream: true`. The proxy then translates the upstream SSE response back into the client's requested format.

```
OpenAI Chat   (/v1/chat/completions)  ─┐
Anthropic     (/v1/messages)           ─┤─→ Proxy ─→ CodeBuddy /v2/chat/completions
Responses API (/v1/responses)          ─┘
```

### Key Design Decisions

- **Forced upstream streaming**: Always sends `stream: true` to upstream, even for non-streaming client requests. Non-streaming responses are assembled by collecting all SSE chunks first.
- **State-machine SSE translators**: Three streaming translators — OpenAI→OpenAI passthrough, OpenAI→Anthropic Messages, OpenAI→Responses API. The Anthropic translator is the most complex, tracking text/thinking/tool-call block indices.
- **Upstream header construction**: 12-20 fixed headers (including `X-Machine-Id`, `X-User-Id`, B3 tracing headers) are critical for upstream acceptance.
- **Route duplication**: All `/v1/` routes are also registered under `/v1/v1/` for clients that double-prepend the path prefix.
- **Liveness probe detection**: When `max_tokens == 1 && stream == true`, a canned response is returned immediately (for Cursor compatibility).
- **Minimum message guarantee**: Every upstream payload has at least 2 messages; a system message is prepended if the client sends only one.
- **Token Pool**: Round-robin with health awareness — 429 cooldown, 401 permanent mark, auto-relogin.
- **Circuit Breaker**: Three-state (Closed/Open/HalfOpen), max_failures=5, reset_timeout=30s.

### Configuration

| Variable | Default | Purpose |
|----------|---------|---------|
| `PORT` | `1026` | Listen port |
| `API_PASSWORD` | empty | When non-empty, requires auth on `/v1/*` endpoints |
| `IMAGE_AUTO_SWITCH_MODEL` | `true` | 图片自动切换模型开关 |
| `TELEMETRY_ENABLED` | `true` | 用量上报开关 |

The upstream base URL (`https://unvcoding.copilot.qq.com`) is hardcoded, not configurable.

### Model List

A hardcoded `extraModels` list (10 models: glm-5.1, glm-5.0, glm-4.7, glm-4.6, minimax-m2.7, minimax-m2.5, kimi-k2.5, deepseek-r1, deepseek-v3-1-lkeap, hunyuan-2.0-instruct) is merged with models fetched from upstream `/v2/config`. Results are cached for 5 minutes. `inferOwnedBy()` maps model name prefixes to provider names.

### Authentication Flow

1. OAuth2 Device Flow via `/auth/start` → browser login → `/auth/poll` captures token
2. Token 持久化（macOS: Keychain; Windows: Credential Manager），进程重启后自动加载
3. Auto-relogin: expired tokens trigger a background re-run of the Device Flow
4. Manual token entry via `/auth/manual`
5. On startup, if no cached token exists, the Device Flow starts automatically

### UI Design Language: Liquid Glass

Glass-morphism with backdrop blur, inspired by HarmonyOS 7. Four theme presets (Deep/Bright/Midnight/Sunset), 20px border radius, translucent card backgrounds.

## macOS Stack

- Swift + SwiftUI (macOS 13+), Hummingbird HTTP server
- Keychain for tokens, UserDefaults for config
- NSStatusItem system tray, SMAppService auto-start
- URLSession HTTP client, AsyncLineSequence for SSE

## Windows Stack

- C# + WinUI 3 (Win 10 1809+), ASP.NET Core Minimal API
- Credential Manager for tokens, JSON config
- H.NotifyIcon system tray
- HttpClient for upstream requests

## Model Defaults

- Chat Completions defaults to `auto-chat`
- Anthropic Messages defaults to `deepseek-v3`
