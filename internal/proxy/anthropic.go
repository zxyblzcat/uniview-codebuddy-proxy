package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// ─── Anthropic → OpenAI 请求转换 ─────────────────────────

// extractAnthropicText 从 Anthropic content 字段提取纯文本
// content 可能是 string 或 content blocks 数组
func extractAnthropicText(content interface{}) string {
	switch c := content.(type) {
	case string:
		return c
	case []interface{}:
		parts := []string{}
		for _, block := range c {
			b, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			if b["type"] == "text" {
				if t, ok := b["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "")
	}
	return ""
}

// convertAnthropicMessagesToOpenai 将 Anthropic 格式的 system + messages 转换为 OpenAI 的 messages 数组
func convertAnthropicMessagesToOpenai(system interface{}, messages []interface{}) []interface{} {
	var openaiMessages []interface{}

	// 处理 system 字段
	if system != nil {
		sysText := ""
		switch s := system.(type) {
		case string:
			sysText = s
		case []interface{}:
			sysText = extractAnthropicText(s)
		}
		if sysText != "" {
			openaiMessages = append(openaiMessages, map[string]interface{}{
				"role":    "system",
				"content": sysText,
			})
		}
	}

	for _, msg := range messages {
		m, ok := msg.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		content := m["content"]

		switch role {
		case "user":
			switch c := content.(type) {
			case string:
				openaiMessages = append(openaiMessages, map[string]interface{}{
					"role":    "user",
					"content": c,
				})
			case []interface{}:
				var textParts []string
				for _, block := range c {
					b, ok := block.(map[string]interface{})
					if !ok {
						continue
					}
					btype, _ := b["type"].(string)
					switch btype {
					case "text":
						if t, ok := b["text"].(string); ok {
							textParts = append(textParts, t)
						}
					case "tool_result":
						// tool_result 转换为 OpenAI 的 tool 角色消息
						toolContent := ""
						rc := b["content"]
						switch r := rc.(type) {
						case string:
							toolContent = r
						case []interface{}:
							toolContent = extractAnthropicText(r)
						}
						// 如果前面有累积的文本，先输出 user 消息
						if len(textParts) > 0 {
							openaiMessages = append(openaiMessages, map[string]interface{}{
								"role":    "user",
								"content": strings.Join(textParts, ""),
							})
							textParts = nil
						}
						toolUseID, _ := b["tool_use_id"].(string)
						openaiMessages = append(openaiMessages, map[string]interface{}{
							"role":         "tool",
							"tool_call_id": toolUseID,
							"content":      toolContent,
						})
					}
				}
				if len(textParts) > 0 {
					openaiMessages = append(openaiMessages, map[string]interface{}{
						"role":    "user",
						"content": strings.Join(textParts, ""),
					})
				}
			default:
				openaiMessages = append(openaiMessages, map[string]interface{}{
					"role":    "user",
					"content": "",
				})
			}

		case "assistant":
			switch c := content.(type) {
			case string:
				openaiMessages = append(openaiMessages, map[string]interface{}{
					"role":    "assistant",
					"content": c,
				})
			case []interface{}:
				var textParts []string
				var toolCalls []interface{}
				for _, block := range c {
					b, ok := block.(map[string]interface{})
					if !ok {
						continue
					}
					btype, _ := b["type"].(string)
					switch btype {
					case "text":
						if t, ok := b["text"].(string); ok {
							textParts = append(textParts, t)
						}
					case "tool_use":
						inputJSON, _ := json.Marshal(b["input"])
						toolCalls = append(toolCalls, map[string]interface{}{
							"id":   b["id"],
							"type": "function",
							"function": map[string]interface{}{
								"name":      b["name"],
								"arguments": string(inputJSON),
							},
						})
					}
				}
				assistantMsg := map[string]interface{}{"role": "assistant"}
				if len(textParts) > 0 {
					assistantMsg["content"] = strings.Join(textParts, "")
				} else {
					assistantMsg["content"] = nil
				}
				if len(toolCalls) > 0 {
					assistantMsg["tool_calls"] = toolCalls
				}
				openaiMessages = append(openaiMessages, assistantMsg)
			default:
				openaiMessages = append(openaiMessages, map[string]interface{}{
					"role":    "assistant",
					"content": "",
				})
			}
		}
	}

	return openaiMessages
}

// convertToolsAnthropicToOpenai 将 Anthropic 工具定义转换为 OpenAI 格式
func convertToolsAnthropicToOpenai(tools []interface{}) []interface{} {
	var openaiTools []interface{}
	for _, tool := range tools {
		t, ok := tool.(map[string]interface{})
		if !ok {
			continue
		}
		openaiTools = append(openaiTools, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        t["name"],
				"description": t["description"],
				"parameters":  t["input_schema"],
			},
		})
	}
	return openaiTools
}

// convertToolChoiceAnthropicToOpenai 将 Anthropic 的 tool_choice 转换为 OpenAI 格式
func convertToolChoiceAnthropicToOpenai(toolChoice interface{}) interface{} {
	switch tc := toolChoice.(type) {
	case string:
		return tc
	case map[string]interface{}:
		tcType, _ := tc["type"].(string)
		switch tcType {
		case "auto":
			return "auto"
		case "any":
			return "required"
		case "none":
			return "none"
		case "tool":
			name, _ := tc["name"].(string)
			return map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name": name,
				},
			}
		}
	}
	return "auto"
}

