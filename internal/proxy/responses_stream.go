package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
)

// StreamResponsesSSE 向上游发送请求，将 OpenAI Chat SSE 流实时转换为 Responses API SSE 流
func StreamResponsesSSE(ctx context.Context, payload map[string]interface{}, model string, bearer string, w http.ResponseWriter) {
	respID := "resp_" + randomHex(24)
	itemID := "msg_" + randomHex(24)
	started := false
	finished := false
	textStarted := false
	textOutputIndex := -1
	nextOutputIndex := 0
	toolCallItemIDs := map[int]string{}
	toolCallOutputIdx := map[int]int{}
	toolCallsStarted := map[int]bool{}
	toolCallNames := map[int]string{}
	toolCallArgs := map[int]string{}
	inputTokens := 0
	outputTokens := 0
	var textContent strings.Builder

	resp, err := doUpstreamRequest(ctx, payload, model, bearer)
	if err != nil {
		if ue, ok := err.(*upstreamError); ok {
			writeResponsesSSEError(w, ue.Error())
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

	// 写入错误跟踪：客户端断连后跳过后续写入和上游处理
	var writeErr error
	safeWrite := func(data string) {
		if writeErr != nil {
			return
		}
		writeErr = writeSSE(w, flusher, canFlush, data)
	}

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	// closeTextBlock 关闭文本 block，发送 content_part.done 和 output_item.done
	closeTextBlock := func() {
		if !textStarted {
			return
		}
		textStarted = false
		fullText := textContent.String()
		safeWrite(responsesSSE("response.output_text.delta", map[string]interface{}{
			"type": "response.output_text.delta", "item_id": itemID,
			"output_index": textOutputIndex, "content_index": 0, "delta": "",
		}))
		safeWrite(responsesSSE("response.output_text.done", map[string]interface{}{
			"type": "response.output_text.done", "item_id": itemID,
			"output_index": textOutputIndex, "content_index": 0, "text": fullText,
		}))
		safeWrite(responsesSSE("response.content_part.done", map[string]interface{}{
			"type": "response.content_part.done", "item_id": itemID,
			"output_index": textOutputIndex, "content_index": 0,
			"part": map[string]interface{}{"type": "output_text", "text": fullText, "annotations": []interface{}{}},
		}))
		safeWrite(responsesSSE("response.output_item.done", map[string]interface{}{
			"type": "response.output_item.done", "output_index": textOutputIndex,
			"item": map[string]interface{}{"id": itemID, "type": "message", "role": "assistant",
				"content": []interface{}{map[string]interface{}{"type": "output_text", "text": fullText, "annotations": []interface{}{}}}},
		}))
	}

	// closeToolCallBlocks 关闭所有已开启的 tool call blocks
	closeToolCallBlocks := func() {
		var tcIndices []int
		for tcIdx := range toolCallsStarted {
			tcIndices = append(tcIndices, tcIdx)
		}
		sort.Ints(tcIndices)
		for _, tcIdx := range tcIndices {
			if toolCallsStarted[tcIdx] {
				tcItemID := toolCallItemIDs[tcIdx]
				tcOutputIdx := toolCallOutputIdx[tcIdx]
				safeWrite(responsesSSE("response.function_call_arguments.delta", map[string]interface{}{
					"type": "response.function_call_arguments.delta", "item_id": tcItemID,
					"output_index": tcOutputIdx, "call_id": tcItemID, "delta": "",
				}))
				safeWrite(responsesSSE("response.function_call_arguments.done", map[string]interface{}{
					"type": "response.function_call_arguments.done", "item_id": tcItemID,
					"output_index": tcOutputIdx, "call_id": tcItemID, "arguments": toolCallArgs[tcIdx],
				}))
				safeWrite(responsesSSE("response.output_item.done", map[string]interface{}{
					"type": "response.output_item.done", "output_index": tcOutputIdx,
					"item": map[string]interface{}{"id": tcItemID, "type": "function_call", "call_id": tcItemID, "name": toolCallNames[tcIdx], "arguments": toolCallArgs[tcIdx]},
				}))
			}
		}
	}

	// emitCompleted 发送 response.completed 事件
	emitCompleted := func() {
		safeWrite(responsesSSE("response.completed", map[string]interface{}{
			"type": "response.completed",
			"response": map[string]interface{}{
				"id": respID, "object": "response", "created_at": time.Now().Unix(),
				"model": model, "status": "completed", "output": []interface{}{},
				"usage": map[string]interface{}{"input_tokens": inputTokens, "output_tokens": outputTokens, "total_tokens": inputTokens + outputTokens},
			},
		}))
	}

	for scanner.Scan() {
		// 客户端断连或写入失败时停止
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
			if pt, ok := u["prompt_tokens"].(float64); ok && int(pt) > 0 {
				inputTokens = int(pt)
			}
			if ct, ok := u["completion_tokens"].(float64); ok && int(ct) > 0 {
				outputTokens = int(ct)
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
			fr, _ := choice["finish_reason"].(string)

			// 首次收到有效内容时发送开始事件
			if !started {
				started = true
				safeWrite(responsesSSE("response.created", map[string]interface{}{
					"type": "response.created",
					"response": map[string]interface{}{
						"id": respID, "object": "response", "created_at": time.Now().Unix(),
						"model": model, "status": "in_progress", "output": []interface{}{},
					},
				}))
				safeWrite(responsesSSE("response.in_progress", map[string]interface{}{
					"type": "response.in_progress",
					"response": map[string]interface{}{
						"id": respID, "object": "response", "created_at": time.Now().Unix(),
						"model": model, "status": "in_progress", "output": []interface{}{},
					},
				}))
			}

			// 处理文本内容
			if content, ok := delta["content"].(string); ok && content != "" {
				if !textStarted {
					textStarted = true
					textOutputIndex = nextOutputIndex
					nextOutputIndex++
					safeWrite(responsesSSE("response.output_item.added", map[string]interface{}{
						"type": "response.output_item.added", "output_index": textOutputIndex,
						"item": map[string]interface{}{"id": itemID, "type": "message", "role": "assistant", "content": []interface{}{}},
					}))
					safeWrite(responsesSSE("response.content_part.added", map[string]interface{}{
						"type": "response.content_part.added", "item_id": itemID,
						"output_index": textOutputIndex, "content_index": 0,
						"part": map[string]interface{}{"type": "output_text", "text": ""},
					}))
				}
				textContent.WriteString(content)
				safeWrite(responsesSSE("response.output_text.delta", map[string]interface{}{
					"type": "response.output_text.delta", "item_id": itemID,
					"output_index": textOutputIndex, "content_index": 0, "delta": content,
				}))
			}

			// 处理工具调用
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

					if _, exists := toolCallsStarted[tcIdx]; !exists {
						toolCallsStarted[tcIdx] = false
						toolCallItemIDs[tcIdx] = "fc_" + randomHex(24)
						toolCallOutputIdx[tcIdx] = nextOutputIndex
						nextOutputIndex++
					}

					// 关闭前面的 text block
					closeTextBlock()

					tcItemID := toolCallItemIDs[tcIdx]
					tcOutputIdx := toolCallOutputIdx[tcIdx]

					if !toolCallsStarted[tcIdx] {
						if id, ok := tcMap["id"].(string); ok && id != "" {
							toolCallsStarted[tcIdx] = true
							fnName := ""
							if fn, ok := tcMap["function"].(map[string]interface{}); ok {
								fnName, _ = fn["name"].(string)
							}
							toolCallNames[tcIdx] = fnName
							safeWrite(responsesSSE("response.output_item.added", map[string]interface{}{
								"type": "response.output_item.added", "output_index": tcOutputIdx,
								"item": map[string]interface{}{"id": tcItemID, "type": "function_call", "call_id": tcItemID, "name": fnName},
							}))
						}
					}

					// arguments 片段
					if toolCallsStarted[tcIdx] {
						if fn, ok := tcMap["function"].(map[string]interface{}); ok {
							if args, ok := fn["arguments"].(string); ok && args != "" {
								toolCallArgs[tcIdx] += args
								safeWrite(responsesSSE("response.function_call_arguments.delta", map[string]interface{}{
									"type": "response.function_call_arguments.delta", "item_id": tcItemID,
									"output_index": tcOutputIdx, "call_id": tcItemID, "delta": args,
								}))
							}
						}
					}
				}
			}

			// 处理 finish_reason
			if fr != "" && !finished {
				finished = true
				closeTextBlock()
				closeToolCallBlocks()
				emitCompleted()
			}
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("SSE scan error: %v", err)
	}
}

// writeResponsesSSEError 写入 Responses API 格式的 SSE 错误
func writeResponsesSSEError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	errData, _ := json.Marshal(map[string]interface{}{
		"type":  "error",
		"error": map[string]interface{}{"message": msg},
	})
	fmt.Fprintf(w, "event: response.error\ndata: %s\n\n", string(errData))
	fmt.Fprintf(w, "event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n")
}
