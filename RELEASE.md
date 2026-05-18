# v1.0.0 - CodeBuddy Proxy 首个正式发布

## 概述

CodeBuddy Proxy 是一个 OpenAI API 兼容的代理服务，支持将请求转发至 CodeBuddy 后端，并提供 Anthropic Messages API 和 OpenAI Responses API 的格式转换能力。

## 功能特性

- **OpenAI Chat Completions 兼容**：支持 `/v1/chat/completions` 端点，含流式与非流式响应
- **Anthropic Messages API 兼容**：支持 `/v1/messages` 端点，自动完成请求/响应/SSE 格式转换
- **OpenAI Responses API 兼容**：支持 `/v1/responses` 端点，自动完成 input→messages 转换与 Chat SSE→Responses SSE 流式转换
- **OAuth2 Device Flow 认证**：支持 `/auth/start`、`/auth/poll`、`/auth/manual`、`/auth/status` 完整认证流程
- **Token 持久化**：自动加载/保存 Token，支持 JWT 解析与过期检测
- **SSE 流式转发**：完整的流式响应代理，含字段清理与 id 替换
- **自动登录**：支持启动时自动完成认证流程

## 下载文件

| 文件 | 平台 | 架构 | 大小 |
|------|------|------|------|
| `codebuddy-proxy-mac-arm64` | macOS | ARM64 (Apple Silicon) | ~29 MB |
| `codebuddy-proxy-mac-intel` | macOS | x86_64 (Intel) | ~31 MB |
| `codebuddy-proxy-windows-386.exe` | Windows | 386 | ~29 MB |
| `codebuddy-proxy-windows-amd64.exe` | Windows | AMD64 | ~30 MB |

## SHA256 校验和

```
7b19d47a7b4ed3210f4f8b7709178f9ddfd37355f42d0c89eb1e60fb7718cca0  codebuddy-proxy-mac-arm64
9899b78581882e05f8a5a4c41cd1291fc784bcb9c75682412f25bafd13f3af23  codebuddy-proxy-mac-intel
c4de2d3e4c997f212dd619d4acc6b780cd1bd10aeb7d40cf0a84edb5f7960433  codebuddy-proxy-windows-386.exe
c8c24e96f08783d3fb7abe7e4ac3ca08bace32f306ff20c38495726b5f7bb069  codebuddy-proxy-windows-amd64.exe
```

## 使用方式

```bash
# macOS (Apple Silicon)
chmod +x codebuddy-proxy-mac-arm64
./codebuddy-proxy-mac-arm64

# macOS (Intel)
chmod +x codebuddy-proxy-mac-intel
./codebuddy-proxy-mac-intel

# Windows
.\codebuddy-proxy-windows-amd64.exe
```
