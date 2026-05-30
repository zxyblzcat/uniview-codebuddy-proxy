package proxy

import (
	"bytes"
	"crypto/subtle"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"uniview-codebuddy-proxy/internal/auth"
	"uniview-codebuddy-proxy/internal/cache"
	"uniview-codebuddy-proxy/internal/config"
	"uniview-codebuddy-proxy/internal/telemetry"
	"uniview-codebuddy-proxy/internal/version"

	"github.com/gin-gonic/gin"
)

// cacheWriter 用于捕获 gin 响应体以便缓存
type cacheWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w *cacheWriter) Write(data []byte) (int, error) {
	w.body.Write(data)
	return w.ResponseWriter.Write(data)
}

// RegisterRoutes 注册所有 /v1/* 路由和健康检查路由
func RegisterRoutes(r *gin.Engine) {
	// 注册路由到 group，同时兼容 /v1/v1/* 双重路径
	registerAPIRoutes(r.Group("/v1"))
	registerAPIRoutes(r.Group("/v1/v1"))

	// 健康检查
	r.GET("/health", handleHealth)
	r.GET("/", optionalAuthMiddleware(), handleRoot)
	r.HEAD("/", handleHeadV1)
	r.HEAD("/v1", handleHeadV1)
}

// registerAPIRoutes 注册 API 路由组
func registerAPIRoutes(g *gin.RouterGroup) {
	if config.APIPassword != "" {
		g.Use(auth.APIPasswordMiddleware())
	}
	g.POST("/chat/completions", handleChatCompletions)
	g.GET("/models", handleModels)
	g.POST("/completions", handleCompletions)
	g.POST("/embeddings", handleEmbeddings)
	g.POST("/messages", handleAnthropicMessages)
	g.POST("/messages/count_tokens", handleCountTokens)
}

// optionalAuthMiddleware 当 API_PASSWORD 已设置时要求认证，否则放行
// 用于 / 等需要保护但不属于 /v1/* 路由组的端点
func optionalAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if config.APIPassword == "" {
			c.Next()
			return
		}
		authHeader := c.GetHeader("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			token := strings.TrimPrefix(authHeader, "Bearer ")
			if subtle.ConstantTimeCompare([]byte(token), []byte(config.APIPassword)) == 1 {
				c.Set("authenticated", true)
				c.Next()
				return
			}
		}
		// 未认证但密码已设置：放行但标记，handler 可据此过滤敏感信息
		c.Set("authenticated", false)
		c.Next()
	}
}

// ensureMinMessages 确保 payload 中至少有 2 条消息，不足则在前面添加系统消息
func ensureMinMessages(payload map[string]interface{}) {
	messages, _ := payload["messages"].([]interface{})
	if len(messages) < 2 {
		sysMsg := map[string]interface{}{"role": "system", "content": "You are a helpful assistant."}
		payload["messages"] = append([]interface{}{sysMsg}, messages...)
	}
}

// buildConversationHeaders 从客户端请求中提取对话头，用于覆盖上游生成的随机值
func buildConversationHeaders(c *gin.Context) map[string]string {
	headers := map[string]string{}
	if convID := c.GetHeader("X-Conversation-ID"); convID != "" {
		headers["X-Conversation-ID"] = convID
	}
	if msgID := c.GetHeader("X-Conversation-Message-ID"); msgID != "" {
		headers["X-Conversation-Message-ID"] = msgID
	}
	return headers
}

