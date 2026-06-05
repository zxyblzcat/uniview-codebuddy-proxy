package proxy

import (
	"bytes"
	"crypto/subtle"
	"fmt"
	"io"
	"log"
	"net/http"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"uniview-codebuddy-proxy/internal/auth"
	"uniview-codebuddy-proxy/internal/cache"
	"uniview-codebuddy-proxy/internal/config"
	"uniview-codebuddy-proxy/internal/telemetry"
	"uniview-codebuddy-proxy/internal/version"

	"github.com/gin-gonic/gin"
)

// activeRequests 跟踪当前正在处理的并发请求数
var activeRequests atomic.Int64

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
	g.GET("/models/:id", HandleModelByID)
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
		errMsg := "Invalid request body"
		if strings.Contains(err.Error(), "http: request body too large") {
			errMsg = "请求体过大（超过限制），请减少输入内容后重试"
		}
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": errMsg, "type": "invalid_request"},
		})
		return
	}

	// 检测 messages 中是否包含 image_url 类型的内容（上游不支持 vision 输入）
	if hasImageURLContent(body) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "上游 API 不支持图片输入（image_url），请移除图片后重试", "type": "invalid_request"},
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
	for _, k := range []string{"temperature", "max_tokens", "tools", "stop"} {
		if v, ok := body[k]; ok {
			payload[k] = v
		}
	}
	// tool_choice 需要规范化：上游只接受 string 类型，不接受对象形式
	if v, ok := body["tool_choice"]; ok {
		payload["tool_choice"] = sanitizeToolChoiceOpenai(v)
	}

	// 强制请求上游在流式响应中返回 usage 信息（即使客户端传了也覆盖）
	payload["stream_options"] = map[string]interface{}{"include_usage": true}

	// 确保至少 2 条消息
	ensureMinMessages(payload)

	// 生成追踪 ID 用于事件上报
	conversationID := "conv-" + randomHex(16)
	telemetryRequestID := "req-" + randomHex(16)
	traceID := randomHex(16)

	// 遥测上报的 input_length：不再使用本地估算，留待上游返回真实值后在上报时使用

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

	// 调试模式：请求计时（在入口处捕获一次 debug 状态，避免运行中切换导致计时不一致）
	var debugStartTime time.Time
	var debugUpstreamStart time.Time
	var debugRequestID string
	debugEnabled := config.DebugEnabledAtomic()
	if debugEnabled {
		debugStartTime = time.Now()
		debugRequestID = "dbg-" + randomHex(8)
		activeRequests.Add(1)
		defer activeRequests.Add(-1)
	}

	// 上报 chat_request_send 事件
	telemetry.ReportChatRequest(conversationID, telemetryRequestID, model, model, traceID, 0)

	// 对话头透传
	extraHeaders := buildConversationHeaders(c)

	if isStream {
		// 流式响应：直接用 SSE 转发
		var ttfb time.Duration
		if debugEnabled {
			debugUpstreamStart = time.Now()
		}
		ttfb = StreamChatCompletions(c.Request.Context(), payload, model, bearer, c.Writer, conversationID, telemetryRequestID, traceID, extraHeaders)
		if debugEnabled {
			totalStreamDuration := time.Since(debugUpstreamStart) // 整个流传输时间（含 TTFB）
			total := time.Since(debugStartTime)
			streamingDuration := totalStreamDuration - ttfb
			if streamingDuration < 0 {
				streamingDuration = 0
			}
			proxyOverhead := total - totalStreamDuration
			if proxyOverhead < 0 {
				proxyOverhead = 0
			}
			log.Printf("[DEBUG] request_id=%s format=openai model=%s stream=true upstream_ttfb=%s upstream_streaming=%s proxy_overhead=%s total=%s active_requests=%d goroutines=%d",
				debugRequestID, model, fmtDur(ttfb), fmtDur(streamingDuration), fmtDur(proxyOverhead), fmtDur(total), activeRequests.Load(), runtime.NumGoroutine())
		}
	} else {
		// 非流式响应：收集所有 chunk 后组装
		if debugEnabled {
			debugUpstreamStart = time.Now()
		}
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

		if debugEnabled {
			upstreamDuration := time.Since(debugUpstreamStart)
			total := time.Since(debugStartTime)
			proxyOverhead := total - upstreamDuration
			if proxyOverhead < 0 {
				proxyOverhead = 0
			}
			log.Printf("[DEBUG] request_id=%s format=openai model=%s stream=false upstream_ttfb=%s upstream_streaming=0s proxy_overhead=%s total=%s active_requests=%d goroutines=%d",
				debugRequestID, model, fmtDur(upstreamDuration), fmtDur(proxyOverhead), fmtDur(total), activeRequests.Load(), runtime.NumGoroutine())
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
				"prompt_tokens_details": gin.H{
					"cached_tokens": result.CachedTokens,
				},
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
		errMsg := "Invalid request body"
		if strings.Contains(err.Error(), "http: request body too large") {
			errMsg = "请求体过大（超过限制），请减少输入内容后重试"
		}
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": errMsg, "type": "invalid_request"},
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
			"model_info":  "GET /v1/models/:id (Anthropic)",
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
// 上游不提供真实的 token 计数 API，使用消息内容长度估算
// 返回 0 会导致 Claude Code autocompact 永不触发
func handleCountTokens(c *gin.Context) {
	var body map[string]interface{}
	if err := c.ShouldBindJSON(&body); err != nil {
		errMsg := "Invalid request body"
		if strings.Contains(err.Error(), "http: request body too large") {
			errMsg = "请求体过大（超过限制），请减少输入内容后重试"
		}
		anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", errMsg)
		return
	}

	// 不再使用本地估算，返回 0
	// 真实的 token 计数由上游 glm-5.1 tokenizer 提供，
	// 在实际的 /v1/messages 请求响应中通过 usage.input_tokens 返回
	c.JSON(http.StatusOK, gin.H{
		"input_tokens":                0,
		"cache_creation_input_tokens": 0,
		"cache_read_input_tokens":     0,
	})
}

// handleAnthropicMessages POST /v1/messages — Anthropic Messages API 兼容端点
func handleAnthropicMessages(c *gin.Context) {
	// 回传 anthropic-version 请求头
	if v := c.GetHeader("anthropic-version"); v != "" {
		c.Header("anthropic-version", v)
	}

	// 回传 anthropic-beta 请求头
	// Claude Code 通过此头声明启用的 beta 特性（如 extended-thinking、prompt-caching），
	// 代理需要将其回传给客户端，让 Claude Code 知道这些特性已被接受
	if v := c.GetHeader("anthropic-beta"); v != "" {
		c.Header("anthropic-beta", v)
	}

	bearer := auth.GetBearerToken()
	if bearer == "" {
		anthropicErrorResponse(c, http.StatusUnauthorized, "authentication_error", "No token. Visit /auth/start to login.")
		return
	}

	var body map[string]interface{}
	if err := c.ShouldBindJSON(&body); err != nil {
		errMsg := "Invalid request body"
		if strings.Contains(err.Error(), "http: request body too large") {
			errMsg = "请求体过大（超过限制），请减少输入内容后重试"
		}
		anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", errMsg)
		return
	}

	// 检测是否包含 image_url（上游不支持 vision 输入）
	if hasImageURLContent(body) {
		anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "上游 API 不支持图片输入（image_url），请移除图片后重试")
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

	// 处理 thinking 配置块：Claude Code 在启用 extended thinking 时发送此字段
	// 上游 CodeBuddy API 不支持 Anthropic 的 thinking 参数，静默忽略即可
	// 上游模型（如 deepseek-r1）如果自身支持 reasoning，会自动返回 reasoning_content，
	// 代理在流式转换中已将其映射为 Anthropic 的 thinking content block

	// 强制请求上游在流式响应中返回 usage 信息（即使客户端传了也覆盖）
	// input_tokens 在 message_start 事件中返回，Claude Code 依赖此值判断上下文占用率
	payload["stream_options"] = map[string]interface{}{"include_usage": true}

	// 确保至少 2 条消息
	ensureMinMessages(payload)

	// 生成追踪 ID 用于事件上报
	anthropicConversationID := "conv-" + randomHex(16)
	anthropicRequestID := "req-" + randomHex(16)
	anthropicTraceID := randomHex(16)

	// 遥测上报的 input_length：不再使用本地估算，留待上游返回真实值后在上报时使用

	// 探活请求检测
	if maxTokens == 1 && isStream {
		msgID := "msg_" + randomHex(24)
		// 探活请求不经过上游，input_tokens 返回 0
		// 真实的 token 计数在实际的 /v1/messages 请求响应中通过 usage.input_tokens 返回
		writeAnthropicProbeSSE(c, msgID, model, 0)
		return
	}

	// 调试模式：请求计时（在入口处捕获一次 debug 状态，避免运行中切换导致计时不一致）
	var debugStartTime time.Time
	var debugUpstreamStart time.Time
	var debugRequestID string
	debugEnabled := config.DebugEnabledAtomic()
	if debugEnabled {
		debugStartTime = time.Now()
		debugRequestID = "dbg-" + randomHex(8)
		activeRequests.Add(1)
		defer activeRequests.Add(-1)
	}

	// 上报 chat_request_send 事件
	telemetry.ReportChatRequest(anthropicConversationID, anthropicRequestID, model, model, anthropicTraceID, 0)

	// 对话头透传
	anthropicExtraHeaders := buildConversationHeaders(c)

	if isStream {
		var ttfb time.Duration
		if debugEnabled {
			debugUpstreamStart = time.Now()
		}
		ttfb = StreamAnthropicMessages(c.Request.Context(), payload, model, bearer, c.Writer, anthropicConversationID, anthropicRequestID, anthropicTraceID, anthropicExtraHeaders)
		if debugEnabled {
			totalStreamDuration := time.Since(debugUpstreamStart) // 整个流传输时间（含 TTFB）
			total := time.Since(debugStartTime)
			streamingDuration := totalStreamDuration - ttfb
			if streamingDuration < 0 {
				streamingDuration = 0
			}
			proxyOverhead := total - totalStreamDuration
			if proxyOverhead < 0 {
				proxyOverhead = 0
			}
			log.Printf("[DEBUG] request_id=%s format=anthropic model=%s stream=true upstream_ttfb=%s upstream_streaming=%s proxy_overhead=%s total=%s active_requests=%d goroutines=%d",
				debugRequestID, model, fmtDur(ttfb), fmtDur(streamingDuration), fmtDur(proxyOverhead), fmtDur(total), activeRequests.Load(), runtime.NumGoroutine())
		}
	} else {
		// 计算缓存 key（lookup 和 store 共用，避免重复计算）
		cacheTemp := 0.0
		if v, ok := payload["temperature"].(float64); ok {
			cacheTemp = v
		}
		ck := cache.Key(model, payload["messages"], payload["tools"], cacheTemp, maxTokens)

		// 非流式：检查缓存
		if config.CacheEnabledAtomic() && cache.GlobalCache.IsEnabled() {
			if cached := cache.GlobalCache.Get(ck); cached != nil {
				c.Data(http.StatusOK, "application/json", cached)
				return
			}
		}
		if debugEnabled {
			debugUpstreamStart = time.Now()
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
		if debugEnabled {
			upstreamDuration := time.Since(debugUpstreamStart)
			total := time.Since(debugStartTime)
			proxyOverhead := total - upstreamDuration
			if proxyOverhead < 0 {
				proxyOverhead = 0
			}
			log.Printf("[DEBUG] request_id=%s format=anthropic model=%s stream=false upstream_ttfb=%s upstream_streaming=0s proxy_overhead=%s total=%s active_requests=%d goroutines=%d",
				debugRequestID, model, fmtDur(upstreamDuration), fmtDur(proxyOverhead), fmtDur(total), activeRequests.Load(), runtime.NumGoroutine())
		}
		// 上报 chat_message_response 事件
		telemetry.ReportChatResponse(anthropicConversationID, anthropicRequestID, model, model, anthropicTraceID, result.PromptTokens, result.CompletionTokens)
		// 缓存响应
		if config.CacheEnabledAtomic() && cache.GlobalCache.IsEnabled() {
			buf := &bytes.Buffer{}
			cw := &cacheWriter{ResponseWriter: c.Writer, body: buf}
			c.Writer = cw
			convertOpenAIToAnthropicResponse(result, model, payload, c)
			cache.GlobalCache.Set(ck, buf.Bytes())
			return
		}
		convertOpenAIToAnthropicResponse(result, model, payload, c)
	}
}

// fmtDur 格式化 time.Duration 为秒（保留2位小数）
func fmtDur(d time.Duration) string {
	return fmt.Sprintf("%.2fs", d.Seconds())
}

// hasImageURLContent 检测 messages 和 system 中是否包含图片类型的内容
// 同时检测 OpenAI 格式 (type: "image_url") 和 Anthropic 格式 (source.type: "base64")
// 注意：Anthropic API 允许 system 字段为数组形式的内容块，其中可能包含图片
func hasImageURLContent(body map[string]interface{}) bool {
	// 检查 messages 中的图片内容
	messages, _ := body["messages"].([]interface{})
	for _, msg := range messages {
		m, _ := msg.(map[string]interface{})
		if m == nil {
			continue
		}
		if hasImageInContent(m["content"]) {
			return true
		}
	}

	// 检查 system 字段中的图片内容（Anthropic 格式允许 system 为数组）
	if sys, ok := body["system"]; ok {
		if hasImageInContent(sys) {
			return true
		}
	}

	return false
}

// hasImageInContent 检测内容中是否包含图片类型
// content 可以是字符串（无图片）、[]interface{} 数组或单个 map 对象
func hasImageInContent(content interface{}) bool {
	switch c := content.(type) {
	case []interface{}:
		for _, item := range c {
			part, _ := item.(map[string]interface{})
			if part == nil {
				continue
			}
			// OpenAI 格式: {"type": "image_url", ...}
			if typ, _ := part["type"].(string); typ == "image_url" {
				return true
			}
			// Anthropic 格式: {"type": "image", "source": {"type": "base64"/"url", ...}}
			if typ, _ := part["type"].(string); typ == "image" {
				if src, _ := part["source"].(map[string]interface{}); src != nil {
					if srcType, _ := src["type"].(string); srcType == "base64" || srcType == "url" {
						return true
					}
				}
			}
		}
	case map[string]interface{}:
		// 单个对象也可能是图片内容块
		if typ, _ := c["type"].(string); typ == "image" {
			if src, _ := c["source"].(map[string]interface{}); src != nil {
				if srcType, _ := src["type"].(string); srcType == "base64" || srcType == "url" {
					return true
				}
			}
		}
	}
	return false
}

// writeAnthropicProbeSSE 返回 Anthropic SSE 格式的探活响应
// 探活请求（max_tokens=1, stream=true）的客户端期望 SSE 格式而非 JSON
// 使用 anthropicSSE 辅助函数构建事件，确保与流式路径的 SSE 格式一致
func writeAnthropicProbeSSE(c *gin.Context, msgID, model string, inputTokens int) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(http.StatusOK)

	w := c.Writer

	// message_start
	fmt.Fprint(w, anthropicSSE("message_start", map[string]interface{}{
		"type": "message_start",
		"message": map[string]interface{}{
			"id":            msgID,
			"type":          "message",
			"role":          "assistant",
			"content":       []interface{}{},
			"model":         model,
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         map[string]interface{}{"input_tokens": inputTokens, "output_tokens": 1, "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0},
		},
	}))

	// content_block_start (text block)
	fmt.Fprint(w, anthropicSSE("content_block_start", map[string]interface{}{
		"type":          "content_block_start",
		"index":         0,
		"content_block": map[string]interface{}{"type": "text", "text": ""},
	}))

	// content_block_delta
	fmt.Fprint(w, anthropicSSE("content_block_delta", map[string]interface{}{
		"type":  "content_block_delta",
		"index": 0,
		"delta": map[string]interface{}{"type": "text_delta", "text": "ok"},
	}))

	// content_block_stop
	fmt.Fprint(w, anthropicSSE("content_block_stop", map[string]interface{}{
		"type":  "content_block_stop",
		"index": 0,
	}))

	// message_delta
	fmt.Fprint(w, anthropicSSE("message_delta", map[string]interface{}{
		"type":  "message_delta",
		"delta": map[string]interface{}{"stop_reason": "end_turn", "stop_sequence": nil},
		"usage": map[string]interface{}{"output_tokens": 1},
	}))

	// message_stop
	fmt.Fprint(w, anthropicSSE("message_stop", map[string]interface{}{
		"type": "message_stop",
	}))

	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}