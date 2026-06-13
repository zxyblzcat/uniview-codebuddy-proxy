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

	"uniview-codebuddy-proxy/internal/config"
	"uniview-codebuddy-proxy/internal/telemetry"
)

// responsesSSE 写入一个 Responses API 格式的 SSE 事件
func responsesSSE(event string, data interface{}) string {
	jsonData, _ := json.Marshal(data)
	return fmt.Sprintf("event: %s\ndata: %s\n\n", event, string(jsonData))
}

// StreamResponsesAPI 将上游 Chat Completions SSE 流实时转换为 Responses API SSE 事件
func StreamResponsesAPI(ctx context.Context, payload map[string]interface{}, model string, bearer string, w http.ResponseWriter, extraHeaders map[string]string, conversationID, telemetryRequestID, traceID string) {
	respID := "resp_" + randomHex(24)
	outputItemID := "msg_" + randomHex(24)

	// 发起上游请求
	resp, err := doUpstreamRequestWithRetry(ctx, config.ChatURL, payload, model, bearer, "craft", extraHeaders)
	if err != nil {
		if ue, ok := err.(*upstreamError); ok {
			if isContextLimitError(ue.Message) {
				writeResponsesSSEContextLimitError(w, ue.Message)
			} else {
				writeResponsesSSEError(w, ue.Error())
			}
		} else {
			writeResponsesSSEError(w, err.Error())
		}
		return
	}
	defer resp.Body.Close()
	body := wrapWithIdleTimeout(resp.Body)

	// 设置 SSE 响应头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, canFlush := w.(http.Flusher)

	// 状态跟踪
	var writeErr error
	started := false
	finished := false
	textStarted := false
	contentIndex := 0
	promptTokens := 0
	completionTokens := 0
	var finishReason string
	var fullContent strings.Builder

	// tool_call 状态跟踪
	type toolCallState struct {
		ID        string
		Name      string
		Arguments string
		Started   bool
	}
	toolCalls := map[int]*toolCallState{}
	var toolCallOrder []int // 维护 tool_call 的顺序

	safeWrite := func(data string) {
		if writeErr != nil {
			return
		}
		_, writeErr = fmt.Fprint(w, data)
		if canFlush {
			flusher.Flush()
		}
	}

	// emitResponseCreated 发送 response.created 和 response.in_progress 事件
	emitResponseCreated := func() {
		createdResp := buildResponsesSSEObject(respID, model, "in_progress", nil, 0, 0)
		safeWrite(responsesSSE("response.created", map[string]interface{}{
			"type":     "response.created",
			"response": createdResp,
		}))
		safeWrite(responsesSSE("response.in_progress", map[string]interface{}{
			"type":     "response.in_progress",
			"response": createdResp,
		}))
	}

	// emitTextStart 发送文本输出的开始事件
	emitTextStart := func() {
		if !textStarted {
			textStarted = true
			// 输出项添加
			safeWrite(responsesSSE("response.output_item.added", map[string]interface{}{
				"type":         "response.output_item.added",
				"output_index": 0,
				"item": map[string]interface{}{
					"type":   "message",
					"id":     outputItemID,
					"status": "in_progress",
					"role":   "assistant",
					"content": []interface{}{},
				},
			}))
			// 内容部分添加
			safeWrite(responsesSSE("response.content_part.added", map[string]interface{}{
				"type":          "response.content_part.added",
				"output_index":  0,
				"content_index": contentIndex,
				"part": map[string]interface{}{
					"type": "output_text",
					"text": "",
				},
			}))
		}
	}

	// closeTextBlock 关闭文本输出块
	closeTextBlock := func() {
		if textStarted {
			// output_text.done
			safeWrite(responsesSSE("response.output_text.done", map[string]interface{}{
				"type":          "response.output_text.done",
				"output_index":  0,
				"content_index": contentIndex,
				"text":          fullContent.String(),
			}))
			// content_part.done
			safeWrite(responsesSSE("response.content_part.done", map[string]interface{}{
				"type":          "response.content_part.done",
				"output_index":  0,
				"content_index": contentIndex,
				"part": map[string]interface{}{
					"type":        "output_text",
					"text":        fullContent.String(),
					"annotations": []interface{}{},
				},
			}))
			// output_item.done
			safeWrite(responsesSSE("response.output_item.done", map[string]interface{}{
				"type":         "response.output_item.done",
				"output_index": 0,
				"item": map[string]interface{}{
					"type":   "message",
					"id":     outputItemID,
					"status": "completed",
					"role":   "assistant",
					"content": []interface{}{
						map[string]interface{}{
							"type":        "output_text",
							"text":        fullContent.String(),
							"annotations": []interface{}{},
						},
					},
				},
			}))
		}
	}

	// closeToolCallBlocks 关闭所有 tool_call 输出块
	closeToolCallBlocks := func() {
		for _, tcIdx := range toolCallOrder {
			tc := toolCalls[tcIdx]
			if tc == nil || !tc.Started {
				continue
			}
			outputIdx := computeToolOutputIndex(textStarted, toolCallOrder, tcIdx)
			// function_call_arguments.done
			safeWrite(responsesSSE("response.function_call_arguments.done", map[string]interface{}{
				"type":         "response.function_call_arguments.done",
				"output_index": outputIdx,
				"item_id":      tc.ID,
				"arguments":    tc.Arguments,
			}))
			// output_item.done
			safeWrite(responsesSSE("response.output_item.done", map[string]interface{}{
				"type":         "response.output_item.done",
				"output_index": outputIdx,
				"item": map[string]interface{}{
					"type":      "function_call",
					"id":        tc.ID,
					"call_id":   tc.ID,
					"name":      tc.Name,
					"arguments": tc.Arguments,
					"status":    "completed",
				},
			}))
		}
	}

	// emitCompleted 发送 response.completed 事件
	emitCompleted := func() {
		status := "completed"
		if finishReason == "length" {
			status = "incomplete"
		}

		// 构建输出项列表
		var outputItems []interface{}
		if textStarted {
			outputItems = append(outputItems, map[string]interface{}{
				"type":   "message",
				"id":     outputItemID,
				"status": "completed",
				"role":   "assistant",
				"content": []interface{}{
					map[string]interface{}{
						"type":        "output_text",
						"text":        fullContent.String(),
						"annotations": []interface{}{},
					},
				},
			})
		}
		for _, tcIdx := range toolCallOrder {
			tc := toolCalls[tcIdx]
			if tc == nil {
				continue
			}
			outputItems = append(outputItems, map[string]interface{}{
				"type":      "function_call",
				"id":        tc.ID,
				"call_id":   tc.ID,
				"name":      tc.Name,
				"arguments": tc.Arguments,
				"status":    "completed",
			})
		}

		completedResp := buildResponsesSSEObject(respID, model, status, outputItems, promptTokens, completionTokens)
		safeWrite(responsesSSE("response.completed", map[string]interface{}{
			"type":     "response.completed",
			"response": completedResp,
		}))
	}


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
			if !finished {
				finished = true
				closeTextBlock()
				closeToolCallBlocks()
				emitCompleted()
			}
			continue
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

		choices, _ := chunk["choices"].([]interface{})
		for _, ch := range choices {
			choice, _ := ch.(map[string]interface{})
			if choice == nil {
				continue
			}
			delta, _ := choice["delta"].(map[string]interface{})
			if delta == nil {
				continue
			}

			// 首次收到有效内容时发送 response.created / response.in_progress
			if !started {
				started = true
				emitResponseCreated()
			}

			// 处理文本内容
			if content, ok := delta["content"].(string); ok && content != "" {
				emitTextStart()
				fullContent.WriteString(content)
				safeWrite(responsesSSE("response.output_text.delta", map[string]interface{}{
					"type":          "response.output_text.delta",
					"output_index":  0,
					"content_index": contentIndex,
					"delta":         content,
				}))
			}

			// 处理 tool_calls
			if tcs, ok := delta["tool_calls"].([]interface{}); ok {
				for _, tc := range tcs {
					tcMap, _ := tc.(map[string]interface{})
					if tcMap == nil {
						continue
					}
					tcIdx := 0
					if v, ok := tcMap["index"].(float64); ok {
						tcIdx = int(v)
					}

					if _, exists := toolCalls[tcIdx]; !exists {
						toolCalls[tcIdx] = &toolCallState{}
						toolCallOrder = append(toolCallOrder, tcIdx)
					}

					tcState := toolCalls[tcIdx]

					// 首次出现此 tool_call（有 ID）
					if id, ok := tcMap["id"].(string); ok && id != "" {
						tcState.ID = id
						if !tcState.Started {
							tcState.Started = true
							fnName := ""
							if fn, ok := tcMap["function"].(map[string]interface{}); ok {
								fnName, _ = fn["name"].(string)
								tcState.Name = fnName
							}

							outputIdx := computeToolOutputIndex(textStarted, toolCallOrder, tcIdx)

							// 先关闭文本块（如果还没关闭）
							// 注意：文本块在 closeTextBlock 中关闭，这里只处理 tool_call 开始

							// function_call 输出项添加
							safeWrite(responsesSSE("response.output_item.added", map[string]interface{}{
								"type":         "response.output_item.added",
								"output_index": outputIdx,
								"item": map[string]interface{}{
									"type":    "function_call",
									"id":      id,
									"call_id": id,
									"name":    fnName,
									"status":  "in_progress",
								},
							}))
						}
					}

					// 处理 function arguments delta
					if fn, ok := tcMap["function"].(map[string]interface{}); ok {
						if args, ok := fn["arguments"].(string); ok && args != "" {
							tcState.Arguments += args
							// 仅在 output_item.added 已发送后才发送 arguments delta
							if tcState.Started {
								outputIdx := computeToolOutputIndex(textStarted, toolCallOrder, tcIdx)
								safeWrite(responsesSSE("response.function_call_arguments.delta", map[string]interface{}{
									"type":         "response.function_call_arguments.delta",
									"output_index": outputIdx,
									"item_id":      tcState.ID,
									"delta":        args,
								}))
							}
						}
						// 也提取 name（可能在后续 chunk 中出现）
						if name, ok := fn["name"].(string); ok && name != "" && tcState.Name == "" {
							tcState.Name = name
						}
					}
				}
			}

			// 处理 finish_reason
			if fr, ok := choice["finish_reason"].(string); ok && fr != "" && !finished {
				finishReason = fr
			}
		}
	}

