package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
)

// StreamAnthropicMessages 向上游发送请求，将 OpenAI SSE 流实时转换为 Anthropic SSE 流
func StreamAnthropicMessages(ctx context.Context, payload map[string]interface{}, model string, bearer string, w http.ResponseWriter) {
	msgID := "msg_" + randomHex(24)

	// 状态机变量
	nextBlockIdx := 0
	textBlockIdx := -1
	toolBlockIdxMap := map[int]int{}
	toolBlocksStarted := map[int]bool{}
	inputTokens := 0
	outputTokens := 0
	started := false
	finished := false

	resp, err := doUpstreamRequest(ctx, payload, model, bearer)
	if err != nil {
		if ue, ok := err.(*upstreamError); ok {
			writeAnthropicSSEError(w, ue.Error())
		} else {
			writeAnthropicSSEError(w, err.Error())
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

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	// closeOpenBlocks 关闭所有已开启的 content block
	closeOpenBlocks := func() {
		if textBlockIdx >= 0 {
			writeSSE(w, flusher, canFlush, anthropicSSE("content_block_stop", map[string]interface{}{
				"type": "content_block_stop", "index": textBlockIdx,
			}))
			textBlockIdx = -1
		}
		var tcIndices []int
		for tcIdx := range toolBlocksStarted {
			tcIndices = append(tcIndices, tcIdx)
		}
		sort.Ints(tcIndices)
		for _, tcIdx := range tcIndices {
			if toolBlocksStarted[tcIdx] {
				writeSSE(w, flusher, canFlush, anthropicSSE("content_block_stop", map[string]interface{}{
					"type": "content_block_stop", "index": toolBlockIdxMap[tcIdx],
				}))
			}
		}
	}

	for scanner.Scan() {
		// 客户端断连时停止读取上游
		select {
		case <-ctx.Done():
			return
		default:
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
				closeOpenBlocks()
				writeSSE(w, flusher, canFlush, anthropicSSE("message_delta", map[string]interface{}{
					"type":  "message_delta",
					"delta": map[string]interface{}{"stop_reason": "end_turn", "stop_sequence": nil},
					"usage": map[string]interface{}{"output_tokens": outputTokens},
				}))
				writeSSE(w, flusher, canFlush, anthropicSSE("message_stop", map[string]interface{}{
					"type": "message_stop",
				}))
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

			// 首次收到有效内容时发送 message_start
			if !started {
				started = true
				writeSSE(w, flusher, canFlush, anthropicSSE("message_start", map[string]interface{}{
					"type": "message_start",
					"message": map[string]interface{}{
						"id":            msgID,
						"type":          "message",
						"role":          "assistant",
						"content":       []interface{}{},
						"model":         model,
						"stop_reason":   nil,
						"stop_sequence": nil,
						"usage":         map[string]interface{}{"input_tokens": inputTokens, "output_tokens": 0},
					},
				}))
			}

			// 处理文本内容
			if content, ok := delta["content"].(string); ok && content != "" {
				if textBlockIdx < 0 {
					textBlockIdx = nextBlockIdx
					nextBlockIdx++
					writeSSE(w, flusher, canFlush, anthropicSSE("content_block_start", map[string]interface{}{
						"type":  "content_block_start",
						"index": textBlockIdx,
						"content_block": map[string]interface{}{
							"type": "text",
							"text": "",
						},
					}))
				}
				writeSSE(w, flusher, canFlush, anthropicSSE("content_block_delta", map[string]interface{}{
					"type":  "content_block_delta",
					"index": textBlockIdx,
					"delta": map[string]interface{}{
						"type": "text_delta",
						"text": content,
					},
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

					if _, exists := toolBlocksStarted[tcIdx]; !exists {
						toolBlocksStarted[tcIdx] = false
						toolBlockIdxMap[tcIdx] = nextBlockIdx
						nextBlockIdx++
					}

					if !toolBlocksStarted[tcIdx] {
						if id, ok := tcMap["id"].(string); ok && id != "" {
							toolBlocksStarted[tcIdx] = true
							// 关闭前面的文本 block
							if textBlockIdx >= 0 {
								writeSSE(w, flusher, canFlush, anthropicSSE("content_block_stop", map[string]interface{}{
									"type": "content_block_stop", "index": textBlockIdx,
								}))
								textBlockIdx = -1
							}
							fnName := ""
							if fn, ok := tcMap["function"].(map[string]interface{}); ok {
								fnName, _ = fn["name"].(string)
							}
							writeSSE(w, flusher, canFlush, anthropicSSE("content_block_start", map[string]interface{}{
								"type":  "content_block_start",
								"index": toolBlockIdxMap[tcIdx],
								"content_block": map[string]interface{}{
									"type":  "tool_use",
									"id":    id,
									"name":  fnName,
									"input": map[string]interface{}{},
								},
							}))
						}
					}

					if toolBlocksStarted[tcIdx] {
						if fn, ok := tcMap["function"].(map[string]interface{}); ok {
							if args, ok := fn["arguments"].(string); ok && args != "" {
								writeSSE(w, flusher, canFlush, anthropicSSE("content_block_delta", map[string]interface{}{
									"type":  "content_block_delta",
									"index": toolBlockIdxMap[tcIdx],
									"delta": map[string]interface{}{
										"type":         "input_json_delta",
										"partial_json": args,
									},
								}))
							}
						}
					}
				}
			}

			// 处理 finish_reason
			if fr != "" && !finished {
				finished = true
				closeOpenBlocks()
				writeSSE(w, flusher, canFlush, anthropicSSE("message_delta", map[string]interface{}{
					"type":  "message_delta",
					"delta": map[string]interface{}{"stop_reason": finishReasonToStopReason(fr), "stop_sequence": nil},
					"usage": map[string]interface{}{"output_tokens": outputTokens},
				}))
				writeSSE(w, flusher, canFlush, anthropicSSE("message_stop", map[string]interface{}{
					"type": "message_stop",
				}))
			}
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("SSE scan error: %v", err)
	}
}

// writeSSE 写入 SSE 数据并 flush
func writeSSE(w http.ResponseWriter, flusher http.Flusher, canFlush bool, data string) {
	fmt.Fprint(w, data)
	if canFlush {
		flusher.Flush()
	}
}

// writeAnthropicSSEError 写入 Anthropic 格式的 SSE 错误
func writeAnthropicSSEError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	errData, _ := json.Marshal(map[string]interface{}{
		"type": "error",
		"error": map[string]interface{}{
			"type":    "api_error",
			"message": msg,
		},
	})
	fmt.Fprintf(w, "event: error\ndata: %s\n\n", string(errData))
	// 发送 message_stop 以便客户端知道流已结束
	fmt.Fprintf(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
}
