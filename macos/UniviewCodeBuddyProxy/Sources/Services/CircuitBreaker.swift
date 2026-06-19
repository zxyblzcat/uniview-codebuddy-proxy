import Foundation

// ═══════════════════════════════════════════════
// CircuitBreaker — 三态熔断器
// Closed → Open:   连续失败次数 >= maxFailures
// Open   → HalfOpen: 经过 resetTimeout 后自动转换
// HalfOpen → Closed: 试探请求成功
// HalfOpen → Open:   试探请求失败
// 默认: maxFailures=5, resetTimeout=30s
// 线程安全: 使用 NSLock 保护状态
// ═══════════════════════════════════════════════

/// 熔断器状态
enum CircuitBreakerState: String, CustomStringConvertible {
	case closed
	case open
	case halfOpen

	var description: String { rawValue }
}

/// 三态熔断器
/// 使用 NSLock 保证线程安全，支持从任何队列/线程调用
final class CircuitBreaker {

	// MARK: - 配置

	let maxFailures: Int
	let resetTimeout: TimeInterval

	// MARK: - 状态

	private var _state: CircuitBreakerState = .closed
	private var _failureCount: Int = 0
	private var _lastFailureTime: Date = .distantPast
	private var _halfOpenProbes: Int = 0

	/// 线程安全锁
	private let lock = NSLock()

	// MARK: - Init

	init(maxFailures: Int = Defaults.cbMaxFailures, resetTimeout: TimeInterval = TimeInterval(Defaults.cbResetTimeoutSecs)) {
		self.maxFailures = max(1, maxFailures)
		self.resetTimeout = max(1, resetTimeout)
	}

	// MARK: - 状态访问

	/// 当前有效状态（考虑时间自动转换）
	var currentState: CircuitBreakerState {
		lock.lock()
		defer { lock.unlock() }

		return effectiveState()
	}

	/// 状态描述（用于 API 显示）
	var stateDescription: String {
		currentState.description
	}

	/// 连续失败计数
	var failureCount: Int {
		lock.lock()
		defer { lock.unlock() }
		return _failureCount
	}

	/// 最近一次失败时间
	var lastFailureTime: Date {
		lock.lock()
		defer { lock.unlock() }
		return _lastFailureTime
	}

	// MARK: - 请求控制

	/// 检查请求是否被允许通过
	/// - Closed: 始终允许
	/// - Open: 检查是否超过 resetTimeout，若是则转为 HalfOpen 并允许
	/// - HalfOpen: 允许最多 maxFailures 个试探请求
	func allowRequest() -> Bool {
		lock.lock()
		defer { lock.unlock() }

		switch effectiveState() {
		case .closed:
			return true

		case .open:
			// Open 状态下不允许任何请求
			// effectiveState() 已处理 Open→HalfOpen 转换
			return false

		case .halfOpen:
			// 半开状态允许有限数量的试探请求
			let maxProbes = max(1, maxFailures)
			if _halfOpenProbes < maxProbes {
				_halfOpenProbes += 1
				return true
			}
			return false
		}
	}

	// MARK: - 结果记录

	/// 记录一次成功响应
	/// - HalfOpen → Closed
	/// - 重置所有计数器
	func recordSuccess() {
		lock.lock()
		defer { lock.unlock() }

		_failureCount = 0
		_state = .closed
		_halfOpenProbes = 0
	}

	/// 记录一次失败响应
	/// - Closed: 递增 failureCount，达到 maxFailures 则转为 Open
	/// - HalfOpen: 直接转为 Open
	func recordFailure() {
		lock.lock()
		defer { lock.unlock() }

		_failureCount += 1
		_lastFailureTime = Date()

		switch effectiveState() {
		case .closed:
			if _failureCount >= maxFailures {
				_state = .open
			}

		case .halfOpen:
			// 试探失败，重新打开熔断器
			_state = .open
			_halfOpenProbes = 0

		case .open:
			// 已在 Open 状态，更新时间
			break
		}
	}

	// MARK: - 重置

	/// 强制重置到 Closed 状态
	func reset() {
		lock.lock()
		defer { lock.unlock() }

		_state = .closed
		_failureCount = 0
		_halfOpenProbes = 0
		_lastFailureTime = .distantPast
	}

	// MARK: - 统计

	/// 返回熔断器当前状态快照
	func stats() -> (state: CircuitBreakerState, failures: Int, lastFailure: Date) {
		lock.lock()
		defer { lock.unlock() }

		return (effectiveState(), _failureCount, _lastFailureTime)
	}

	// MARK: - 私有方法

	/// 计算有效状态（考虑 Open→HalfOpen 的时间转换）
	/// 调用者必须持有 lock
	private func effectiveState() -> CircuitBreakerState {
		if _state == .open {
			let elapsed = Date().timeIntervalSince(_lastFailureTime)
			if elapsed >= resetTimeout {
				// 自动转为半开状态
				_state = .halfOpen
				_halfOpenProbes = 0
				return .halfOpen
			}
			return .open
		}
		return _state
	}
}