// 上报 telemetry 响应事件
telemetry.ReportResponsesResponse(conversationID, telemetryRequestID, model, model, traceID, promptTokens, completionTokens)

	if err := scanner.Err(); err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			log.Printf("SSE scan error in StreamResponsesAPI: %v", err)
		}
		// 向客户端发送流截断通知
		if !finished {
			if started {
				closeTextBlock()
				closeToolCallBlocks()
			} else {
				// 未收到任何内容就出错，需要先发送 response.created + response.in_progress
				emitResponseCreated()
			}
			// 发送一个 truncated 状态的 completed 事件
			status := "incomplete"
			completedResp := buildResponsesSSEObject(respID, model, status, nil, promptTokens, completionTokens)
			safeWrite(responsesSSE("response.completed", map[string]interface{}{
				"type":     "response.completed",
				"response": completedResp,
			}))
		}
	}
}

// buildResponsesSSEObject 构建用于 SSE 事件的 response 对象
func buildResponsesSSEObject(respID, model, status string, outputItems []interface{}, inputTokens, outputTokens int) map[string]interface{} {
	resp := map[string]interface{}{
		"id":         respID,
		"object":     "response",
		"created_at": time.Now().Unix(),
		"model":      model,
		"status":     status,
		"output":     outputItems,
		"usage": map[string]interface{}{
			"input_tokens":  inputTokens,
			"output_tokens": outputTokens,
			"total_tokens":  inputTokens + outputTokens,
		},
		"metadata": map[string]interface{}{},
	}
	return resp
}

