package proxy

import (
	"encoding/json"
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

// Responses API 数据结构

// ResponsesRequest 是 /v1/responses 的请求体
type ResponsesRequest struct {
	Model             string          `json:"model"`
	Input             json.RawMessage `json:"input"`
	Instructions      string          `json:"instructions,omitempty"`
	Tools             []interface{}   `json:"tools,omitempty"`
	Stream            bool            `json:"stream,omitempty"`
	Temperature       *float64        `json:"temperature,omitempty"`
	MaxOutputTokens   int             `json:"max_output_tokens,omitempty"`
	PreviousResponseID string        `json:"previous_response_id,omitempty"`
	Reasoning         *ResponsesReasoning `json:"reasoning,omitempty"`
	TopP              *float64        `json:"top_p,omitempty"`
	Metadata          interface{}     `json:"metadata,omitempty"`
}

// ResponsesReasoning 是 reasoning 配置
type ResponsesReasoning struct {
	Effort          string `json:"effort,omitempty"`
	GenerateSummary string `json:"generate_summary,omitempty"`
}

// ResponsesAPIResponse 是 /v1/responses 的非流式响应
type ResponsesAPIResponse struct {
	ID                 string             `json:"id"`
	Object             string             `json:"object"`
	CreatedAt          int64              `json:"created_at"`
	Model              string             `json:"model"`
	Status             string             `json:"status"`
	Output             []ResponsesOutputItem `json:"output"`
	Usage              ResponsesUsage     `json:"usage"`
	Instructions       interface{}        `json:"instructions,omitempty"`
	Temperature        interface{}        `json:"temperature,omitempty"`
	TopP               interface{}        `json:"top_p,omitempty"`
	MaxOutputTokens    interface{}        `json:"max_output_tokens,omitempty"`
	Tools              interface{}        `json:"tools,omitempty"`
	PreviousResponseID interface{}       `json:"previous_response_id,omitempty"`
	Reasoning          interface{}       `json:"reasoning,omitempty"`
	Metadata           interface{}       `json:"metadata,omitempty"`
}

// ResponsesOutputItem 是响应中的输出项
type ResponsesOutputItem struct {
	ID      string                `json:"id,omitempty"`
	Type    string                `json:"type"`
	Role    string                `json:"role,omitempty"`
	Status  string                `json:"status,omitempty"`
	Content []ResponsesContentItem `json:"content,omitempty"`
	// function_call 类型
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	CallID    string `json:"call_id,omitempty"`
}

// ResponsesContentItem 是 message 输出项中的内容项
type ResponsesContentItem struct {
	Type        string      `json:"type"`
	Text        string      `json:"text,omitempty"`
	Annotations []interface{} `json:"annotations,omitempty"`
}

// ResponsesUsage 是响应的用量信息
type ResponsesUsage struct {
	InputTokens         int                    `json:"input_tokens"`
	OutputTokens        int                    `json:"output_tokens"`
	TotalTokens         int                    `json:"total_tokens"`
	InputTokensDetails  map[string]interface{} `json:"input_tokens_details,omitempty"`
	OutputTokensDetails map[string]interface{} `json:"output_tokens_details,omitempty"`
}

// inputMessage 是 input 数组中的消息项
type inputMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// convertResponsesInputToMessages 将 Responses API 的 input 转换为 Chat Completions messages
// 支持 message、function_call、function_call_output 三种输入项类型
func convertResponsesInputToMessages(req *ResponsesRequest) ([]interface{}, string) {
	var messages []interface{}
	var systemContent string

	// instructions → system message
	if req.Instructions != "" {
		systemContent = req.Instructions
	}

	// 解析 input
	var inputRaw json.RawMessage = req.Input
	if len(inputRaw) == 0 {
		return messages, systemContent
	}

	// 尝试解析为字符串
	var inputStr string
	if err := json.Unmarshal(inputRaw, &inputStr); err == nil {
		// input 是简单字符串 → 单条 user 消息
		msg := map[string]interface{}{
			"role":    "user",
			"content": inputStr,
		}
		messages = append(messages, msg)
		return messages, systemContent
	}

	// 尝试解析为通用对象数组（支持 message/function_call/function_call_output）
	var inputItems []map[string]interface{}
	if err := json.Unmarshal(inputRaw, &inputItems); err == nil {
		// 收集连续的 function_call 项，合并到同一个 assistant 消息的 tool_calls 中
		var pendingToolCalls []interface{}
		for _, item := range inputItems {
			itemType, _ := item["type"].(string)
			switch itemType {
			case "function_call":
				// function_call → 收集为 tool_calls 项
				callID, _ := item["call_id"].(string)
				name, _ := item["name"].(string)
				arguments, _ := item["arguments"].(string)
				if callID == "" {
					callID, _ = item["id"].(string)
				}
				toolCall := map[string]interface{}{
					"id":   callID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      name,
						"arguments": arguments,
					},
				}
				pendingToolCalls = append(pendingToolCalls, toolCall)

			case "function_call_output":
				// 先 flush 积攒的 function_call 为 assistant 消息
				if len(pendingToolCalls) > 0 {
					messages = append(messages, map[string]interface{}{
						"role":      "assistant",
						"tool_calls": pendingToolCalls,
						"content":   nil,
					})
					pendingToolCalls = nil
				}
				// function_call_output → tool 消息
				callID, _ := item["call_id"].(string)
				output, _ := item["output"].(string)
				messages = append(messages, map[string]interface{}{
					"role":       "tool",
					"tool_call_id": callID,
					"content":    output,
				})

			default:
				// 先 flush 积攒的 function_call 为 assistant 消息
				if len(pendingToolCalls) > 0 {
					messages = append(messages, map[string]interface{}{
						"role":      "assistant",
						"tool_calls": pendingToolCalls,
						"content":   nil,
					})
					pendingToolCalls = nil
				}
				// 普通消息项（有 role 和 content）
				role, _ := item["role"].(string)
				if role == "" {
					continue
				}
				var contentVal interface{}
				if c, ok := item["content"]; ok {
					contentVal = c
				}
				msg := map[string]interface{}{
					"role":    role,
					"content": contentVal,
				}
				messages = append(messages, msg)
			}
		}
		// flush 尾部积攒的 function_call
		if len(pendingToolCalls) > 0 {
			messages = append(messages, map[string]interface{}{
				"role":      "assistant",
				"tool_calls": pendingToolCalls,
				"content":   nil,
			})
		}
		return messages, systemContent
	}

	// 尝试解析为单个消息对象
	var singleMsg inputMessage
	if err := json.Unmarshal(inputRaw, &singleMsg); err == nil {
		var content interface{}
		if err := json.Unmarshal(singleMsg.Content, &content); err != nil {
			content = string(singleMsg.Content)
		}
		messages = append(messages, map[string]interface{}{
			"role":    singleMsg.Role,
			"content": content,
		})
	}

	return messages, systemContent
}

