package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"uniview-codebuddy-proxy/internal/auth"
	"uniview-codebuddy-proxy/internal/config"
	"uniview-codebuddy-proxy/internal/telemetry"

	"github.com/gin-gonic/gin"
)

// handleCompletions POST /v1/completions
func handleCompletions(c *gin.Context) {
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

	model := "deepseek-v3-1-lkeap"
	if v, ok := body["model"].(string); ok {
		model = v
	}

	isStream := false
	if v, ok := body["stream"].(bool); ok {
		isStream = v
	}

	promptVal, ok := body["prompt"]
	if !ok || promptVal == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "prompt is required", "type": "invalid_request"},
		})
		return
	}

	// 构建上游 payload（强制 stream: true）
	payload := map[string]interface{}{
		"model":  model,
		"prompt": promptVal,
		"stream": true,
	}
	// 可选参数透传
	for _, k := range []string{"suffix", "temperature", "max_tokens", "top_p", "n", "stop"} {
		if v, ok := body[k]; ok {
			payload[k] = v
		}
	}

	// 构建 extraHeaders：X-API-Version + 对话头透传
	extraHeaders := buildConversationHeaders(c)
	extraHeaders["X-API-Version"] = "v2"

	// 遥测追踪
	conversationID := "conv-" + randomHex(16)
	traceID := randomHex(16)

	telemetry.ReportCompletionTrigger(conversationID, model, model, traceID)

	if isStream {
		StreamCompletions(c.Request.Context(), payload, model, bearer, c.Writer, extraHeaders, conversationID, traceID)
	} else {
		result, err := CollectCompletionChunks(c.Request.Context(), payload, model, bearer, extraHeaders)
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
		if result.StatusCode != 200 {
			c.JSON(result.StatusCode, gin.H{
				"error": gin.H{"message": result.ErrorText, "type": "upstream_error"},
			})
			return
		}

		telemetry.ReportCompletionResponse(conversationID, model, model, traceID, result.PromptTokens, result.CompletionTokens)

		requestID := "cmpl-" + randomHex(12)
		text := strings.Join(result.TextParts, "")

		c.JSON(http.StatusOK, gin.H{
			"id":      requestID,
			"object":  "text_completion",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": []interface{}{
				gin.H{
					"text":          text,
					"index":         0,
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

// CompletionResult 收集非流式补全的结果
type CompletionResult struct {
	StatusCode       int
	ErrorText        string
	TextParts        []string
	FinishReason     string
	PromptTokens     int
	CompletionTokens int
}

// StreamCompletions 流式转发代码补全 SSE
func StreamCompletions(ctx context.Context, payload map[string]interface{}, model string, bearer string, w http.ResponseWriter, extraHeaders map[string]string, conversationID, traceID string) {
	requestID := "cmpl-" + randomHex(12)

	resp, err := doUpstreamRequest(ctx, config.CompletionURL, payload, model, bearer, "CodeCompletion", extraHeaders)
	if err != nil {
		if ue, ok := err.(*upstreamError); ok {
			writeSSEError(w, ue.Error())
		} else {
			writeSSEError(w, err.Error())
		}
		return
	}
	defer resp.Body.Close()
	body := wrapWithIdleTimeout(resp.Body)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, canFlush := w.(http.Flusher)

	var writeErr error
	var promptTokens, completionTokens int

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if writeErr != nil {
			return
		}

		line := scanner.Text()
		if line == "" {
			continue
		}

		dataStr, done, ok := parseSSELine(line)
		if !ok {
			continue
		}

		if done {
			telemetry.ReportCompletionResponse(conversationID, model, model, traceID, promptTokens, completionTokens)
			_, writeErr = fmt.Fprintf(w, "data: [DONE]\n\n")
			if canFlush {
				flusher.Flush()
			}
			break
		}

		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
			continue
		}

		// 提取 usage
		if u, ok := chunk["usage"].(map[string]interface{}); ok {
			if pt, ok := u["prompt_tokens"].(float64); ok {
				promptTokens = int(pt)
			}
			if ct, ok := u["completion_tokens"].(float64); ok {
				completionTokens = int(ct)
			}
		}

		if _, ok := chunk["choices"]; ok {
			chunk["model"] = model
			chunk["id"] = requestID
			chunk["object"] = "text_completion"
			cleanCompletionChunkChoices(chunk)
		}

		cleaned, err := json.Marshal(chunk)
		if err != nil {
			continue
		}
		_, writeErr = fmt.Fprintf(w, "data: %s\n\n", string(cleaned))
		if canFlush {
			flusher.Flush()
		}
	}
	if err := scanner.Err(); err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			log.Printf("SSE scan error in StreamCompletions: %v", err)
		}
	}
}

// CollectCompletionChunks 收集所有补全 SSE chunk 后组装非流式响应
func CollectCompletionChunks(ctx context.Context, payload map[string]interface{}, model string, bearer string, extraHeaders map[string]string) (*CompletionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	resp, err := doUpstreamRequest(ctx, config.CompletionURL, payload, model, bearer, "CodeCompletion", extraHeaders)
	if err != nil {
		if ue, ok := err.(*upstreamError); ok {
			return &CompletionResult{StatusCode: ue.StatusCode, ErrorText: ue.Message}, nil
		}
		return nil, err
	}
	defer resp.Body.Close()
	body := wrapWithIdleTimeout(resp.Body)

	result := &CompletionResult{StatusCode: 200}

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		dataStr, done, ok := parseSSELine(line)
		if !ok || done {
			continue
		}

		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
			continue
		}

		choices, _ := chunk["choices"].([]interface{})
		for _, ch := range choices {
			choice, _ := ch.(map[string]interface{})
			if choice == nil {
				continue
			}
			// 补全格式使用 "text" 而非 "delta"
			if t, ok := choice["text"].(string); ok && t != "" {
				result.TextParts = append(result.TextParts, t)
			}
			if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
				result.FinishReason = fr
			}
		}

		if u, ok := chunk["usage"].(map[string]interface{}); ok {
			if pt, ok := u["prompt_tokens"].(float64); ok {
				result.PromptTokens = int(pt)
			}
			if ct, ok := u["completion_tokens"].(float64); ok {
				result.CompletionTokens = int(ct)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			// 标记为截断响应，避免客户端静默使用不完整的补全
			result.StatusCode = 502
			result.ErrorText = fmt.Sprintf("upstream stream truncated: %v", err)
			return result, nil
		}
		log.Printf("SSE scan error in CollectCompletionChunks: %v", err)
		result.StatusCode = 502
		result.ErrorText = fmt.Sprintf("upstream stream error: %v", err)
	}

	return result, nil
}

// cleanCompletionChunkChoices 剥离非标准字段，仅保留 OpenAI completions 格式
func cleanCompletionChunkChoices(chunk map[string]interface{}) {
	choices, _ := chunk["choices"].([]interface{})
	for _, ch := range choices {
		choice, _ := ch.(map[string]interface{})
		if choice == nil {
			continue
		}
		for key := range choice {
			if key != "index" && key != "text" && key != "finish_reason" {
				delete(choice, key)
			}
		}
		// 规范化 finish_reason
		if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
			switch fr {
			case "stop", "length", "content_filter":
			default:
				choice["finish_reason"] = "stop"
			}
		}
	}
}