// handleChatCompletions POST /v1/chat/completions
func handleChatCompletions(c *gin.Context) {
	bearer := auth.GetBearerToken()
	if bearer == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{"message": "No token. Visit /auth/start to login.", "type": "auth_required"},
		})
		return
	}

	var body map[string]interface{}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "Invalid request body", "type": "invalid_request"},
		})
		return
	}

	isStream := false
	if v, ok := body["stream"].(bool); ok {
		isStream = v
	}
	model := "glm-5.1"
	if v, ok := body["model"].(string); ok {
		model = v
	}

	// 构建上游 payload（强制 stream: true）
	payload := map[string]interface{}{
		"model":    model,
		"messages": body["messages"],
		"stream":   true,
	}
	// 可选参数
	for _, k := range []string{"temperature", "max_tokens", "tools", "tool_choice", "stop", "stream_options"} {
		if v, ok := body[k]; ok {
			payload[k] = v
		}
	}

	// 确保至少 2 条消息
	ensureMinMessages(payload)

	// 生成追踪 ID 用于事件上报
	conversationID := "conv-" + randomHex(16)
	telemetryRequestID := "req-" + randomHex(16)
	traceID := randomHex(16)

	// 计算输入长度用于上报
	inputLength := 0
	if msgs, ok := body["messages"].([]interface{}); ok {
		for _, m := range msgs {
			if msg, ok := m.(map[string]interface{}); ok {
				if c, ok := msg["content"].(string); ok {
					inputLength += len(c)
				}
			}
		}
	}

	// 探活请求检测
	if maxTokens, ok := body["max_tokens"].(float64); ok && maxTokens == 1 && isStream {
		requestID := "chatcmpl-" + randomHex(12)
		c.JSON(http.StatusOK, gin.H{
			"id":      requestID,
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": []interface{}{
				gin.H{
					"index": 0,
					"message": gin.H{
						"role":    "assistant",
						"content": "ok",
					},
					"finish_reason": "stop",
				},
			},
			"usage": gin.H{
				"prompt_tokens":     0,
				"completion_tokens": 1,
				"total_tokens":      1,
			},
		})
		return
	}

	// 上报 chat_request_send 事件
	telemetry.ReportChatRequest(conversationID, telemetryRequestID, model, model, traceID, inputLength)

	// 对话头透传
	extraHeaders := buildConversationHeaders(c)

	if isStream {
		// 流式响应：直接用 SSE 转发
		StreamChatCompletions(c.Request.Context(), payload, model, bearer, c.Writer, conversationID, telemetryRequestID, traceID, extraHeaders)
	} else {
		// 非流式响应：收集所有 chunk 后组装
		result, err := CollectUpstreamChunks(c.Request.Context(), payload, bearer, extraHeaders)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": gin.H{"message": err.Error(), "type": "proxy_error"},
			})
			return
		}
		if result.StatusCode != 200 {
			c.JSON(result.StatusCode, gin.H{
				"error": gin.H{"message": result.ErrorText, "type": "upstream_error"},
			})
			return
		}

		// 上报 chat_message_response 事件
		telemetry.ReportChatResponse(conversationID, telemetryRequestID, model, model, traceID, result.PromptTokens, result.CompletionTokens)

		requestID := "chatcmpl-" + randomHex(12)
		content := strings.Join(result.ContentParts, "")
		msg := gin.H{"role": "assistant", "content": content}
		if content == "" {
			msg["content"] = nil
		}
		if len(result.ToolCalls) > 0 {
			msg["tool_calls"] = result.ToolCalls
		}

		c.JSON(http.StatusOK, gin.H{
			"id":      requestID,
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": []interface{}{
				gin.H{
					"index":         0,
					"message":       msg,
					"finish_reason": result.FinishReason,
				},
			},
			"usage": gin.H{
				"prompt_tokens":     result.PromptTokens,
				"completion_tokens": result.CompletionTokens,
				"total_tokens":      result.PromptTokens + result.CompletionTokens,
			},
		})
	}
}

// handleModels GET /v1/models
func handleModels(c *gin.Context) {
	models := FetchModels()
	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   models,
	})
}

// handleEmbeddings POST /v1/embeddings
func handleEmbeddings(c *gin.Context) {
	bearer := auth.GetBearerToken()
	if bearer == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{"message": "No token. Visit /auth/start to login.", "type": "auth_required"},
		})
		return
	}

	var body map[string]interface{}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "Invalid request body", "type": "invalid_request"},
		})
		return
	}

	model := "text-embedding-3-large"
	if v, ok := body["model"].(string); ok {
		model = v
	}

	inputVal, ok := body["input"]
	if !ok || inputVal == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "input is required", "type": "invalid_request"},
		})
		return
	}

	// 构建白名单 payload，避免转发客户端注入的字段（如 stream）
	payload := map[string]interface{}{
		"model": model,
		"input": inputVal,
	}
	for _, k := range []string{"encoding_format", "dimensions", "user"} {
		if v, ok := body[k]; ok {
			payload[k] = v
		}
	}

	extraHeaders := buildConversationHeaders(c)
	extraHeaders["Accept"] = "application/json"

	resp, err := doUpstreamRequest(c.Request.Context(), config.EmbeddingURL, payload, model, bearer, "embedding", extraHeaders)
	if err != nil {
		if ue, ok := err.(*upstreamError); ok {
			c.JSON(ue.StatusCode, gin.H{
				"error": gin.H{"message": ue.Message, "type": "upstream_error"},
			})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": gin.H{"message": err.Error(), "type": "proxy_error"},
			})
		}
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": "Failed to read upstream response", "type": "proxy_error"},
		})
		return
	}

	c.Data(http.StatusOK, "application/json", respBody)
}

// handleHealth GET /health
func handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"service": "uniview-codebuddy-proxy",
		"version": version.Version,
	})
}

