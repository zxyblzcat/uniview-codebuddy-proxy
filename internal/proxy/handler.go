package proxy

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"codebuddy-proxy/internal/auth"
	"codebuddy-proxy/internal/config"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes 注册所有 /v1/* 路由和健康检查路由
func RegisterRoutes(r *gin.Engine) {
	// API Key 认证中间件
	v1 := r.Group("/v1")
	v1.Use(apiKeyAuthMiddleware())
	{
		v1.POST("/chat/completions", handleChatCompletions)
		v1.GET("/models", handleModels)
		v1.POST("/messages", handleAnthropicMessages)
	}

	// 健康检查（无需认证）
	r.GET("/health", handleHealth)
	r.GET("/", handleRoot)
	r.HEAD("/v1", handleHeadV1)
}

// apiKeyAuthMiddleware API Key 认证中间件
// 支持 Authorization: Bearer xxx 和 x-api-key: xxx
func apiKeyAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if config.APIPassword == "" {
			c.Next()
			return
		}

		var key string
		authHeader := c.GetHeader("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			key = authHeader[7:]
		} else {
			key = c.GetHeader("x-api-key")
		}

		if key == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"message": "Missing Authorization header or x-api-key", "type": "auth_required"},
			})
			return
		}
		if key != config.APIPassword {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": gin.H{"message": "Invalid API key", "type": "forbidden"},
			})
			return
		}
		c.Next()
	}
}

// handleChatCompletions POST /v1/chat/completions
func handleChatCompletions(c *gin.Context) {
	bearer := auth.GetBearerToken()
	if bearer == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{"message": "No token. Visit /auth/start to login first.", "type": "auth_required"},
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
	for _, k := range []string{"temperature", "max_tokens", "tools", "tool_choice"} {
		if v, ok := body[k]; ok {
			payload[k] = v
		}
	}

	// 确保至少 2 条消息
	messages, _ := payload["messages"].([]interface{})
	if len(messages) < 2 {
		sysMsg := map[string]interface{}{"role": "system", "content": "You are a helpful assistant."}
		payload["messages"] = append([]interface{}{sysMsg}, messages...)
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

	if isStream {
		// 流式响应：直接用 SSE 转发
		StreamChatCompletions(payload, model, c.Writer)
	} else {
		// 非流式响应：收集所有 chunk 后组装
		result, err := CollectUpstreamChunks(payload)
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
		"service": "codebuddy-proxy",
		"version": "3.0.0",
	})
}

// handleRoot GET /
func handleRoot(c *gin.Context) {
	hasToken := auth.LoadToken() != nil
	c.JSON(http.StatusOK, gin.H{
		"service":   "CodeBuddy CN -> OpenAI API Proxy",
		"version":   "3.0.0",
		"upstream":  config.ChatURL,
		"auth":      "OAuth2 Device Flow",
		"has_token": hasToken,
		"endpoints": gin.H{
			"auth_start":  "GET /auth/start",
			"auth_poll":   "GET /auth/poll?auth_state=xxx",
			"auth_manual": "POST /auth/manual  (set bearer token directly)",
			"auth_status": "GET /auth/status",
			"chat":        "POST /v1/chat/completions",
			"messages":    "POST /v1/messages (Anthropic)",
			"models":      "GET /v1/models",
		},
		"usage": gin.H{
			"base_url": fmt.Sprintf("http://localhost:%d/v1", config.Port),
		},
	})
}

// handleHeadV1 HEAD /v1 — 连通性检查
func handleHeadV1(c *gin.Context) {
	c.Status(http.StatusOK)
}

// handleAnthropicMessages POST /v1/messages — Anthropic Messages API 兼容端点
func handleAnthropicMessages(c *gin.Context) {
	bearer := auth.GetBearerToken()
	if bearer == "" {
		anthropicErrorResponse(c, http.StatusUnauthorized, "authentication_error", "No token. Visit /auth/start to login first.")
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

	// 确保至少 2 条消息
	msgs, _ := payload["messages"].([]interface{})
	if len(msgs) < 2 {
		sysMsg := map[string]interface{}{"role": "system", "content": "You are a helpful assistant."}
		payload["messages"] = append([]interface{}{sysMsg}, msgs...)
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

	if isStream {
		StreamAnthropicMessages(payload, model, c.Writer)
	} else {
		result, err := CollectUpstreamChunks(payload)
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
