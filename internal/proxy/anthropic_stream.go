package proxy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"codebuddy-proxy/internal/auth"
	"codebuddy-proxy/internal/config"
)

// StreamAnthropicMessages 向上游发送请求，将 OpenAI SSE 流实时转换为 Anthropic SSE 流
func StreamAnthropicMessages(payload map[string]interface{}, model string, bearer string, w http.ResponseWriter) {
	msgID := "msg_" + randomHex(24)

	// 状态机变量
	nextBlockIdx := 0       // Anthropic content block 连续 index（从 0 开始递增）
	textBlockIdx := -1      // text block 的 index，-1 表示未开启
	toolBlockIdxMap := map[int]int{} // OpenAI tcIdx → Anthropic block index
	toolBlocksStarted := map[int]bool{} // OpenAI tool_calls index → 是否已发 content_block_start
	inputTokens := 0
	outputTokens := 0
	started := false
	finished := false

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		writeAnthropicSSEError(w, "marshal payload error: "+err.Error())
		return
	}

	req, err := http.NewRequest("POST", config.ChatURL, strings.NewReader(string(payloadJSON)))
	if err != nil {
		writeAnthropicSSEError(w, "create request error: "+err.Error())
		return
	}

	headers := auth.BuildUpstreamHeaders(model)
	if bearer != "" {
		headers["Authorization"] = "Bearer " + bearer
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		writeAnthropicSSEError(w, "upstream request error: "+err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		errText := stripHTML(string(body))
		if len(errText) > 300 {
			errText = errText[:300]
		}
		log.Printf("upstream error %d: %s", resp.StatusCode, errText)
		writeAnthropicSSEError(w, fmt.Sprintf("upstream %d: %s", resp.StatusCode, errText))
		return
	}

	// 设置 SSE 响应头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, canFlush := w.(http.Flusher)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		dataStr := line
		if strings.HasPrefix(line, "data: ") {
			dataStr = line[6:]
		} else if strings.HasPrefix(line, "data:") {
			dataStr = line[5:]
		} else {
			continue
		}

		dataStr = strings.TrimSpace(dataStr)

		if dataStr == "[DONE]" {
			if !finished {
				finished = true
				// 关闭所有已开启的 content block
				if textBlockIdx >= 0 {
					writeSSE(w, flusher, canFlush, anthropicSSE("content_block_stop", map[string]interface{}{
						"type": "content_block_stop", "index": textBlockIdx,
					}))
				}
				for tcIdx, tcStarted := range toolBlocksStarted {
					if tcStarted {
						writeSSE(w, flusher, canFlush, anthropicSSE("content_block_stop", map[string]interface{}{
							"type": "content_block_stop", "index": toolBlockIdxMap[tcIdx],
						}))
					}
				}
				// 发送 message_delta 和 message_stop
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

		if dataStr == "" {
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

					// 首次出现：有 id 和 function.name，发送 content_block_start
					if _, exists := toolBlocksStarted[tcIdx]; !exists {
						toolBlocksStarted[tcIdx] = false
						// 分配 Anthropic block index
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

					// arguments 片段：发送 content_block_delta
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

			// 处理 finish_reason
			if fr != "" && !finished {
				finished = true
				// 关闭所有已开启的 content block
				if textBlockIdx >= 0 {
					writeSSE(w, flusher, canFlush, anthropicSSE("content_block_stop", map[string]interface{}{
						"type": "content_block_stop", "index": textBlockIdx,
					}))
				}
				for tcIdx2, tcStarted2 := range toolBlocksStarted {
					if tcStarted2 {
						writeSSE(w, flusher, canFlush, anthropicSSE("content_block_stop", map[string]interface{}{
							"type": "content_block_stop", "index": toolBlockIdxMap[tcIdx2],
						}))
					}
				}
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
}