// convertResponsesToolsToOpenai 将 Responses API 的 tools 转换为 Chat Completions tools
func convertResponsesToolsToOpenai(tools []interface{}) []interface{} {
	var result []interface{}
	for _, tool := range tools {
		tm, ok := tool.(map[string]interface{})
		if !ok {
			continue
		}
		toolType, _ := tm["type"].(string)
		switch toolType {
		case "function":
			// Responses API 格式: {"type":"function","name":"...","parameters":{...}}
			// Chat Completions 格式: {"type":"function","function":{"name":"...","parameters":{...}}}
			fnObj := map[string]interface{}{
				"name":       tm["name"],
				"parameters": tm["parameters"],
			}
			if desc, ok := tm["description"]; ok {
				fnObj["description"] = desc
			}
			if strict, ok := tm["strict"]; ok {
				fnObj["strict"] = strict
			}
			openaiTool := map[string]interface{}{
				"type":     "function",
				"function": fnObj,
			}
			result = append(result, openaiTool)
		default:
			// 内置工具（web_search, file_search 等）— 上游不支持，跳过
			// 但保留 function 类型工具
		}
	}
	return result
}

// convertReasoningEffort 将 reasoning.effort 转换为 max_completion_tokens 提示
// 上游不一定支持 reasoning 参数，但可以映射 effort 到温度等参数
func convertReasoningEffort(reasoning *ResponsesReasoning) map[string]interface{} {
	if reasoning == nil {
		return nil
	}
	result := map[string]interface{}{}
	if reasoning.Effort != "" {
		// 映射 effort 到温度调整（低 effort → 高温度，高 effort → 低温度）
		// 但实际上大多数上游不直接支持，这里只做透传
		result["reasoning_effort"] = reasoning.Effort
	}
	return result
}

