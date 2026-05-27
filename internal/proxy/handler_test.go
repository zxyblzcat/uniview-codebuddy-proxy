package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"uniview-codebuddy-proxy/internal/config"
)

// parseSSEEvents 从 httptest ResponseRecorder 中解析所有 SSE 事件
// 返回 []{eventType, data}
func parseSSEEvents(t *testing.T, body string) []struct {
	EventType string
	Data      map[string]interface{}
} {
	t.Helper()
	var events []struct {
		EventType string
		Data      map[string]interface{}
	}
	scanner := bufio.NewScanner(strings.NewReader(body))
	var currentEvent string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			dataStr := strings.TrimPrefix(line, "data: ")
			var data map[string]interface{}
			if err := json.Unmarshal([]byte(dataStr), &data); err == nil {
				events = append(events, struct {
					EventType string
					Data      map[string]interface{}
				}{currentEvent, data})
			}
			currentEvent = ""
		}
	}
	return events
}

// startMockUpstream 启动模拟上游 SSE 服务器
func startMockUpstream(chunks []string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for _, chunk := range chunks {
			w.Write([]byte(chunk + "\n\n"))
		}
		w.Write([]byte("data: [DONE]\n\n"))
	}))
}

// ─── parseSSELine 测试 ───────────────────────────────────────

func TestParseSSELine(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		wantData string
		wantDone bool
		wantOK   bool
	}{
		{"data line", "data: hello", "hello", false, true},
		{"data with space", "data:  world", "world", false, true},
		{"done signal", "data: [DONE]", "", true, true},
		{"non-data line", "event: message_start", "", false, false},
		{"empty data", "data: ", "", false, false},
		{"data without space", "data:test", "test", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, done, ok := parseSSELine(tt.line)
			if data != tt.wantData || done != tt.wantDone || ok != tt.wantOK {
				t.Errorf("parseSSELine(%q) = (%q, %v, %v), want (%q, %v, %v)",
					tt.line, data, done, ok, tt.wantData, tt.wantDone, tt.wantOK)
			}
		})
	}
}

// ─── Anthropic SSE 流翻译测试 ────────────────────────────────

func TestStreamAnthropicMessages_TextOnly(t *testing.T) {
	chunks := []string{
		`data: {"choices":[{"delta":{"content":"Hello"},"finish_reason":""}]}`,
		`data: {"choices":[{"delta":{"content":" World"},"finish_reason":"stop"}]}`,
	}

	srv := startMockUpstream(chunks)
	defer srv.Close()

	// 替换上游 URL
	origChatURL := config.ChatURL
	config.ChatURL = srv.URL
	defer func() { config.ChatURL = origChatURL }()

	w := httptest.NewRecorder()
	StreamAnthropicMessages(context.Background(), map[string]interface{}{"model": "test", "messages": []interface{}{}, "stream": true}, "test", "", w, "conv-test", "req-test", "trace-test", nil)

	events := parseSSEEvents(t, w.Body.String())

	// 应该有: message_start, content_block_start, content_block_delta x2, content_block_stop, message_delta, message_stop
	eventTypes := make([]string, len(events))
	for i, e := range events {
		eventTypes[i] = e.EventType
	}

	// 验证关键事件存在
	hasStart := false
	hasBlockStart := false
	hasDelta := false
	hasBlockStop := false
	hasMsgDelta := false
	hasMsgStop := false
	for _, et := range eventTypes {
		switch et {
		case "message_start":
			hasStart = true
		case "content_block_start":
			hasBlockStart = true
		case "content_block_delta":
			hasDelta = true
		case "content_block_stop":
			hasBlockStop = true
		case "message_delta":
			hasMsgDelta = true
		case "message_stop":
			hasMsgStop = true
		}
	}

	if !hasStart {
		t.Error("missing message_start event")
	}
	if !hasBlockStart {
		t.Error("missing content_block_start event")
	}
	if !hasDelta {
		t.Error("missing content_block_delta event")
	}
	if !hasBlockStop {
		t.Error("missing content_block_stop event")
	}
	if !hasMsgDelta {
		t.Error("missing message_delta event")
	}
	if !hasMsgStop {
		t.Error("missing message_stop event")
	}
}