// ─── OpenAI → Anthropic 响应转换 ─────────────────────────

// finishReasonToStopReason 映射 OpenAI finish_reason 到 Anthropic stop_reason
func finishReasonToStopReason(finishReason string) string {
	mapping := map[string]string{
		"stop":           "end_turn",
		"tool_calls":     "tool_use",
		"length":         "max_tokens",
		"content_filter": "end_turn",
	}
	if v, ok := mapping[finishReason]; ok {
		return v
	}
	return "end_turn"
}

// convertOpenAIToAnthropicResponse 将 OpenAI 收集结果转换为 Anthropic 非流式响应格式
func convertOpenAIToAnthropicResponse(result *CollectedResult, model string, c *gin.Context) {
	msgID := "msg_" + randomHex(24)
	var contentBlocks []interface{}

	text := strings.Join(result.ContentParts, "")
	if text != "" {
		contentBlocks = append(contentBlocks, map[string]interface{}{
			"type": "text",
			"text": text,
		})
	}

	for _, tc := range result.ToolCalls {
		var inputData interface{} = map[string]interface{}{}
		if tc.Function.Arguments != "" {
			json.Unmarshal([]byte(tc.Function.Arguments), &inputData)
		}
		contentBlocks = append(contentBlocks, map[string]interface{}{
			"type":  "tool_use",
			"id":    tc.ID,
			"name":  tc.Function.Name,
			"input": inputData,
		})
	}

	if len(contentBlocks) == 0 {
		contentBlocks = append(contentBlocks, map[string]interface{}{
			"type": "text",
			"text": "",
		})
	}

	c.JSON(http.StatusOK, map[string]interface{}{
		"id":            msgID,
		"type":          "message",
		"role":          "assistant",
		"content":       contentBlocks,
		"model":         model,
		"stop_reason":   finishReasonToStopReason(result.FinishReason),
		"stop_sequence": nil,
		"usage": map[string]interface{}{
			"input_tokens":                result.PromptTokens,
			"output_tokens":               result.CompletionTokens,
			"cache_creation_input_tokens": 0,
			"cache_read_input_tokens":     result.CachedTokens,
		},
	})
}

// anthropicErrorResponse 生成 Anthropic 风格的错误响应
func anthropicErrorResponse(c *gin.Context, statusCode int, errorType string, message string) {
	c.JSON(statusCode, map[string]interface{}{
		"type": "error",
		"error": map[string]interface{}{
			"type":    errorType,
			"message": message,
		},
	})
}

// anthropicSSE 格式化单个 Anthropic SSE 事件
func anthropicSSE(eventType string, data map[string]interface{}) string {
	dataJSON, _ := json.Marshal(data)
	return fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, string(dataJSON))
}