// buildChatCompletionsPayload 从 Responses API 请求构建 Chat Completions payload
func buildChatCompletionsPayload(req *ResponsesRequest) map[string]interface{} {
	messages, systemContent := convertResponsesInputToMessages(req)

	// 如果有 system message，前置
	if systemContent != "" {
		sysMsg := map[string]interface{}{
			"role":    "system",
			"content": systemContent,
		}
		messages = append([]interface{}{sysMsg}, messages...)
	}

	model := req.Model
	if model == "" {
		model = "glm-5.1"
	}

	payload := map[string]interface{}{
		"model":    model,
		"messages": messages,
		"stream":   true, // 强制流式
	}

	if req.MaxOutputTokens > 0 {
		payload["max_tokens"] = req.MaxOutputTokens
	}
	if req.Temperature != nil {
		payload["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		payload["top_p"] = *req.TopP
	}
	if len(req.Tools) > 0 {
		openaiTools := convertResponsesToolsToOpenai(req.Tools)
		if len(openaiTools) > 0 {
			payload["tools"] = openaiTools
		}
	}

	// reasoning 参数（上游可能不支持，但透传）
	if req.Reasoning != nil {
		reasoningMap := convertReasoningEffort(req.Reasoning)
		if len(reasoningMap) > 0 {
			payload["reasoning"] = reasoningMap
		}
	}

	// 强制请求上游在流式响应中返回 usage 信息
	payload["stream_options"] = map[string]interface{}{"include_usage": true}

	return payload
}

// buildResponsesAPIResponse 从 Chat Completions CollectedResult 构建 Responses API 响应
func buildResponsesAPIResponse(result *CollectedResult, model string, req *ResponsesRequest) *ResponsesAPIResponse {
	respID := "resp_" + randomHex(24)
	msgID := "msg_" + randomHex(24)

	content := strings.Join(result.ContentParts, "")
	var outputItems []ResponsesOutputItem

	// 构建 message 输出项
	outputMsg := ResponsesOutputItem{
		ID:     msgID,
		Type:   "message",
		Role:   "assistant",
		Status: "completed",
		Content: []ResponsesContentItem{
			{
				Type:        "output_text",
				Text:        content,
				Annotations: []interface{}{},
			},
		},
	}
	outputItems = append(outputItems, outputMsg)

	// 如果有 tool_calls，添加 function_call 输出项
	for i, tc := range result.ToolCalls {
		callID := tc.ID
		if callID == "" {
			callID = fmt.Sprintf("call_%s", randomHex(16))
		}
		outputItems = append(outputItems, ResponsesOutputItem{
			Type:      "function_call",
			ID:        fmt.Sprintf("fc_%d_%s", i, randomHex(12)),
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
			CallID:    callID,
			Status:    "completed",
		})
	}

	// 用量
	usage := ResponsesUsage{
		InputTokens:  result.PromptTokens,
		OutputTokens: result.CompletionTokens,
		TotalTokens:  result.PromptTokens + result.CompletionTokens,
		InputTokensDetails: map[string]interface{}{
			"cached_tokens": result.CachedTokens,
		},
		OutputTokensDetails: map[string]interface{}{
			"reasoning_tokens": 0,
		},
	}

	// 状态
	status := "completed"
	if result.FinishReason == "length" {
		status = "incomplete"
	}

	// 构建可选字段的 interface{} 值
	var instructionsVal interface{}
	if req.Instructions != "" {
		instructionsVal = req.Instructions
	}
	var tempVal interface{}
	if req.Temperature != nil {
		tempVal = *req.Temperature
	}
	var topPVal interface{}
	if req.TopP != nil {
		topPVal = *req.TopP
	}
	var maxOutputVal interface{}
	if req.MaxOutputTokens > 0 {
		maxOutputVal = req.MaxOutputTokens
	}

	return &ResponsesAPIResponse{
		ID:                 respID,
		Object:             "response",
		CreatedAt:          time.Now().Unix(),
		Model:              model,
		Status:             status,
		Output:             outputItems,
		Usage:              usage,
		Instructions:       instructionsVal,
		Temperature:        tempVal,
		TopP:               topPVal,
		MaxOutputTokens:    maxOutputVal,
		Tools:              nil,
		PreviousResponseID: nil,
		Reasoning:          nil,
		Metadata:           map[string]interface{}{},
	}
}

// handleResponses POST /v1/responses
func handleResponses(c *gin.Context) {
	bearer := auth.GetBearerToken()
	if bearer == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{"message": "No token. Visit /auth/start to login.", "type": "auth_required"},
		})
		return
	}

	var req ResponsesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errMsg := "Invalid request body"
		if strings.Contains(err.Error(), "http: request body too large") {
			errMsg = "请求体过大（超过限制），请减少输入内容后重试"
		}
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": errMsg, "type": "invalid_request_error"},
		})
		return
	}

	// 检测图片输入
	if req.Input != nil {
		var bodyForCheck map[string]interface{}

		// 纯字符串输入不含图片，跳过
		var inputStr string
		if err := json.Unmarshal(req.Input, &inputStr); err != nil {
			// 非 string，尝试解析为数组或对象，统一包装为 {"messages": [...]}
			var inputArr []interface{}
			if err := json.Unmarshal(req.Input, &inputArr); err == nil {
				bodyForCheck = map[string]interface{}{"messages": inputArr}
			} else {
				var inputMap map[string]interface{}
				if err := json.Unmarshal(req.Input, &inputMap); err == nil {
					bodyForCheck = map[string]interface{}{"messages": []interface{}{inputMap}}
				}
			}

			if bodyForCheck != nil && hasImageURLContent(bodyForCheck) {
				if config.ImageUnderstandingAtomic() {
					understandImages(bodyForCheck)
					// 解包回 input 格式
					if msgs, ok := bodyForCheck["messages"].([]interface{}); ok {
						var newInput json.RawMessage
						if len(msgs) == 1 {
							newInput, _ = json.Marshal(msgs[0])
						} else {
							newInput, _ = json.Marshal(msgs)
						}
						if newInput != nil {
							req.Input = newInput
						}
					}
					log.Printf("images: understood and replaced image content in responses request, forwarding text-only")
				} else if config.DropImagesWhenUnsupportedAtomic() {
					stripImagesFromBody(bodyForCheck)
					// 解包回 input 格式
					if msgs, ok := bodyForCheck["messages"].([]interface{}); ok {
						var newInput json.RawMessage
						if len(msgs) == 1 {
							newInput, _ = json.Marshal(msgs[0])
						} else {
							newInput, _ = json.Marshal(msgs)
						}
						if newInput != nil {
							req.Input = newInput
						}
					}
					log.Printf("images: stripped image content from responses request, forwarding text-only")
				} else {
					c.JSON(http.StatusBadRequest, gin.H{
						"error": gin.H{"message": "上游 API 不支持图片输入（image_url），请移除图片后重试", "type": "invalid_request_error"},
					})
					return
				}
			}
		}
	}

	model := req.Model
	if model == "" {
		model = "glm-5.1"
	}

	// 生成追踪 ID 并上报请求事件
	conversationID := "conv-" + randomHex(16)
	telemetryRequestID := "req-" + randomHex(16)
	traceID := randomHex(16)
	telemetry.ReportResponsesRequest(conversationID, telemetryRequestID, model, model, traceID, len([]rune(string(req.Input))))

	payload := buildChatCompletionsPayload(&req)

	// 确保至少 2 条消息
	ensureMinMessages(payload)

	// 对话头透传
	extraHeaders := buildConversationHeaders(c)

	// Claude Inject: 透传客户端的 anthropic-beta 头到上游
	if config.ClaudeInjectAtomic() {
		if ab := c.GetHeader("anthropic-beta"); ab != "" {
			extraHeaders["anthropic-beta"] = ab
		}
	}

	if req.Stream {
		// 流式响应：将 Chat Completions SSE 转换为 Responses API SSE 事件
		StreamResponsesAPI(c.Request.Context(), payload, model, bearer, c.Writer, extraHeaders, conversationID, telemetryRequestID, traceID)
	} else {
		// 非流式：收集所有 chunk 后转换
		result, err := CollectUpstreamChunks(c.Request.Context(), payload, bearer, extraHeaders)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": gin.H{"message": err.Error(), "type": "server_error"},
			})
			return
		}
		if result.StatusCode != 200 {
			// 检测上下文超限
			if isContextLimitError(result.ErrorText) {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": gin.H{"message": "request too large: " + result.ErrorText, "type": "invalid_request_error"},
				})
			} else {
				c.JSON(result.StatusCode, gin.H{
					"error": gin.H{"message": result.ErrorText, "type": "server_error"},
				})
			}
			return
		}
		telemetry.ReportResponsesResponse(conversationID, telemetryRequestID, model, model, traceID, result.PromptTokens, result.CompletionTokens)
		resp := buildResponsesAPIResponse(result, model, &req)
		c.JSON(http.StatusOK, resp)
	}
}

