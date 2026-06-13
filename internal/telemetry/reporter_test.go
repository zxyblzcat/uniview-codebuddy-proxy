package telemetry

import (
	"sync"
	"testing"

	"uniview-codebuddy-proxy/internal/config"
)

func TestTelemetryEnabledSwitch(t *testing.T) {
	// 保存原始状态
	origVal := config.TelemetryEnabledAtomic()
	defer config.SetTelemetryEnabled(origVal)

	// 确保 reporter 已初始化
	Init()

	tests := []struct {
		name      string
		enabled   bool
		wantAdded bool
	}{
		{
			name:      "开关关闭时 Report 不应添加事件",
			enabled:   false,
			wantAdded: false,
		},
		{
			name:      "开关开启时 Report 应添加事件",
			enabled:   true,
			wantAdded: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config.SetTelemetryEnabled(tt.enabled)

			// 记录添加前的缓冲区长度
			defaultReporter.mu.Lock()
			beforeLen := len(defaultReporter.events)
			defaultReporter.mu.Unlock()

			// 调用 Report
			Report("test_event", map[string]interface{}{
				"test_key": "test_value",
			})

			// 检查缓冲区变化
			defaultReporter.mu.Lock()
			afterLen := len(defaultReporter.events)
			defaultReporter.mu.Unlock()

			added := afterLen > beforeLen
			if added != tt.wantAdded {
				t.Errorf("开关=%v: 期望事件添加=%v, 实际=%v (before=%d, after=%d)",
					tt.enabled, tt.wantAdded, added, beforeLen, afterLen)
			}
		})
	}
}

func TestTelemetryEnabledSwitchConvenience(t *testing.T) {
	// 保存原始状态
	origVal := config.TelemetryEnabledAtomic()
	defer config.SetTelemetryEnabled(origVal)

	// 确保 reporter 已初始化
	Init()

	// 测试关闭时，便捷方法也不应添加事件
	config.SetTelemetryEnabled(false)

	defaultReporter.mu.Lock()
	beforeLen := len(defaultReporter.events)
	defaultReporter.mu.Unlock()

	ReportChatRequest("conv-1", "req-1", "glm-5.1", "glm-5.1", "trace-1", 100)
	ReportChatResponse("conv-1", "req-1", "glm-5.1", "glm-5.1", "trace-1", 50, 30)
	ReportResponsesRequest("conv-2", "req-2", "glm-5.1", "glm-5.1", "trace-2", 200)
	ReportResponsesResponse("conv-2", "req-2", "glm-5.1", "glm-5.1", "trace-2", 100, 80)
	ReportUpstreamRetry("glm-5.1", 429, 1, 3, 500)
	ReportUpstreamFailure("glm-5.1", 500, 3, 3, "timeout")
	ReportImageUnderstandingSuccess("image_url")
	ReportImageUnderstandingFailure("image", "timeout")

	defaultReporter.mu.Lock()
	afterLen := len(defaultReporter.events)
	defaultReporter.mu.Unlock()

	if afterLen != beforeLen {
		t.Errorf("开关关闭时便捷方法不应添加事件: before=%d, after=%d", beforeLen, afterLen)
	}

	// 测试开启时，便捷方法应添加事件
	config.SetTelemetryEnabled(true)

	defaultReporter.mu.Lock()
	beforeLen = len(defaultReporter.events)
	defaultReporter.mu.Unlock()

	ReportChatRequest("conv-3", "req-3", "glm-5.1", "glm-5.1", "trace-3", 100)

	defaultReporter.mu.Lock()
	afterLen = len(defaultReporter.events)
	defaultReporter.mu.Unlock()

	if afterLen <= beforeLen {
		t.Errorf("开关开启时便捷方法应添加事件: before=%d, after=%d", beforeLen, afterLen)
	}
}

func TestTelemetryEnabledAtomicConsistency(t *testing.T) {
	// 测试 atomic 读写一致性
	origVal := config.TelemetryEnabledAtomic()
	defer config.SetTelemetryEnabled(origVal)

	config.SetTelemetryEnabled(true)
	if !config.TelemetryEnabledAtomic() {
		t.Error("SetTelemetryEnabled(true) 后 TelemetryEnabledAtomic() 应返回 true")
	}

	config.SetTelemetryEnabled(false)
	if config.TelemetryEnabledAtomic() {
		t.Error("SetTelemetryEnabled(false) 后 TelemetryEnabledAtomic() 应返回 false")
	}
}

func TestTelemetryEnabledConcurrent(t *testing.T) {
	// 并发读写测试
	origVal := config.TelemetryEnabledAtomic()
	defer config.SetTelemetryEnabled(origVal)

	Init()

	var wg sync.WaitGroup
	const goroutines = 50

	// 并发切换开关
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			config.SetTelemetryEnabled(n%2 == 0)
		}(i)
	}

	// 并发调用 Report
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Report("concurrent_test", map[string]interface{}{"key": "val"})
		}()
	}

	wg.Wait()
	// 不检查具体结果，只确保没有 race condition（需用 -race 运行验证）
}

