package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	mrand "math/rand/v2"
	"net/http"
	"regexp"
	"strings"
	"time"

	"uniview-codebuddy-proxy/internal/auth"
	"uniview-codebuddy-proxy/internal/config"
	"uniview-codebuddy-proxy/internal/telemetry"
)

const upstreamIdleTimeout = 2 * time.Minute

type idleTimeoutReader struct {
	pr *io.PipeReader
}

func newIdleTimeoutReader(r io.ReadCloser, idle time.Duration) *idleTimeoutReader {
	pr, pw := io.Pipe()
	tr := &idleTimeoutReader{pr: pr}

	go func() {
		defer r.Close()
		defer pw.Close()

		buf := make([]byte, 4096)
		timer := time.NewTimer(idle)
		defer timer.Stop()

		for {
			type readResult struct {
				n   int
				err error
			}
			done := make(chan readResult, 1)
			go func() {
				n, err := r.Read(buf)
				done <- readResult{n, err}
			}()

			select {
			case res := <-done:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(idle)
				if res.n > 0 {
					if _, err := pw.Write(buf[:res.n]); err != nil {
						return
					}
				}
				if res.err != nil {
					pw.CloseWithError(res.err)
					return
				}
			case <-timer.C:
				// 关闭 pipe writer 让读取 goroutine 的后续 r.Read 返回错误
				pw.CloseWithError(fmt.Errorf("upstream stream idle for %v", idle))
				// 排空 done channel 防止读取 goroutine 泄漏
				<-done
				return
			}
		}
	}()

	return tr
}

func (t *idleTimeoutReader) Read(p []byte) (int, error) {
	return t.pr.Read(p)
}

func (t *idleTimeoutReader) Close() error {
	return t.pr.Close()
}

var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

var httpClient = &http.Client{
	Timeout: 0,
	Transport: &http.Transport{
		ResponseHeaderTimeout: 30 * time.Minute,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	},
}

func wrapWithIdleTimeout(body io.ReadCloser) io.ReadCloser {
	return newIdleTimeoutReader(body, upstreamIdleTimeout)
}

func doUpstreamRequest(ctx context.Context, url string, payload map[string]interface{}, model string, bearer string, intent string, extraHeaders map[string]string) (*http.Response, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(payloadJSON))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req = req.WithContext(ctx)

	headers := auth.BuildUpstreamHeaders(model, intent)
	if bearer != "" {
		headers["Authorization"] = "Bearer " + bearer
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	// extraHeaders 中禁止覆盖认证和安全相关头
	protectedHeaders := map[string]bool{
		"Authorization": true, "X-Machine-Id": true, "X-User-Id": true,
		"Content-Type": true, "Host": true,
	}
	for k, v := range extraHeaders {
		if !protectedHeaders[k] {
			req.Header.Set(k, v)
		}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upstream request: %w", err)
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 限制最多读取 1MB
		resp.Body.Close()
		errText := stripHTML(string(body))
		if len(errText) > 300 {
			errText = errText[:300]
		}
		log.Printf("upstream error %d: %s", resp.StatusCode, errText)
		// 标记 token 健康状态
		userID := auth.GetUserID()
		switch resp.StatusCode {
		case 429:
			auth.GetPool().MarkCooldown(userID)
		case 401:
			auth.GetPool().MarkUnavailable(userID)
		}
		return nil, &upstreamError{StatusCode: resp.StatusCode, Message: errText}
	}

	return resp, nil
}

type upstreamError struct {
	StatusCode int
	Message    string
}

func (e *upstreamError) Error() string {
	return fmt.Sprintf("upstream %d: %s", e.StatusCode, e.Message)
}

func parseSSELine(line string) (data string, done bool, ok bool) {
	dataStr := line
	if strings.HasPrefix(line, "data: ") {
		dataStr = line[6:]
	} else if strings.HasPrefix(line, "data:") {
		dataStr = line[5:]
	} else {
		return "", false, false
	}

	dataStr = strings.TrimSpace(dataStr)

	if dataStr == "[DONE]" {
		return "", true, true
	}

	if dataStr == "" {
		return "", false, false
	}

	return dataStr, false, true
}

