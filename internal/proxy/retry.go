package proxy

import (
	"context"
	"errors"

	"log"
	mrand "math/rand/v2"
	"net/http"
	"strings"
	"time"

	"uniview-codebuddy-proxy/internal/auth"
	"uniview-codebuddy-proxy/internal/circuitbreaker"
	"uniview-codebuddy-proxy/internal/config"
	"uniview-codebuddy-proxy/internal/telemetry"
)

// isRetryable 判断 HTTP 状态码是否可以重试
// 429 (限流)、502 (网关错误)、503 (服务不可用)、504 (网关超时)
// 注意：401 (认证失败) 不重试，因为重试同样会失败
func isRetryable(code int) bool {
	return code == 429 || code == 502 || code == 503 || code == 504
}

// computeBackoff 计算指数退避延迟
// base = 2^attempt * 100ms，加上随机抖动（0~500ms）
// 如果服务器返回 Retry-After 头，取 max(computed, retryAfter)
func computeBackoff(attempt int, retryAfter time.Duration) time.Duration {
	// 限制位移量防止溢出（1<<30 已远超 30s 上限）
	shift := attempt
	if shift > 30 {
		shift = 30
	}
	// 指数退避：100ms, 200ms, 400ms, 800ms...（1<<shift * 100ms）
	base := time.Duration(1<<shift) * 100 * time.Millisecond

	// 加随机抖动 (0~500ms)
	jitter := time.Duration(mrand.IntN(500)) * time.Millisecond
	computed := base + jitter

	// 上限 30s
	if computed > 30*time.Second {
		computed = 30 * time.Second
	}

	// 如果服务器指定了 Retry-After，取较大值（但不超过 60s 上限，防止恶意上游卡住代理）
	if retryAfter > 0 {
		if retryAfter > 60*time.Second {
			retryAfter = 60 * time.Second
		}
		if retryAfter > computed {
			return retryAfter
		}
	}

	return computed
}

// doUpstreamRequestWithRetry 带熔断器和重试机制的上游请求
// 调用 doUpstreamRequest，在遇到可重试错误时自动重试
// 重试前会刷新 bearer token（token 池可能已轮换）
func doUpstreamRequestWithRetry(ctx context.Context, url string, payload map[string]interface{}, model string, bearer string, intent string, extraHeaders map[string]string) (*http.Response, error) {
	maxRetries := config.MaxRetriesAtomic()
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// 检查熔断器
		if !circuitbreaker.GetBreaker().Allow() {
		telemetry.ReportUpstreamFailure(model, 503, 0, maxRetries, "circuit breaker open")
			return nil, &upstreamError{
				StatusCode: 503,
				Message:    "circuit breaker open: upstream service unavailable",
			}
		}

		resp, err := doUpstreamRequest(ctx, url, payload, model, bearer, intent, extraHeaders)
		if err == nil {
			circuitbreaker.GetBreaker().RecordSuccess()
			return resp, nil
		}

		lastErr = err

		var ue *upstreamError
		if !errors.As(err, &ue) {
			// 非 upstreamError（如网络错误），直接返回
			circuitbreaker.GetBreaker().RecordFailure()
		telemetry.ReportUpstreamFailure(model, 0, attempt+1, maxRetries, err.Error())
			return nil, err
		}

		// 不可重试的状态码，直接返回
		if !isRetryable(ue.StatusCode) {
		telemetry.ReportUpstreamFailure(model, ue.StatusCode, attempt+1, maxRetries, ue.Message)
			return nil, err
		}

		// 记录失败到熔断器
		circuitbreaker.GetBreaker().RecordFailure()

		// 已达最大重试次数，不再重试
		if attempt >= maxRetries {
			log.Printf("retry: max retries (%d) exhausted for %s %s", maxRetries, model, url)
		telemetry.ReportUpstreamFailure(model, ue.StatusCode, attempt+1, maxRetries, ue.Message)
			return nil, err
		}

		// 计算退避延迟
		delay := computeBackoff(attempt, ue.RetryAfter)

		log.Printf("retry: attempt %d/%d failed (status %d), retrying in %v: %s",
			attempt+1, maxRetries+1, ue.StatusCode, delay, truncateStr(ue.Message, 100))
		telemetry.ReportUpstreamRetry(model, ue.StatusCode, attempt+1, maxRetries, delay.Milliseconds())

		// 等待退避延迟或上下文取消
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}

		// 刷新 bearer token（token 池可能已轮换）
		bearer = auth.GetBearerToken()
		if bearer == "" {
			return nil, &upstreamError{
				StatusCode: 401,
				Message:    "no available bearer token after retry",
			}
		}
	}

	return nil, lastErr
}

// truncateStr 截断字符串到指定长度（按 rune 截断，避免截断 UTF-8 多字节字符）
func truncateStr(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// contextLimitPatterns 上下文窗口限制错误的关键词模式
// 提取为包级变量避免每次调用重新分配
var contextLimitPatterns = []string{
	"context length",
	"context window limit",
	"context_window_limit",
	"token limit exceeded",
	"prompt is too long",
	"exceeds the maximum context",
	"maximum context length",
	"too many tokens for",
	"reduce the prompt length",
	"input is too long",
	"input length exceeds",
}

// isContextLimitError 检测上游错误是否为上下文窗口限制错误
// 用于上下文自动压缩（autocompact）触发
func isContextLimitError(errText string) bool {
	lower := strings.ToLower(errText)
	for _, p := range contextLimitPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}
