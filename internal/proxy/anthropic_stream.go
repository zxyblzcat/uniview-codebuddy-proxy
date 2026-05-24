package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
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
	thinkingBlockIdx := -1
	textBlockIdx := -1
	toolBlockIdxMap := map[int]int{}
	toolBlocksStarted := map[int]bool{}
	inputTokens := 0
	outputTokens := 0
	cachedTokens := 0
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

	// closeOpenBlocks 关闭所有已开启的 content block
	closeOpenBlocks := func() {
		if thinkingBlockIdx >= 0 {
			safeWrite(anthropicSSE("content_block_stop", map[string]interface{}{
				"type": "content_block_stop", "index": thinkingBlockIdx,
			}))
			thinkingBlockIdx = -1
		}
		if textBlockIdx >= 0 {
			safeWrite(anthropicSSE("content_block_stop", map[string]interface{}{
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
				safeWrite(anthropicSSE("content_block_stop", map[string]interface{}{
					"type": "content_block_stop", "index": toolBlockIdxMap[tcIdx],
				}))
			}
		}
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
				closeOpenBlocks()
				safeWrite(anthropicSSE("message_delta", map[string]interface{}{
					"type":  "message_delta",
					"delta": map[string]interface{}{"stop_reason": "end_turn", "stop_sequence": nil},
					"usage": map[string]interface{}{"output_tokens": outputTokens, "cache_creation_input_tokens": 0, "cache_read_input_tokens": cachedTokens},
				}))
				safeWrite(anthropicSSE("message_stop", map[string]interface{}{
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
			if details, ok := u["prompt_tokens_details"].(map[string]interface{}); ok {
				if ct, ok := details["cached_tokens"].(float64); ok && int(ct) > 0 {
					cachedTokens = int(ct)
				}
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
				safeWrite(anthropicSSE("message_start", map[string]interface{}{
					"type": "message_start",
					"message": map[string]interface{}{
						"id":            msgID,
						"type":          "message",
						"role":          "assistant",
						"content":       []interface{}{},
						"model":         model,
						"stop_reason":   nil,
						"stop_sequence": nil,
						"usage":         map[string]interface{}{"input_tokens": inputTokens, "output_tokens": 0, "cache_creation_input_tokens": 0, "cache_read_input_tokens": cachedTokens},
					},
				}))
			}

			// 处理 reasoning_content（thinking blocks）
			if reasoningContent, ok := delta["reasoning_content"].(string); ok && reasoningContent != "" {
				if thinkingBlockIdx < 0 {
					thinkingBlockIdx = nextBlockIdx
					nextBlockIdx++
					safeWrite(anthropicSSE("content_block_start", map[string]interface{}{
						"type":  "content_block_start",
						"index": thinkingBlockIdx,
						"content_block": map[string]interface{}{
							"type":      "thinking",
							"thinking":  "",
							"signature": "",
						},
					}))
				}
				safeWrite(anthropicSSE("content_block_delta", map[string]interface{}{
					"type":  "content_block_delta",
					"index": thinkingBlockIdx,
					"delta": map[string]interface{}{
						"type":     "thinking_delta",
						"thinking": reasoningContent,
					},
				}))
			}

			// 处理文本内容
			if content, ok := delta["content"].(string); ok && content != "" {
				// thinking→text 切换：关闭 thinking block
				if thinkingBlockIdx >= 0 {
					safeWrite(anthropicSSE("content_block_stop", map[string]interface{}{
						"type": "content_block_stop", "index": thinkingBlockIdx,
					}))
					thinkingBlockIdx = -1
				}
				if textBlockIdx < 0 {
					textBlockIdx = nextBlockIdx
					nextBlockIdx++
					safeWrite(anthropicSSE("content_block_start", map[string]interface{}{
						"type":  "content_block_start",
						"index": textBlockIdx,
						"content_block": map[string]interface{}{
							"type": "text",
							"text": "",
						},
					}))
				}
				safeWrite(anthropicSSE("content_block_delta", map[string]interface{}{
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
							// 关闭前面的 thinking block
							if thinkingBlockIdx >= 0 {
								safeWrite(anthropicSSE("content_block_stop", map[string]interface{}{
									"type": "content_block_stop", "index": thinkingBlockIdx,
								}))
								thinkingBlockIdx = -1
							}
							// 关闭前面的文本 block
							if textBlockIdx >= 0 {
								safeWrite(anthropicSSE("content_block_stop", map[string]interface{}{
									"type": "content_block_stop", "index": textBlockIdx,
								}))
								textBlockIdx = -1
							}
							fnName := ""
							if fn, ok := tcMap["function"].(map[string]interface{}); ok {
								fnName, _ = fn["name"].(string)
							}
							safeWrite(anthropicSSE("content_block_start", map[string]interface{}{
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
								safeWrite(anthropicSSE("content_block_delta", map[string]interface{}{
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
				safeWrite(anthropicSSE("message_delta", map[string]interface{}{
					"type":  "message_delta",
					"delta": map[string]interface{}{"stop_reason": finishReasonToStopReason(fr), "stop_sequence": nil},
					"usage": map[string]interface{}{"output_tokens": outputTokens, "cache_creation_input_tokens": 0, "cache_read_input_tokens": cachedTokens},
				}))
				safeWrite(anthropicSSE("message_stop", map[string]interface{}{
					"type": "message_stop",
				}))
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			log.Printf("SSE scan error: %v", err)
		}
	}
}

// writeSSE 写入 SSE 数据并 flush，返回写入错误
func writeSSE(w http.ResponseWriter, flusher http.Flusher, canFlush bool, data string) error {
	_, err := fmt.Fprint(w, data)
	if canFlush {
		flusher.Flush()
	}
	return err
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
	fmt.Fprintf(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
}