// handleRoot GET /
func handleRoot(c *gin.Context) {
	result := gin.H{
		"service": "CodeBuddy CN -> OpenAI API Proxy",
		"version": version.Version,
		"endpoints": gin.H{
			"chat":        "POST /v1/chat/completions",
			"completions": "POST /v1/completions",
			"embeddings":  "POST /v1/embeddings",
			"messages":    "POST /v1/messages (Anthropic)",
			"models":      "GET /v1/models",
		},
	}

	// 仅在已认证时返回敏感信息
	if authed, ok := c.Get("authenticated"); ok {
		if isAuthed, _ := authed.(bool); isAuthed {
			result["upstream"] = config.ChatURL
			result["usage"] = gin.H{
				"base_url": fmt.Sprintf("http://localhost:%d/v1", config.Port),
			}
		}
	}

	c.JSON(http.StatusOK, result)
}

// handleHeadV1 HEAD /v1 — 连通性检查
func handleHeadV1(c *gin.Context) {
	c.Status(http.StatusOK)
}

// handleCountTokens POST /v1/messages/count_tokens — Anthropic token counting 兼容端点
// 上游不提供此 API，返回基于字符数的估算值
func handleCountTokens(c *gin.Context) {
	var body map[string]interface{}
	if err := c.ShouldBindJSON(&body); err != nil {
		anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}

	// 基于 rune 数估算 token 数（约 1.3 token/字符）
	charCount := countContentChars(body)

	// tools 定义也计入 token
	if tools, ok := body["tools"].([]interface{}); ok {
		for _, t := range tools {
			if tool, ok := t.(map[string]interface{}); ok {
				if name, ok := tool["name"].(string); ok {
					charCount += utf8.RuneCountInString(name)
				}
				if desc, ok := tool["description"].(string); ok {
					charCount += utf8.RuneCountInString(desc)
				}
			}
		}
	}

	tokens := charCount * 13 / 10 // ~1.3 token/char
	if tokens == 0 {
		tokens = 1
	}

	c.JSON(http.StatusOK, gin.H{
		"input_tokens":                tokens,
		"cache_creation_input_tokens": 0,
		"cache_read_input_tokens":     0,
	})
}

// countContentChars 统计 messages 和 system 中所有文本的 rune 数
func countContentChars(body map[string]interface{}) int {
	charCount := 0

	if msgs, ok := body["messages"].([]interface{}); ok {
		for _, m := range msgs {
			if msg, ok := m.(map[string]interface{}); ok {
				charCount += countMessageChars(msg)
			}
		}
	}
	if sys, ok := body["system"].(string); ok {
		charCount += utf8.RuneCountInString(sys)
	} else if sysBlocks, ok := body["system"].([]interface{}); ok {
		for _, block := range sysBlocks {
			charCount += countBlockChars(block)
		}
	}

	return charCount
}

// countMessageChars 统计单条 message 中 content 的 rune 数
func countMessageChars(msg map[string]interface{}) int {
	n := 0
	if content, ok := msg["content"].(string); ok {
		n += utf8.RuneCountInString(content)
	} else if contentArr, ok := msg["content"].([]interface{}); ok {
		for _, block := range contentArr {
			n += countBlockChars(block)
		}
	}
	return n
}

// countBlockChars 统计内容块中文本的 rune 数，覆盖 text/tool_result/tool_use/thinking
func countBlockChars(block interface{}) int {
	b, ok := block.(map[string]interface{})
	if !ok {
		return 0
	}
	n := 0
	switch b["type"] {
	case "text":
		if text, ok := b["text"].(string); ok {
			n += utf8.RuneCountInString(text)
		}
	case "tool_result":
		if content, ok := b["content"].(string); ok {
			n += utf8.RuneCountInString(content)
		} else if contentArr, ok := b["content"].([]interface{}); ok {
			for _, sub := range contentArr {
				n += countBlockChars(sub)
			}
		}
	case "tool_use":
		if name, ok := b["name"].(string); ok {
			n += utf8.RuneCountInString(name)
		}
		if input, ok := b["input"].(map[string]interface{}); ok {
			n += estimateMapChars(input)
		}
	case "thinking":
		if text, ok := b["thinking"].(string); ok {
			n += utf8.RuneCountInString(text)
		}
	default:
		// 兜底：提取所有字符串值
		if text, ok := b["text"].(string); ok {
			n += utf8.RuneCountInString(text)
		}
	}
	return n
}

// estimateMapChars 粗略估算 map 序列化后的字符数
func estimateMapChars(m map[string]interface{}) int {
	n := 0
	for k, v := range m {
		n += utf8.RuneCountInString(k)
		if s, ok := v.(string); ok {
			n += utf8.RuneCountInString(s)
		}
	}
	return n
}

