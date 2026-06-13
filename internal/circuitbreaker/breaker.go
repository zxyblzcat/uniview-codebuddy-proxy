package circuitbreaker

import (
	"sync"
	"time"

	"uniview-codebuddy-proxy/internal/config"
)

// State 表示熔断器的状态
type State int

const (
	StateClosed   State = iota // 正常：请求通过，计数失败
	StateOpen                   // 熔断：请求被拒绝，等待超时后进入半开
	StateHalfOpen               // 半开：允许少量试探请求
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

// CircuitBreaker 三态熔断器
// Closed → Open：连续失败次数超过 maxFailures
// Open → HalfOpen：超过 resetTimeout 后自动转换
// HalfOpen → Closed：试探请求成功
// HalfOpen → Open：试探请求失败
type CircuitBreaker struct {
	mu           sync.Mutex
	state        State
	failures     int       // 连续失败计数
	lastFailure  time.Time // 最近一次失败时间
	halfOpenReqs int       // 半开状态下已允许的试探请求数
}

var (
	defaultBreaker *CircuitBreaker
	once           sync.Once
)

// GetBreaker 返回全局熔断器单例
func GetBreaker() *CircuitBreaker {
	once.Do(func() {
		defaultBreaker = &CircuitBreaker{state: StateClosed}
	})
	return defaultBreaker
}

// Allow 检查请求是否被允许通过
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		// 检查是否已超过重置超时
		resetTimeout := time.Duration(config.CBResetTimeoutSecsAtomic()) * time.Second
		if time.Since(cb.lastFailure) > resetTimeout {
			cb.state = StateHalfOpen
			cb.halfOpenReqs = 0
			return true
		}
		return false
	case StateHalfOpen:
		// 半开状态只允许有限数量的试探请求
		maxHalfOpen := int(config.CBMaxFailuresAtomic()) // 用 maxFailures 作为半开试探上限
		if maxHalfOpen < 1 {
			maxHalfOpen = 1
		}
		if cb.halfOpenReqs < maxHalfOpen {
			cb.halfOpenReqs++
			return true
		}
		return false
	default:
		return false
	}
}

// RecordSuccess 记录一次成功响应
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = 0
	cb.state = StateClosed
	cb.halfOpenReqs = 0
}

// RecordFailure 记录一次失败响应
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailure = time.Now()

	maxFailures := int(config.CBMaxFailuresAtomic())
	if maxFailures < 1 {
		maxFailures = 5
	}

	switch cb.state {
	case StateClosed:
		if cb.failures >= maxFailures {
			cb.state = StateOpen
		}
	case StateHalfOpen:
		// 试探失败，重新打开熔断器
		cb.state = StateOpen
		cb.halfOpenReqs = 0
	}
}

// Stats 返回熔断器当前状态
func (cb *CircuitBreaker) Stats() (state State, failures int, lastFailure time.Time) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state, cb.failures, cb.lastFailure
}

// Reset 重置熔断器到 Closed 状态
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = StateClosed
	cb.failures = 0
	cb.halfOpenReqs = 0
	cb.lastFailure = time.Time{}
}
