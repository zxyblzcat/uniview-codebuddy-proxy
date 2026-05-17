package proxy

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// convertResponsesToChat 将 OpenAI Responses API 请求的 input 转换为 Chat Completions 的 messages 数组
// 支持 input 为纯字符串或数组（含 role+content、function_call_output 等项）
func convertResponsesToChat(body map[string]interface{}) []interface{} {
	var messages []interface{}
	input := body["input"]

	// 处理纯字符串 input：如 "input": "hello"
	if inputStr, ok := input.(string); ok && inputStr != "" {
		return []interface{}{
			map[string]interface{}{
				"role":    "user",
				"content": inputStr,
			},
		}
	}

	inputArr, _ := input.([]interface{})
	for _, item := range inputArr {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		itemType, _ := m["type"].(string)

		// 处理 function_call_output：转换为 OpenAI tool 角色消息
		if itemType == "function_call_output" {
			callID, _ := m["call_id"].(string)
			output, _ := m["output"].(string)
			messages = append(messages, map[string]interface{}{
				"role":         "tool",
				"tool_call_id": callID,
				"content":      output,
			})
			continue
		}

		role, _ := m["role"].(string)
		content := m["content"]

		// content 可能是 string 或 content block 数组
		switch c := content.(type) {
		case string:
			if role != "" {
				messages = append(messages, map[string]interface{}{
					"role":    role,
					"content": c,
				})
			}
		case []interface{}:
			// content 是 content block 数组，提取文本
			var textParts []string
			for _, block := range c {
				b, ok := block.(map[string]interface{})
				if !ok {
					continue
				}
				btype, _ := b["type"].(string)
				if btype == "input_text" || btype == "text" {
					if t, ok := b["text"].(string); ok {
						textParts = append(textParts, t)
					}
				}
			}
			if role != "" {
				messages = append(messages, map[string]interface{}{
					"role":    role,
					"content": strings.Join(textParts, ""),
				})
			}
		default:
			if role != "" {
				messages = append(messages, map[string]interface{}{
					"role":    role,
					"content": "",
				})
			}
		}
	}
	return messages
}

// convertChatToResponsesResult 将收集的上游 chunk 数据转换为 OpenAI Responses API 格式
func convertChatToResponsesResult(result *CollectedResult, model string, requestID string, c *gin.Context) {
	text := strings.Join(result.ContentParts, "")
	var outputItems []interface{}

	if text != "" {
		outputItems = append(outputItems, map[string]interface{}{
			"type":    "message",
			"id":      "msg_" + randomHex(24),
			"role":    "assistant",
			"content": []interface{}{map[string]interface{}{"type": "output_text", "text": text}},
		})
	}

	for _, tc := range result.ToolCalls {
		outputItems = append(outputItems, map[string]interface{}{
			"type":      "function_call",
			"id":        tc.ID,
			"call_id":   tc.ID,
			"name":      tc.Function.Name,
			"arguments": tc.Function.Arguments,
		})
	}

	if len(outputItems) == 0 {
		outputItems = append(outputItems, map[string]interface{}{
			"type":    "message",
			"id":      "msg_" + randomHex(24),
			"role":    "assistant",
			"content": []interface{}{map[string]interface{}{"type": "output_text", "text": ""}},
		})
	}

	c.JSON(http.StatusOK, map[string]interface{}{
		"id":         requestID,
		"object":     "response",
		"created_at": time.Now().Unix(),
		"model":      model,
		"status":     "completed",
		"output":     outputItems,
		"usage": map[string]interface{}{
			"input_tokens":  result.PromptTokens,
			"output_tokens": result.CompletionTokens,
			"total_tokens":  result.PromptTokens + result.CompletionTokens,
		},
	})
}

// responsesSSE 格式化单个 Responses API SSE 事件
func responsesSSE(eventType string, data map[string]interface{}) string {
	dataJSON, _ := json.Marshal(data)
	return "event: " + eventType + "\ndata: " + string(dataJSON) + "\n\n"
}
