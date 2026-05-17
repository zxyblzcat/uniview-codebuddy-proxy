package proxy

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"codebuddy-proxy/internal/auth"
	"codebuddy-proxy/internal/config"
)

// StreamChatCompletions 向上游发送流式请求并实时转发 SSE 事件
// 清理非标准字段，替换 model 和 id
func StreamChatCompletions(payload map[string]interface{}, model string, w http.ResponseWriter) {
	requestID := "chatcmpl-" + randomHex(12)
	bearer := auth.GetBearerToken()

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		writeSSEError(w, "marshal payload error: "+err.Error())
		return
	}

	req, err := http.NewRequest("POST", config.ChatURL, strings.NewReader(string(payloadJSON)))
	if err != nil {
		writeSSEError(w, "create request error: "+err.Error())
		return
	}

	// 设置请求头
	headers := auth.BuildUpstreamHeaders(model)
	headers["Authorization"] = "Bearer " + bearer
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeSSEError(w, "upstream request error: "+err.Error())
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
		writeSSEError(w, fmt.Sprintf("upstream %d: %s", resp.StatusCode, errText))
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
			fmt.Fprintf(w, "data: [DONE]\n\n")
			if canFlush {
				flusher.Flush()
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

		// 替换 model 和 id
		if _, ok := chunk["choices"]; ok {
			chunk["model"] = model
			if chunk["id"] == nil || chunk["id"] == "" {
				chunk["id"] = requestID
			}
			// 清理上游非标准字段
			cleanChunkChoices(chunk)
		}

		cleaned, err := json.Marshal(chunk)
		if err != nil {
			continue
		}
		fmt.Fprintf(w, "data: %s\n\n", string(cleaned))
		if canFlush {
			flusher.Flush()
		}
	}
}

// CollectUpstreamChunks 从上游收集所有流式 chunk，返回结构化数据
func CollectUpstreamChunks(payload map[string]interface{}) (*CollectedResult, error) {
	bearer := auth.GetBearerToken()

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", config.ChatURL, strings.NewReader(string(payloadJSON)))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// 从 payload 提取 model 用于构建 headers
	model, _ := payload["model"].(string)
	headers := auth.BuildUpstreamHeaders(model)
	headers["Authorization"] = "Bearer " + bearer
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upstream request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		errText := string(body)
		if len(errText) > 300 {
			errText = errText[:300]
		}
		return &CollectedResult{StatusCode: resp.StatusCode, ErrorText: errText}, nil
	}

	result := &CollectedResult{StatusCode: 200}

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
		if dataStr == "" || dataStr == "[DONE]" {
			continue
		}

		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
			continue
		}

		choices, _ := chunk["choices"].([]interface{})
		for _, ch := range choices {
			choice, _ := ch.(map[string]interface{})
			if choice == nil {
				continue
			}
			delta, _ := choice["delta"].(map[string]interface{})
			if delta != nil {
				// 文本内容
				if c, ok := delta["content"].(string); ok && c != "" {
					result.ContentParts = append(result.ContentParts, c)
				}
				// 工具调用
				if tcs, ok := delta["tool_calls"].([]interface{}); ok {
					for _, tc := range tcs {
						tcMap, _ := tc.(map[string]interface{})
						if tcMap == nil {
							continue
						}
						idx := getIntFromMap(tcMap, "index")
						for len(result.ToolCalls) <= idx {
							result.ToolCalls = append(result.ToolCalls, ToolCall{
								ID:   "",
								Type: "function",
								Function: FunctionCall{
									Name:      "",
									Arguments: "",
								},
							})
						}
						if id, ok := tcMap["id"].(string); ok && id != "" {
							result.ToolCalls[idx].ID = id
						}
						if fn, ok := tcMap["function"].(map[string]interface{}); ok {
							if name, ok := fn["name"].(string); ok && name != "" {
								result.ToolCalls[idx].Function.Name += name
							}
							if args, ok := fn["arguments"].(string); ok && args != "" {
								result.ToolCalls[idx].Function.Arguments += args
							}
						}
					}
				}
			}
			// finish_reason
			if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
				result.FinishReason = fr
			}
		}

		// usage
		if u, ok := chunk["usage"].(map[string]interface{}); ok {
			if pt, ok := u["prompt_tokens"].(float64); ok {
				result.PromptTokens = int(pt)
			}
			if ct, ok := u["completion_tokens"].(float64); ok {
				result.CompletionTokens = int(ct)
			}
		}
	}

	return result, nil
}

// CollectedResult 从上游收集的完整响应数据
type CollectedResult struct {
	StatusCode       int
	ErrorText        string
	ContentParts     []string
	ToolCalls        []ToolCall
	FinishReason     string
	PromptTokens     int
	CompletionTokens int
}

// ToolCall 表示 OpenAI 格式的工具调用
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall 表示工具调用的函数信息
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// cleanChunkChoices 清理上游返回的非标准字段
func cleanChunkChoices(chunk map[string]interface{}) {
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
		// 只保留 role, content, tool_calls
		for key := range delta {
			if key != "role" && key != "content" && key != "tool_calls" {
				delete(delta, key)
			}
		}
		// 清理空的 tool_calls 数组
		if tcs, ok := delta["tool_calls"].([]interface{}); ok && len(tcs) == 0 {
			delete(delta, "tool_calls")
		}
		// 非标准 finish_reason 替换为 stop
		if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
			if fr != "stop" && fr != "tool_calls" {
				choice["finish_reason"] = "stop"
			}
		}
	}
}

func writeSSEError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	errJSON, _ := json.Marshal(map[string]interface{}{
		"error": map[string]string{"message": msg, "type": "upstream_error"},
	})
	fmt.Fprintf(w, "data: %s\n\n", string(errJSON))
	fmt.Fprintf(w, "data: [DONE]\n\n")
}

func getIntFromMap(m map[string]interface{}, key string) int {
	v, _ := m[key].(float64)
	return int(v)
}

// randomHex 生成指定长度的随机十六进制字符串
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