// handleResponsesCompact POST /v1/responses/compact
// 代理 Claude Code 的 compact 请求到上游，实现上下文压缩
func handleResponsesCompact(c *gin.Context) {
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
			"error": gin.H{"message": errMsg, "type": "invalid_request_error"},
		})
		return
	}

	model := "glm-5.1"
	if v, ok := body["model"].(string); ok {
		model = v
	}

	// 生成追踪 ID 并上报请求事件
	conversationID := "conv-" + randomHex(16)
	telemetryRequestID := "req-" + randomHex(16)
	traceID := randomHex(16)
	telemetry.ReportResponsesRequest(conversationID, telemetryRequestID, model, model, traceID, 0)

	// 检测并剥离 input 中的图片内容（与 handleResponses 保持一致）
	if inputRaw, ok := body["input"]; ok && inputRaw != nil {
		// 纯字符串输入不含图片，跳过
		if _, ok := inputRaw.(string); !ok {
			var bodyForCheck map[string]interface{}
			if inputArr, ok := inputRaw.([]interface{}); ok {
				bodyForCheck = map[string]interface{}{"messages": inputArr}
			} else if inputMap, ok := inputRaw.(map[string]interface{}); ok {
				bodyForCheck = map[string]interface{}{"messages": []interface{}{inputMap}}
			}
			if bodyForCheck != nil && hasImageURLContent(bodyForCheck) {
				if config.ImageUnderstandingAtomic() {
					understandImages(bodyForCheck)
					// 解包回 input 格式并回写 body
					if msgs, ok := bodyForCheck["messages"].([]interface{}); ok {
						if len(msgs) == 1 {
							body["input"] = msgs[0]
						} else {
							body["input"] = msgs
						}
					}
					log.Printf("images: understood and replaced image content in responses compact request, forwarding text-only")
				} else if config.DropImagesWhenUnsupportedAtomic() {
					stripImagesFromBody(bodyForCheck)
					// 解包回 input 格式并回写 body
					if msgs, ok := bodyForCheck["messages"].([]interface{}); ok {
						if len(msgs) == 1 {
							body["input"] = msgs[0]
						} else {
							body["input"] = msgs
						}
					}
					log.Printf("images: stripped image content from responses compact request, forwarding text-only")
				} else {
					c.JSON(http.StatusBadRequest, gin.H{
						"error": gin.H{"message": "上游 API 不支持图片输入（image_url），请移除图片后重试", "type": "invalid_request_error"},
					})
					return
				}
			}
		}
	}

	// compact 请求中，Claude Code 发送 input 字段（而非 messages）
	// 需要通过 convertResponsesInputToMessages 转换为 Chat Completions messages 格式
	var messages []interface{}
	if inputRaw, ok := body["input"]; ok && inputRaw != nil {
		inputJSON, _ := json.Marshal(inputRaw)
		tempReq := &ResponsesRequest{Input: inputJSON}
		if instr, ok := body["instructions"].(string); ok {
			tempReq.Instructions = instr
		}
		messages, _ = convertResponsesInputToMessages(tempReq)
	}

	// 构建 compact 指令：要求模型生成简洁摘要
	compactInstruction := map[string]interface{}{
		"role": "system",
		"content": "You are a context compaction assistant. Your task is to create a concise summary of the conversation so far, preserving all important facts, decisions, code changes, and user preferences. Be thorough but concise. Output only the summary, nothing else.",
	}

	// 在消息列表前面插入压缩指令
	if len(messages) > 0 {
		messages = append([]interface{}{compactInstruction}, messages...)
	}

	payload := map[string]interface{}{
		"model":      model,
		"messages":   messages,
		"stream":     true,
		"max_tokens": 4096,
	}

	// 强制请求上游返回 usage 信息
	payload["stream_options"] = map[string]interface{}{"include_usage": true}

	ensureMinMessages(payload)

	extraHeaders := buildConversationHeaders(c)

	if config.ClaudeInjectAtomic() {
		if ab := c.GetHeader("anthropic-beta"); ab != "" {
			extraHeaders["anthropic-beta"] = ab
		}
	}

	// 判断客户端是否要求流式
	isStream := false
	if v, ok := body["stream"].(bool); ok {
		isStream = v
	}

	if isStream {
		// 流式：使用 Responses API SSE 格式转换
		StreamResponsesAPI(c.Request.Context(), payload, model, bearer, c.Writer, extraHeaders, conversationID, telemetryRequestID, traceID)
	} else {
		// 非流式：收集后返回
		result, err := CollectUpstreamChunks(c.Request.Context(), payload, bearer, extraHeaders)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": gin.H{"message": err.Error(), "type": "server_error"},
			})
			return
		}
		if result.StatusCode != 200 {
			if isContextLimitError(result.ErrorText) {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": gin.H{"message": "request too large: " + result.ErrorText, "type": "invalid_request_error"},
				})
			} else {
				c.JSON(result.StatusCode, gin.H{
					"error": gin.H{"message": result.ErrorText, "type": "server_error"},
				})
			}
			return
		}

		telemetry.ReportResponsesResponse(conversationID, telemetryRequestID, model, model, traceID, result.PromptTokens, result.CompletionTokens)
		content := strings.Join(result.ContentParts, "")
		c.JSON(http.StatusOK, gin.H{
			"id":      "resp_" + randomHex(24),
			"object":  "response",
			"created": time.Now().Unix(),
			"model":   model,
			"status":  "completed",
			"output": []interface{}{
				gin.H{
					"type":   "message",
					"id":     "msg_" + randomHex(24),
					"role":   "assistant",
					"status": "completed",
					"content": []interface{}{
						gin.H{
							"type":        "output_text",
							"text":        content,
							"annotations": []interface{}{},
						},
					},
				},
			},
			"usage": gin.H{
				"input_tokens":  result.PromptTokens,
				"output_tokens": result.CompletionTokens,
				"total_tokens":  result.PromptTokens + result.CompletionTokens,
			},
		})
	}
}