func TestStreamAnthropicMessages_ToolCalls(t *testing.T) {
	// 模拟：先文本，再两个 tool call
	chunks := []string{
		`data: {"choices":[{"delta":{"content":"I'll help"},"finish_reason":""}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"get_weather","arguments":""}}]},"finish_reason":""}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":"}}]},"finish_reason":""}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Beijing\"}"}}]},"finish_reason":"tool_calls"}]}`,
	}

	srv := startMockUpstream(chunks)
	defer srv.Close()

	origChatURL := config.ChatURL
	config.ChatURL = srv.URL
	defer func() { config.ChatURL = origChatURL }()

	w := httptest.NewRecorder()
	StreamAnthropicMessages(context.Background(), map[string]interface{}{"model": "test", "messages": []interface{}{}, "stream": true}, "test", "", w, "conv-test", "req-test", "trace-test", nil)

	events := parseSSEEvents(t, w.Body.String())

	// 验证 content_block_stop 事件的 index 是递增的
	var blockStopIndices []int
	for _, e := range events {
		if e.EventType == "content_block_stop" {
			if idx, ok := e.Data["index"].(float64); ok {
				blockStopIndices = append(blockStopIndices, int(idx))
			}
		}
	}

	// content_block_stop 的 index 应该是递增的（0, 1）
	for i := 1; i < len(blockStopIndices); i++ {
		if blockStopIndices[i] <= blockStopIndices[i-1] {
			t.Errorf("content_block_stop indices not monotonically increasing: %v", blockStopIndices)
		}
	}

	// 验证 tool_use content_block_start 出现
	hasToolBlockStart := false
	for _, e := range events {
		if e.EventType == "content_block_start" {
			if cb, ok := e.Data["content_block"].(map[string]interface{}); ok {
				if cb["type"] == "tool_use" {
					hasToolBlockStart = true
				}
			}
		}
	}
	if !hasToolBlockStart {
		t.Error("missing tool_use content_block_start event")
	}
}

func TestStreamAnthropicMessages_MultipleToolCalls_Ordered(t *testing.T) {
	// 测试多个 tool call 的 content_block_stop 是否按正确顺序
	chunks := []string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"name":"func_a","arguments":""}}]},"finish_reason":""}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_b","function":{"name":"func_b","arguments":""}}]},"finish_reason":"tool_calls"}]}`,
	}

	srv := startMockUpstream(chunks)
	defer srv.Close()

	origChatURL := config.ChatURL
	config.ChatURL = srv.URL
	defer func() { config.ChatURL = origChatURL }()

	w := httptest.NewRecorder()
	StreamAnthropicMessages(context.Background(), map[string]interface{}{"model": "test", "messages": []interface{}{}, "stream": true}, "test", "", w, "conv-test", "req-test", "trace-test", nil)

	events := parseSSEEvents(t, w.Body.String())

	// 提取所有 content_block_stop 的 index
	var stopIndices []int
	for _, e := range events {
		if e.EventType == "content_block_stop" {
			if idx, ok := e.Data["index"].(float64); ok {
				stopIndices = append(stopIndices, int(idx))
			}
		}
	}

	// 验证按升序排列
	for i := 1; i < len(stopIndices); i++ {
		if stopIndices[i] <= stopIndices[i-1] {
			t.Errorf("content_block_stop indices not ordered: %v", stopIndices)
		}
	}
}


// ─── Chat Completions 流测试 ─────────────────────────────────

func TestStreamChatCompletions_ReplacesModelAndID(t *testing.T) {
	chunks := []string{
		`data: {"id":"upstream-id","object":"chat.completion.chunk","choices":[{"delta":{"content":"Hi"},"finish_reason":""}]}`,
		`data: {"id":"upstream-id","object":"chat.completion.chunk","choices":[{"delta":{},"finish_reason":"stop"}]}`,
	}

	srv := startMockUpstream(chunks)
	defer srv.Close()

	origChatURL := config.ChatURL
	config.ChatURL = srv.URL
	defer func() { config.ChatURL = origChatURL }()

	w := httptest.NewRecorder()
	StreamChatCompletions(context.Background(), map[string]interface{}{"model": "my-model", "messages": []interface{}{}, "stream": true}, "my-model", "", w, "conv-test", "req-test", "trace-test", nil)

	body := w.Body.String()
	// 验证 model 被替换为本地 model
	if !strings.Contains(body, `"my-model"`) {
		t.Errorf("expected model to be replaced with 'my-model', got: %s", body)
	}
	// 验证 id 被替换
	if strings.Contains(body, `"upstream-id"`) {
		t.Errorf("upstream id should be replaced, got: %s", body)
	}
}

// ─── CollectUpstreamChunks 测试 ───────────────────────────────

