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
	"time"

	"uniview-codebuddy-proxy/internal/config"
	"uniview-codebuddy-proxy/internal/telemetry"
)

// StreamAnthropicMessages 向上游发送请求，将 OpenAI SSE 流实时转换为 Anthropic SSE 流
// 返回 TTFB（从发起上游请求到收到首个有效 SSE 数据的时间）
func StreamAnthropicMessages(ctx context.Context, payload map[string]interface{}, model string, bearer string, w http.ResponseWriter, conversationID, telemetryRequestID, traceID string, extraHeaders map[string]string) time.Duration {
	var ttfb time.Duration
	upstreamStart := time.Now()
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

	resp, err := doUpstreamRequest(ctx, config.ChatURL, payload, model, bearer, "craft", extraHeaders)
	if err != nil {
		ttfb = time.Since(upstreamStart)
		if ue, ok := err.(*upstreamError); ok {
			writeAnthropicSSEError(w, ue.Error())
		} else {
			writeAnthropicSSEError(w, err.Error())
		}
		return ttfb
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

	// emitThinkingSignatureDelta 发送 thinking block 的 signature_delta 事件
	// Anthropic SSE 协议要求在 content_block_stop 之前发送 signature_delta
	emitThinkingSignatureDelta := func(blockIdx int) {
		safeWrite(anthropicSSE("content_block_delta", map[string]interface{}{
			"type":  "content_block_delta",
			"index": blockIdx,
			"delta": map[string]interface{}{
				"type":      "signature_delta",
				"signature": "",
			},
		}))
	}

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	// closeOpenBlocks 关闭所有已开启的 content block
	closeOpenBlocks := func() {
		if thinkingBlockIdx >= 0 {
			emitThinkingSignatureDelta(thinkingBlockIdx)
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

	// 直接使用上游返回的 prompt_tokens 作为 input_tokens
	// 上游的 usage 在流的最后（finish_reason chunk）才提供真实 prompt_tokens，
	// message_start 阶段 inputTokens 为 0，在 message_delta 中补充真实值

	for scanner.Scan() {
		// 客户端断连或写入失败时停止
		select {
		case <-ctx.Done():
			return ttfb
		default:
		}
		if writeErr != nil {
			return ttfb
		}

		line := scanner.Text()
		if line == "" {
			continue
		}

		dataStr, done, ok := parseSSELine(line)
		if !ok {
			continue
		}

		// 首次解析到有效 SSE 数据时记录 TTFB
		if ttfb == 0 {
			ttfb = time.Since(upstreamStart)
		}
		if done {
			if !finished {
				finished = true
				closeOpenBlocks()
				if config.DebugEnabledAtomic() {
					log.Printf("[DEBUG] compact: [DONE] input_tokens=%d cached=%d model=%s", inputTokens, cachedTokens, model)
				}
				safeWrite(anthropicSSE("message_delta", map[string]interface{}{
					"type":  "message_delta",
					"delta": map[string]interface{}{"stop_reason": "end_turn", "stop_sequence": nil},
					"usage": map[string]interface{}{"input_tokens": inputTokens, "output_tokens": outputTokens},
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
				if config.DebugEnabledAtomic() {
					log.Printf("[DEBUG] compact: message_start input_tokens=%d cached=%d model=%s", inputTokens, cachedTokens, model)
				}
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
						"usage":         map[string]interface{}{"input_tokens": inputTokens, "output_tokens": 1, "cache_creation_input_tokens": 0, "cache_read_input_tokens": cachedTokens},
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
				// thinking→text 切换：关闭 thinking block（先发送 signature_delta）
				if thinkingBlockIdx >= 0 {
					emitThinkingSignatureDelta(thinkingBlockIdx)
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
						// 首次出现此 tool_call index：先关闭前面的 thinking/text block
						// 必须在 id 检查之前执行，否则无 id 的 tool_call chunk 不会关闭前面的 block
						if thinkingBlockIdx >= 0 {
							emitThinkingSignatureDelta(thinkingBlockIdx)
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

						if id, ok := tcMap["id"].(string); ok && id != "" {
							toolBlocksStarted[tcIdx] = true
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
				if config.DebugEnabledAtomic() {
					log.Printf("[DEBUG] compact: finish_reason=%s input_tokens=%d cached=%d model=%s", fr, inputTokens, cachedTokens, model)
				}
				safeWrite(anthropicSSE("message_delta", map[string]interface{}{
					"type":  "message_delta",
					"delta": map[string]interface{}{"stop_reason": finishReasonToStopReason(fr), "stop_sequence": nil},
					"usage": map[string]interface{}{"input_tokens": inputTokens, "output_tokens": outputTokens},
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
		// 向客户端发送流截断通知，避免客户端无限等待
		if !finished {
			closeOpenBlocks()
			safeWrite(anthropicSSE("message_delta", map[string]interface{}{
				"type":  "message_delta",
				"delta": map[string]interface{}{"stop_reason": "stop_sequence", "stop_sequence": "<stream_error>"},
				"usage": map[string]interface{}{"input_tokens": inputTokens, "output_tokens": outputTokens},
			}))
			safeWrite(anthropicSSE("message_stop", map[string]interface{}{
				"type": "message_stop",
			}))
		}
	}
	// 上报 chat_message_response 事件（直接使用上游返回的 prompt_tokens）
	telemetry.ReportChatResponse(conversationID, telemetryRequestID, model, model, traceID, inputTokens, outputTokens)
	return ttfb
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
