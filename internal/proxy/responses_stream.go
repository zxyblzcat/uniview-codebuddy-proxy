package proxy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"codebuddy-proxy/internal/auth"
	"codebuddy-proxy/internal/config"
)

// StreamResponsesSSE 向上游发送请求，将 OpenAI Chat SSE 流实时转换为 Responses API SSE 流
func StreamResponsesSSE(payload map[string]interface{}, model string, w http.ResponseWriter) {
	respID := "resp_" + randomHex(24)
	itemID := "msg_" + randomHex(24)
	started := false
	finished := false
	textStarted := false
	inputTokens := 0
	outputTokens := 0

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		writeResponsesSSEError(w, "marshal payload error: "+err.Error())
		return
	}

	req, err := http.NewRequest("POST", config.ChatURL, strings.NewReader(string(payloadJSON)))
	if err != nil {
		writeResponsesSSEError(w, "create request error: "+err.Error())
		return
	}

	headers := auth.BuildUpstreamHeaders(model)
	headers["Authorization"] = "Bearer " + auth.GetBearerToken()
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeResponsesSSEError(w, "upstream request error: "+err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		errText := string(body)
		if len(errText) > 300 {
			errText = errText[:300]
		}
		log.Printf("upstream error %d: %s", resp.StatusCode, errText)
		writeResponsesSSEError(w, fmt.Sprintf("upstream %d: %s", resp.StatusCode, errText))
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
				if textStarted {
					textStarted = false
					writeSSE(w, flusher, canFlush, responsesSSE("response.output_text.delta", map[string]interface{}{
						"type":          "response.output_text.delta",
						"item_id":       itemID,
						"output_index":  0,
						"content_index": 0,
						"delta":         "",
					}))
					writeSSE(w, flusher, canFlush, responsesSSE("response.content_part.done", map[string]interface{}{
						"type":          "response.content_part.done",
						"item_id":       itemID,
						"output_index":  0,
						"content_index": 0,
						"part":          map[string]interface{}{"type": "output_text", "text": "", "annotations": []interface{}{}},
					}))
					writeSSE(w, flusher, canFlush, responsesSSE("response.output_item.done", map[string]interface{}{
						"type":         "response.output_item.done",
						"output_index": 0,
						"item":         map[string]interface{}{"id": itemID, "type": "message", "role": "assistant", "content": []interface{}{map[string]interface{}{"type": "output_text", "text": "", "annotations": []interface{}{}}}},
					}))
				}
				writeSSE(w, flusher, canFlush, responsesSSE("response.completed", map[string]interface{}{
					"type": "response.completed",
					"response": map[string]interface{}{
						"id":         respID,
						"object":     "response",
						"created_at": time.Now().Unix(),
						"model":      model,
						"status":     "completed",
						"output":     []interface{}{},
						"usage":      map[string]interface{}{"input_tokens": inputTokens, "output_tokens": outputTokens, "total_tokens": inputTokens + outputTokens},
					},
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

			// 首次收到有效内容时发送开始事件
			if !started {
				started = true
				textStarted = true
				writeSSE(w, flusher, canFlush, responsesSSE("response.created", map[string]interface{}{
					"type": "response.created",
					"response": map[string]interface{}{
						"id": respID, "object": "response", "created_at": time.Now().Unix(), "model": model, "status": "in_progress", "output": []interface{}{},
					},
				}))
				writeSSE(w, flusher, canFlush, responsesSSE("response.in_progress", map[string]interface{}{
					"type": "response.in_progress",
					"response": map[string]interface{}{
						"id": respID, "object": "response", "created_at": time.Now().Unix(), "model": model, "status": "in_progress", "output": []interface{}{},
					},
				}))
				writeSSE(w, flusher, canFlush, responsesSSE("response.output_item.added", map[string]interface{}{
					"type":         "response.output_item.added",
					"output_index": 0,
					"item":         map[string]interface{}{"id": itemID, "type": "message", "role": "assistant", "content": []interface{}{}},
				}))
				writeSSE(w, flusher, canFlush, responsesSSE("response.content_part.added", map[string]interface{}{
					"type":          "response.content_part.added",
					"item_id":       itemID,
					"output_index":  0,
					"content_index": 0,
					"part":          map[string]interface{}{"type": "output_text", "text": ""},
				}))
			}

			// 处理文本内容
			if content, ok := delta["content"].(string); ok && content != "" {
				writeSSE(w, flusher, canFlush, responsesSSE("response.output_text.delta", map[string]interface{}{
					"type":          "response.output_text.delta",
					"item_id":       itemID,
					"output_index":  0,
					"content_index": 0,
					"delta":         content,
				}))
			}

			// 处理 finish_reason
			if fr != "" && !finished {
				finished = true
				textStarted = false
				writeSSE(w, flusher, canFlush, responsesSSE("response.output_text.done", map[string]interface{}{
					"type":          "response.output_text.done",
					"item_id":       itemID,
					"output_index":  0,
					"content_index": 0,
					"text":          "",
				}))
				writeSSE(w, flusher, canFlush, responsesSSE("response.content_part.done", map[string]interface{}{
					"type":          "response.content_part.done",
					"item_id":       itemID,
					"output_index":  0,
					"content_index": 0,
					"part":          map[string]interface{}{"type": "output_text", "text": "", "annotations": []interface{}{}},
				}))
				writeSSE(w, flusher, canFlush, responsesSSE("response.output_item.done", map[string]interface{}{
					"type":         "response.output_item.done",
					"output_index": 0,
					"item":         map[string]interface{}{"id": itemID, "type": "message", "role": "assistant", "content": []interface{}{map[string]interface{}{"type": "output_text", "text": "", "annotations": []interface{}{}}}},
				}))
				writeSSE(w, flusher, canFlush, responsesSSE("response.completed", map[string]interface{}{
					"type": "response.completed",
					"response": map[string]interface{}{
						"id":         respID,
						"object":     "response",
						"created_at": time.Now().Unix(),
						"model":      model,
						"status":     "completed",
						"output":     []interface{}{},
						"usage":      map[string]interface{}{"input_tokens": inputTokens, "output_tokens": outputTokens, "total_tokens": inputTokens + outputTokens},
					},
				}))
			}
		}
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
}