// writeResponsesSSEError 写入 Responses API 格式的 SSE 错误
func writeResponsesSSEError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	errData, _ := json.Marshal(map[string]interface{}{
		"type": "error",
		"error": map[string]interface{}{
			"type":    "server_error",
			"message": msg,
		},
	})
	fmt.Fprintf(w, "event: error\ndata: %s\n\n", string(errData))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// writeResponsesSSEContextLimitError 写入 Responses API 格式的上下文超限 SSE 错误
func writeResponsesSSEContextLimitError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	errData, _ := json.Marshal(map[string]interface{}{
		"type": "error",
		"error": map[string]interface{}{
			"type":    "invalid_request_error",
			"message": "request too large: " + msg,
		},
	})
	fmt.Fprintf(w, "event: error\ndata: %s\n\n", string(errData))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// indexOfInt 返回 int 在 slice 中的索引，不存在则返回 -1
func indexOfInt(slice []int, val int) int {
	for i, v := range slice {
		if v == val {
			return i
		}
	}
	return -1
}

// computeToolOutputIndex 计算 tool_call 的 output_index
// text 输出占 index 0，tool_call 从 textStarted 后开始偏移
func computeToolOutputIndex(textStarted bool, toolCallOrder []int, tcIdx int) int {
	outputIdx := 0
	if textStarted {
		outputIdx = 1
	}
	return outputIdx + indexOfInt(toolCallOrder, tcIdx)
}