func StreamChatCompletions(ctx context.Context, payload map[string]interface{}, model string, bearer string, w http.ResponseWriter, conversationID, telemetryRequestID, traceID string, extraHeaders map[string]string) {
	requestID := "chatcmpl-" + randomHex(12)

	resp, err := doUpstreamRequest(ctx, config.ChatURL, payload, model, bearer, "craft", extraHeaders)
	if err != nil {
		if ue, ok := err.(*upstreamError); ok {
			writeSSEError(w, ue.Error())
		} else {
			writeSSEError(w, err.Error())
		}
		return
	}
	defer resp.Body.Close()
	body := wrapWithIdleTimeout(resp.Body)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, canFlush := w.(http.Flusher)

	var writeErr error
	var promptTokens, completionTokens int

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
			// 流结束，上报 chat_message_response 事件
			telemetry.ReportChatResponse(conversationID, telemetryRequestID, model, model, traceID, promptTokens, completionTokens)
			_, writeErr = fmt.Fprintf(w, "data: [DONE]\n\n")
			if canFlush {
				flusher.Flush()
			}
			break
		}

		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
			continue
		}

		// 提取 usage 信息用于上报
		if u, ok := chunk["usage"].(map[string]interface{}); ok {
			if pt, ok := u["prompt_tokens"].(float64); ok {
				promptTokens = int(pt)
			}
			if ct, ok := u["completion_tokens"].(float64); ok {
				completionTokens = int(ct)
			}
		}

		if _, ok := chunk["choices"]; ok {
			chunk["model"] = model
			chunk["id"] = requestID
			cleanChunkChoices(chunk)
		}

		cleaned, err := json.Marshal(chunk)
		if err != nil {
			continue
		}
		_, writeErr = fmt.Fprintf(w, "data: %s\n\n", string(cleaned))
		if canFlush {
			flusher.Flush()
		}
	}
	if err := scanner.Err(); err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			log.Printf("SSE scan error: %v", err)
		}
	}
}

func CollectUpstreamChunks(ctx context.Context, payload map[string]interface{}, bearer string, extraHeaders map[string]string) (*CollectedResult, error) {
	model, _ := payload["model"].(string)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	resp, err := doUpstreamRequest(ctx, config.ChatURL, payload, model, bearer, "craft", extraHeaders)
	if err != nil {
		if ue, ok := err.(*upstreamError); ok {
			return &CollectedResult{StatusCode: ue.StatusCode, ErrorText: ue.Message}, nil
		}
		return nil, err
	}
	defer resp.Body.Close()
	body := wrapWithIdleTimeout(resp.Body)

	result := &CollectedResult{StatusCode: 200}

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		dataStr, done, ok := parseSSELine(line)
		if !ok || done {
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
				if c, ok := delta["content"].(string); ok && c != "" {
					result.ContentParts = append(result.ContentParts, c)
				}
				if rc, ok := delta["reasoning_content"].(string); ok && rc != "" {
					result.ReasoningParts = append(result.ReasoningParts, rc)
				}
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
								result.ToolCalls[idx].Function.Name = name
							}
							if args, ok := fn["arguments"].(string); ok && args != "" {
								result.ToolCalls[idx].Function.Arguments += args
							}
						}
					}
				}
			}
			if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
				result.FinishReason = fr
			}
		}

		if u, ok := chunk["usage"].(map[string]interface{}); ok {
			if pt, ok := u["prompt_tokens"].(float64); ok {
				result.PromptTokens = int(pt)
			}
			if ct, ok := u["completion_tokens"].(float64); ok {
				result.CompletionTokens = int(ct)
			}
			if details, ok := u["prompt_tokens_details"].(map[string]interface{}); ok {
				if ct, ok := details["cached_tokens"].(float64); ok && int(ct) > 0 {
					result.CachedTokens = int(ct)
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			// 客户端断开或请求超时，非上游错误，不标记 502
			return result, nil
		}
		log.Printf("SSE scan error in CollectUpstreamChunks: %v", err)
		// 如果有部分数据但扫描出错，标记为截断响应
		result.StatusCode = 502
		result.ErrorText = fmt.Sprintf("upstream stream error: %v", err)
	}

	return result, nil
}

