package proxy

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"codebuddy-proxy/internal/auth"
	"codebuddy-proxy/internal/config"
)

// upstreamIdleTimeout 上游 SSE 流读取空闲超时
// 上游停止发送数据但保持连接时，防止 goroutine 无限挂起
const upstreamIdleTimeout = 2 * time.Minute

// idleTimeoutReader 使用 io.Pipe 在后台 goroutine 中读取，
// 每次成功读取后重置定时器，超时时关闭 pipe 使 scanner 解除阻塞
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
			// 使用 channel 竞争：读取完成 vs 超时
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
					// timer 已触发，但读取成功完成，忽略过期信号
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(idle)
				// 先写入数据，再检查错误
				// io.Reader 可以在一次 Read 中同时返回 n>0 和 err（如 io.EOF），
				// 必须先写数据再关闭 pipe，否则最后一批数据会丢失
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
				pw.CloseWithError(fmt.Errorf("upstream stream idle for %v", idle))
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

// htmlTagRe 匹配 HTML 标签
var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

// httpClient 用于所有上游请求（无总超时，支持长思考模型）
var httpClient = &http.Client{
	Timeout: 0, // 无总超时，模型思考可能很久
	Transport: &http.Transport{
		ResponseHeaderTimeout: 30 * time.Minute, // 等待响应头的超时（推理模型思考可能很久）
	},
}

// wrapWithIdleTimeout 包装响应体，添加读取空闲超时
// 防止上游半开连接导致 goroutine 泄漏
func wrapWithIdleTimeout(body io.ReadCloser) io.ReadCloser {
	return newIdleTimeoutReader(body, upstreamIdleTimeout)
}

// doUpstreamRequest 构建上游请求并发送，返回响应
// 上游非 200 时读取错误体并返回 (nil, upstreamError)；其他错误返回 (nil, err)
// 调用方负责关闭 resp.Body 和处理错误
func doUpstreamRequest(ctx context.Context, payload map[string]interface{}, model string, bearer string) (*http.Response, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", config.ChatURL, strings.NewReader(string(payloadJSON)))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req = req.WithContext(ctx)

	headers := auth.BuildUpstreamHeaders(model)
	if bearer != "" {
		headers["Authorization"] = "Bearer " + bearer
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upstream request: %w", err)
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		errText := stripHTML(string(body))
		if len(errText) > 300 {
			errText = errText[:300]
		}
		log.Printf("upstream error %d: %s", resp.StatusCode, errText)
		return nil, &upstreamError{StatusCode: resp.StatusCode, Message: errText}
	}

	return resp, nil
}

// upstreamError 表示上游返回的非 200 错误
type upstreamError struct {
	StatusCode int
	Message    string
}

func (e *upstreamError) Error() string {
	return fmt.Sprintf("upstream %d: %s", e.StatusCode, e.Message)
}

// parseSSELine 解析 SSE 行，返回 (data, done, ok)
// ok=false 表示非数据行应跳过，done=true 表示收到 [DONE]
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

// StreamChatCompletions 向上游发送流式请求并实时转发 SSE 事件
// 清理非标准字段，替换 model 和 id
func StreamChatCompletions(ctx context.Context, payload map[string]interface{}, model string, bearer string, w http.ResponseWriter) {
	requestID := "chatcmpl-" + randomHex(12)

	resp, err := doUpstreamRequest(ctx, payload, model, bearer)
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

	// 设置 SSE 响应头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, canFlush := w.(http.Flusher)

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

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
			fmt.Fprintf(w, "data: [DONE]\n\n")
			if canFlush {
				flusher.Flush()
			}
			continue
		}

		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
			continue
		}

		// 替换 model 和 id（始终替换为本地生成的 id）
		if _, ok := chunk["choices"]; ok {
			chunk["model"] = model
			chunk["id"] = requestID
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
	if err := scanner.Err(); err != nil {
		log.Printf("SSE scan error: %v", err)
	}
}

// CollectUpstreamChunks 从上游收集所有流式 chunk，返回结构化数据
func CollectUpstreamChunks(ctx context.Context, payload map[string]interface{}, bearer string) (*CollectedResult, error) {
	model, _ := payload["model"].(string)

	// 为非流式路径设置 10 分钟总超时
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	resp, err := doUpstreamRequest(ctx, payload, model, bearer)
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
								result.ToolCalls[idx].Function.Name = name
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
	if err := scanner.Err(); err != nil {
		log.Printf("SSE scan error: %v", err)
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
		// 只保留 delta 中的 role, content, tool_calls
		for key := range delta {
			if key != "role" && key != "content" && key != "tool_calls" {
				delete(delta, key)
			}
		}
		// 清理空的 tool_calls 数组
		if tcs, ok := delta["tool_calls"].([]interface{}); ok && len(tcs) == 0 {
			delete(delta, "tool_calls")
		}
		// 清理 choice 层级非标准字段（logprobs 等）
		for key := range choice {
			if key != "index" && key != "delta" && key != "finish_reason" {
				delete(choice, key)
			}
		}
		// 保留已知的 finish_reason，未知值替换为 stop
		if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
			switch fr {
			case "stop", "tool_calls", "length", "content_filter":
				// 已知合法值，保留原样
			default:
				choice["finish_reason"] = "stop"
			}
		}
	}
}

// stripHTML 移除 HTML 标签，返回纯文本
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

// randomHex 生成指定长度的随机十六进制字符串
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand.Read failed: %v", err))
	}
	return hex.EncodeToString(b)
}