// handleAnthropicMessages POST /v1/messages — Anthropic Messages API 兼容端点
func handleAnthropicMessages(c *gin.Context) {
	// 回传 anthropic-version 请求头
	if v := c.GetHeader("anthropic-version"); v != "" {
		c.Header("anthropic-version", v)
	}

	bearer := auth.GetBearerToken()
	if bearer == "" {
		anthropicErrorResponse(c, http.StatusUnauthorized, "authentication_error", "No token. Visit /auth/start to login.")
		return
	}

	var body map[string]interface{}
	if err := c.ShouldBindJSON(&body); err != nil {
		anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}

	model := "glm-5.1"
	if v, ok := body["model"].(string); ok {
		model = v
	}
	isStream := false
	if v, ok := body["stream"].(bool); ok {
		isStream = v
	}

	// 转换 messages
	system := body["system"]
	messages, _ := body["messages"].([]interface{})
	openaiMessages := convertAnthropicMessagesToOpenai(system, messages)

	// 构建上游 payload
	maxTokens := 4096
	if v, ok := body["max_tokens"].(float64); ok {
		maxTokens = int(v)
	}
	payload := map[string]interface{}{
		"model":      model,
		"messages":   openaiMessages,
		"stream":     true,
		"max_tokens": maxTokens,
	}
	if v, ok := body["temperature"]; ok {
		payload["temperature"] = v
	}
	if tools, ok := body["tools"].([]interface{}); ok {
		payload["tools"] = convertToolsAnthropicToOpenai(tools)
	}
	if v, ok := body["tool_choice"]; ok {
		payload["tool_choice"] = convertToolChoiceAnthropicToOpenai(v)
	}
	if ss, ok := body["stop_sequences"].([]interface{}); ok {
		payload["stop"] = ss
	}

	// 确保至少 2 条消息
	ensureMinMessages(payload)

	// 生成追踪 ID 用于事件上报
	anthropicConversationID := "conv-" + randomHex(16)
	anthropicRequestID := "req-" + randomHex(16)
	anthropicTraceID := randomHex(16)

	// 计算输入长度用于上报
	anthropicInputLength := 0
	if msgs, ok := body["messages"].([]interface{}); ok {
		for _, m := range msgs {
			if msg, ok := m.(map[string]interface{}); ok {
				if c, ok := msg["content"].(string); ok {
					anthropicInputLength += len(c)
				}
			}
		}
	}

	// 探活请求检测
	if maxTokens == 1 && isStream {
		msgID := "msg_" + randomHex(24)
		c.JSON(http.StatusOK, map[string]interface{}{
			"id":            msgID,
			"type":          "message",
			"role":          "assistant",
			"content":       []interface{}{map[string]interface{}{"type": "text", "text": "ok"}},
			"model":         model,
			"stop_reason":   "end_turn",
			"stop_sequence": nil,
			"usage":         map[string]interface{}{"input_tokens": 0, "output_tokens": 1},
		})
		return
	}

	// 上报 chat_request_send 事件
	telemetry.ReportChatRequest(anthropicConversationID, anthropicRequestID, model, model, anthropicTraceID, anthropicInputLength)

	// 对话头透传
	anthropicExtraHeaders := buildConversationHeaders(c)

	if isStream {
		StreamAnthropicMessages(c.Request.Context(), payload, model, bearer, c.Writer, anthropicConversationID, anthropicRequestID, anthropicTraceID, anthropicExtraHeaders)
	} else {
		// 非流式：检查缓存
		if config.CacheEnabledAtomic() && cache.GlobalCache.IsEnabled() {
			ck := cache.Key(model, payload["messages"], payload["tools"], 0)
			if cached := cache.GlobalCache.Get(ck); cached != nil {
				c.Data(http.StatusOK, "application/json", cached)
				return
			}
		}
		result, err := CollectUpstreamChunks(c.Request.Context(), payload, bearer, anthropicExtraHeaders)
		if err != nil {
			anthropicErrorResponse(c, http.StatusInternalServerError, "api_error", err.Error())
			return
		}
		if result.StatusCode != 200 {
			anthropicErrorResponse(c, result.StatusCode, "api_error", result.ErrorText)
			return
		}
		// 上报 chat_message_response 事件
		telemetry.ReportChatResponse(anthropicConversationID, anthropicRequestID, model, model, anthropicTraceID, result.PromptTokens, result.CompletionTokens)
		// 缓存响应
		if config.CacheEnabledAtomic() && cache.GlobalCache.IsEnabled() {
			buf := &bytes.Buffer{}
			cw := &cacheWriter{ResponseWriter: c.Writer, body: buf}
			c.Writer = cw
			convertOpenAIToAnthropicResponse(result, model, payload, c)
			cache.GlobalCache.Set(cache.Key(model, payload["messages"], payload["tools"], 0), buf.Bytes())
			return
		}
		convertOpenAIToAnthropicResponse(result, model, payload, c)
	}
}