type CollectedResult struct {
	StatusCode       int
	ErrorText        string
	ContentParts     []string
	ReasoningParts   []string
	ToolCalls        []ToolCall
	FinishReason     string
	PromptTokens     int
	CompletionTokens int
	CachedTokens     int
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

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
		for key := range delta {
			if key != "role" && key != "content" && key != "tool_calls" && key != "reasoning_content" {
				delete(delta, key)
			}
		}
		if tcs, ok := delta["tool_calls"].([]interface{}); ok && len(tcs) == 0 {
			delete(delta, "tool_calls")
		}
		for key := range choice {
			if key != "index" && key != "delta" && key != "finish_reason" {
				delete(choice, key)
			}
		}
		if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
			switch fr {
			case "stop", "tool_calls", "length", "content_filter":
			default:
				choice["finish_reason"] = "stop"
			}
		}
	}
}

func stripHTML(s string) string {
	return strings.TrimSpace(htmlTagRe.ReplaceAllString(s, ""))
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

// estimateInputTokens 基于请求 payload 估算 input_tokens 数量
// 当上游不返回 usage 信息时，使用此估算值替代
// 估算规则：约 1 token ≈ 4 字节（综合中英文场景）
func estimateInputTokens(payload map[string]interface{}) int {
	totalBytes := 0

	// 统计 messages 中的文本字节
	if msgs, ok := payload["messages"].([]interface{}); ok {
		for _, m := range msgs {
			if msg, ok := m.(map[string]interface{}); ok {
				totalBytes += estimateMessageBytes(msg)
			}
		}
	}

	// 统计 tools 定义
	if tools, ok := payload["tools"].([]interface{}); ok {
		for _, t := range tools {
			if tool, ok := t.(map[string]interface{}); ok {
				if b, err := json.Marshal(tool); err == nil {
					totalBytes += len(b)
				}
			}
		}
	}

	tokens := totalBytes / 4
	if tokens == 0 {
		tokens = 1
	}
	return tokens
}

// estimateMessageBytes 估算单条消息的字节大小
func estimateMessageBytes(msg map[string]interface{}) int {
	n := 0
	if content, ok := msg["content"].(string); ok {
		n += len(content)
	} else if contentArr, ok := msg["content"].([]interface{}); ok {
		for _, block := range contentArr {
			n += estimateBlockBytes(block)
		}
	}
	// tool_calls 也计入
	if tcs, ok := msg["tool_calls"].([]interface{}); ok {
		for _, tc := range tcs {
			if b, err := json.Marshal(tc); err == nil {
				n += len(b)
			}
		}
	}
	return n
}

// estimateBlockBytes 估算内容块的字节大小
func estimateBlockBytes(block interface{}) int {
	b, ok := block.(map[string]interface{})
	if !ok {
		return 0
	}
	n := 0
	switch b["type"] {
	case "text":
		if text, ok := b["text"].(string); ok {
			n += len(text)
		}
	case "tool_result":
		if content, ok := b["content"].(string); ok {
			n += len(content)
		} else if contentArr, ok := b["content"].([]interface{}); ok {
			for _, sub := range contentArr {
				n += estimateBlockBytes(sub)
			}
		}
	case "tool_use":
		if serialized, err := json.Marshal(b); err == nil {
			n += len(serialized)
		}
	case "image_url":
		if url, ok := b["image_url"].(map[string]interface{}); ok {
			if u, ok := url["url"].(string); ok {
				n += len(u) / 3
			}
		}
	default:
		if text, ok := b["text"].(string); ok {
			n += len(text)
		}
	}
	return n
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		log.Printf("Warning: crypto/rand.Read failed: %v", err)
		for i := range b {
			b[i] = byte(mrand.IntN(256))
		}
	}
	return hex.EncodeToString(b)
}
