package telemetry

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"uniview-codebuddy-proxy/internal/auth"
	"uniview-codebuddy-proxy/internal/config"
)

const (
	fireDelay    = 2 * time.Second // 批量发送延迟，与官方 CLI 一致
	maxBatchSize = 50              // 单次最大批量
)

// Reporter 事件上报服务（单例）
type Reporter struct {
	mu      sync.Mutex
	events  []Event
	client  *http.Client
	stopCh  chan struct{}
	stopped bool
}

var defaultReporter *Reporter
var once sync.Once

// Init 初始化全局 Reporter 并启动定时发送
func Init() {
	once.Do(func() {
		defaultReporter = &Reporter{
			events: make([]Event, 0, 20),
			client: &http.Client{Timeout: 10 * time.Second},
			stopCh: make(chan struct{}),
		}
		go defaultReporter.fireLoop()
	})
}

// Report 向全局 Reporter 添加一个事件
func Report(eventCode string, data map[string]interface{}) {
	if defaultReporter == nil {
		return
	}
	defaultReporter.add(Event{
		EventCode:   eventCode,
		Timestamp:   time.Now().UnixMilli(),
		ReportDelay: 0,
		Data:        data,
	})
}

// ReportChatRequest 便捷方法：上报 chat_request_send 事件
func ReportChatRequest(conversationID, requestID, modelID, modelName, traceID string, inputLength int) {
	Report(EventChatRequestSend, map[string]interface{}{
		"mode":           "craft",
		"conversationId": conversationID,
		"requestId":      requestID,
		"inputLength":    inputLength,
		"inputType":      "text",
		"modelId":        modelID,
		"modelName":      modelName,
		"traceId":        traceID,
	})
}

// ReportChatResponse 便捷方法：上报 chat_message_response 事件
func ReportChatResponse(conversationID, requestID, modelID, modelName, traceID string, inputToken, outputToken int) {
	Report(EventChatMessageResponse, map[string]interface{}{
		"conversationId": conversationID,
		"requestId":      requestID,
		"inputToken":     inputToken,
		"outputToken":    outputToken,
		"totalToken":     inputToken + outputToken,
		"modelId":        modelID,
		"modelName":      modelName,
		"traceId":        traceID,
	})
}

// ReportCompletionTrigger 便捷方法：上报 completion_trigger 事件
func ReportCompletionTrigger(conversationID, modelID, modelName, traceID string) {
	Report(EventCompletionTrigger, map[string]interface{}{
		"source":         "auto",
		"conversationId": conversationID,
		"modelId":       modelID,
		"modelName":     modelName,
		"traceId":       traceID,
	})
}

// ReportCompletionResponse 便捷方法：上报 completion_response 事件
func ReportCompletionResponse(conversationID, modelID, modelName, traceID string, inputToken, outputToken int) {
	Report(EventCompletionResponse, map[string]interface{}{
		"conversationId": conversationID,
		"inputToken":     inputToken,
		"outputToken":    outputToken,
		"modelId":        modelID,
		"modelName":      modelName,
		"traceId":        traceID,
		"intent":         "inline",
	})
}

// ReportCompletionAccept 便捷方法：上报 completion_action(accept) 事件
func ReportCompletionAccept(conversationID, modelID, modelName, traceID string) {
	Report(EventCompletionAction, map[string]interface{}{
		"action":         ActionAccept,
		"source":         "tab",
		"acceptMode":     "full",
		"conversationId": conversationID,
		"modelId":        modelID,
		"modelName":      modelName,
		"traceId":        traceID,
	})
}

// Shutdown 停止 Reporter，发送剩余事件
func Shutdown() {
	if defaultReporter == nil {
		return
	}
	defaultReporter.mu.Lock()
	if defaultReporter.stopped {
		defaultReporter.mu.Unlock()
		return
	}
	defaultReporter.stopped = true
	defaultReporter.mu.Unlock()

	close(defaultReporter.stopCh)
	defaultReporter.fireBatch() // 发送剩余事件
}

func (r *Reporter) add(event Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return
	}
	r.events = append(r.events, event)
	// 缓冲事件数超过阈值时立即触发发送
	if len(r.events) >= maxBatchSize {
		go r.fireBatch()
	}
}

func (r *Reporter) fireLoop() {
	ticker := time.NewTicker(fireDelay)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.fireBatch()
		case <-r.stopCh:
			return
		}
	}
}

func (r *Reporter) fireBatch() {
	r.mu.Lock()
	if len(r.events) == 0 {
		r.mu.Unlock()
		return
	}
	batch := r.events
	r.events = make([]Event, 0, 20)
	r.mu.Unlock()

	r.send(batch)
}

func (r *Reporter) send(events []Event) {
	if len(events) == 0 {
		return
	}

	// 计算 reportDelay
	now := time.Now().UnixMilli()
	for i := range events {
		events[i].ReportDelay = now - events[i].Timestamp
	}

	payload, err := json.Marshal(events)
	if err != nil {
		log.Printf("telemetry: marshal events error: %v", err)
		return
	}

	req, err := http.NewRequest("POST", config.ReportURL, bytes.NewReader(payload))
	if err != nil {
		log.Printf("telemetry: create request error: %v", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Product", "SaaS")
	req.Header.Set("X-Domain", config.Domain)
	req.Header.Set("User-Agent", "CLI/1.0.8 CodeBuddy/1.0.8")
	if uid := auth.GetUserID(); uid != "" {
		req.Header.Set("X-User-Id", uid)
	}
	bearer := auth.GetBearerToken()
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		log.Printf("telemetry: send events error: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("telemetry: send events returned %d", resp.StatusCode)
	}
}
