package proxy

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"uniview-codebuddy-proxy/internal/auth"
	"uniview-codebuddy-proxy/internal/config"
	"uniview-codebuddy-proxy/internal/version"

	"github.com/gin-gonic/gin"
)

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
	g.POST("/messages", handleAnthropicMessages)
	g.POST("/messages/count_tokens", handleCountTokens)
	g.POST("/responses", handleResponses)
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
	model := "auto-chat"
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

	if isStream {
		// 流式响应：直接用 SSE 转发
		StreamChatCompletions(c.Request.Context(), payload, model, bearer, c.Writer)
	} else {
		// 非流式响应：收集所有 chunk 后组装
		result, err := CollectUpstreamChunks(c.Request.Context(), payload, bearer)
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
			"chat":      "POST /v1/chat/completions",
			"messages":  "POST /v1/messages (Anthropic)",
			"responses": "POST /v1/responses",
			"models":    "GET /v1/models",
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
		"input_tokens": tokens,
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

	model := "deepseek-v3"
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

	if isStream {
		StreamAnthropicMessages(c.Request.Context(), payload, model, bearer, c.Writer)
	} else {
		result, err := CollectUpstreamChunks(c.Request.Context(), payload, bearer)
		if err != nil {
			anthropicErrorResponse(c, http.StatusInternalServerError, "api_error", err.Error())
			return
		}
		if result.StatusCode != 200 {
			anthropicErrorResponse(c, result.StatusCode, "api_error", result.ErrorText)
			return
		}
		convertOpenAIToAnthropicResponse(result, model, c)
	}
}

// handleResponses POST /v1/responses — OpenAI Responses API 兼容端点
func handleResponses(c *gin.Context) {
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

	model := "deepseek-v3"
	if v, ok := body["model"].(string); ok {
		model = v
	}
	isStream := false
	if v, ok := body["stream"].(bool); ok {
		isStream = v
	}

	// 转换 input → messages
	messages := convertResponsesToChat(body)

	// 构建上游 payload
	payload := map[string]interface{}{
		"model":    model,
		"messages": messages,
		"stream":   true,
	}
	if v, ok := body["max_output_tokens"]; ok {
		payload["max_tokens"] = v
	}
	if v, ok := body["temperature"]; ok {
		payload["temperature"] = v
	}
	if v, ok := body["tools"]; ok {
		payload["tools"] = v
	}
	if v, ok := body["tool_choice"]; ok {
		payload["tool_choice"] = v
	}

	// 确保至少 2 条消息
	ensureMinMessages(payload)

	requestID := "resp_" + randomHex(24)

	if isStream {
		StreamResponsesSSE(c.Request.Context(), payload, model, bearer, c.Writer)
	} else {
		result, err := CollectUpstreamChunks(c.Request.Context(), payload, bearer)
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
		convertChatToResponsesResult(result, model, requestID, c)
	}
}