func TestCollectUpstreamChunks_ToolCallNameNotDuplicated(t *testing.T) {
	// 测试 name 不应该用 += 拼接
	chunks := []string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"get_weather","arguments":""}}]},"finish_reason":""}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"get_weather","arguments":"{\"city\":\"Beijing\"}"}}]},"finish_reason":"tool_calls"}]}`,
	}

	srv := startMockUpstream(chunks)
	defer srv.Close()

	origChatURL := config.ChatURL
	config.ChatURL = srv.URL
	defer func() { config.ChatURL = origChatURL }()

	result, err := CollectUpstreamChunks(context.Background(), map[string]interface{}{"model": "test", "messages": []interface{}{}, "stream": true}, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.ToolCalls) == 0 {
		t.Fatal("expected tool calls")
	}

	name := result.ToolCalls[0].Function.Name
	if name != "get_weather" {
		t.Errorf("tool call name = %q, want %q (should not be duplicated)", name, "get_weather")
	}
}

func TestCollectUpstreamChunks_ArgumentsConcatenated(t *testing.T) {
	chunks := []string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"search","arguments":"{\"q\":"}}]},"finish_reason":""}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"hello\"}"}}]},"finish_reason":""}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	}

	srv := startMockUpstream(chunks)
	defer srv.Close()

	origChatURL := config.ChatURL
	config.ChatURL = srv.URL
	defer func() { config.ChatURL = origChatURL }()

	result, err := CollectUpstreamChunks(context.Background(), map[string]interface{}{"model": "test", "messages": []interface{}{}, "stream": true}, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.ToolCalls) == 0 {
		t.Fatal("expected tool calls")
	}

	args := result.ToolCalls[0].Function.Arguments
	want := `{"q":"hello"}`
	if args != want {
		t.Errorf("tool call arguments = %q, want %q", args, want)
	}
}

// ─── 格式转换函数测试 ─────────────────────────────────────────


func TestConvertAnthropicMessagesToOpenai(t *testing.T) {
	tests := []struct {
		name     string
		system   interface{}
		messages []interface{}
		wantLen  int
	}{
		{
			name:     "string system",
			system:   "You are helpful",
			messages: []interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
			wantLen:  2,
		},
		{
			name:     "no system",
			system:   nil,
			messages: []interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
			wantLen:  1,
		},
		{
			name:   "tool_use in assistant",
			system: nil,
			messages: []interface{}{
				map[string]interface{}{
					"role": "assistant",
					"content": []interface{}{
						map[string]interface{}{"type": "text", "text": "Let me check"},
						map[string]interface{}{"type": "tool_use", "id": "tool_1", "name": "search", "input": map[string]interface{}{"q": "test"}},
					},
				},
			},
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertAnthropicMessagesToOpenai(tt.system, tt.messages)
			if len(result) != tt.wantLen {
				t.Errorf("got %d messages, want %d", len(result), tt.wantLen)
			}
		})
	}
}

func TestFinishReasonToStopReason(t *testing.T) {
	tests := []struct {
		reason string
		want   string
	}{
		{"stop", "end_turn"},
		{"tool_calls", "tool_use"},
		{"length", "max_tokens"},
		{"content_filter", "end_turn"},
		{"unknown", "end_turn"},
	}
	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			got := finishReasonToStopReason(tt.reason)
			if got != tt.want {
				t.Errorf("finishReasonToStopReason(%q) = %q, want %q", tt.reason, got, tt.want)
			}
		})
	}
}

func TestCleanChunkChoices(t *testing.T) {
	chunk := map[string]interface{}{
		"choices": []interface{}{
			map[string]interface{}{
				"index":         float64(0),
				"delta":         map[string]interface{}{"role": "assistant", "content": "hi", "custom_field": "x"},
				"finish_reason": "",
				"logprobs":      "should_be_removed",
			},
		},
	}
	cleanChunkChoices(chunk)

	choice := chunk["choices"].([]interface{})[0].(map[string]interface{})
	delta := choice["delta"].(map[string]interface{})

	// custom_field should be removed from delta
	if _, ok := delta["custom_field"]; ok {
		t.Error("custom_field should be removed from delta")
	}
	// role and content should be kept
	if _, ok := delta["role"]; !ok {
		t.Error("role should be kept in delta")
	}
	if _, ok := delta["content"]; !ok {
		t.Error("content should be kept in delta")
	}
	// logprobs should be removed from choice
	if _, ok := choice["logprobs"]; ok {
		t.Error("logprobs should be removed from choice")
	}
}
