import Foundation
import os

// ═══════════════════════════════════════════════
// RetryHandler — 上游请求重试处理器
// 指数退避 + Retry-After 尊重 + 熔断器感知
// 仅对 429/502/503/504 进行重试
// 每次重试前刷新 Bearer Token
// 上报 upstream_retry 和 upstream_failure 遥测事件
// ═══════════════════════════════════════════════

/// 重试错误类型
enum RetryError: Error, LocalizedError {
	case circuitOpen
	case rateLimited(retryAfter: TimeInterval)
	case unauthorized
	case serverError(Int)
	case upstreamError(Int, String)
	case maxRetriesExceeded
	case noBearerToken

	var errorDescription: String? {
		switch self {
		case .circuitOpen:
			return "circuit breaker is open: upstream service unavailable"
		case .rateLimited(let after):
			return "rate limited, retry after \(after)s"
		case .unauthorized:
			return "authentication failed"
		case .serverError(let code):
			return "server error \(code)"
		case .upstreamError(let code, let msg):
			return "upstream error \(code): \(msg)"
		case .maxRetriesExceeded:
			return "max retries exceeded"
		case .noBearerToken:
			return "no available bearer token after retry"
		}
	}

	/// 是否可重试
	var isRetryable: Bool {
		switch self {
		case .rateLimited, .serverError:
			return true
		case .circuitOpen, .unauthorized, .upstreamError, .maxRetriesExceeded, .noBearerToken:
			return false
		}
	}
}

/// 上游请求重试处理器
final class RetryHandler {

	// MARK: - 依赖

	private let upstreamClient: UpstreamClient
	private let circuitBreaker: CircuitBreaker
	private let tokenManager: TokenManager
	private let telemetryReporter: TelemetryReporter

	// MARK: - 配置

	private let maxRetries: Int

	// MARK: - 常量

	/// 退避基础间隔（100ms）
	private static let baseBackoff: TimeInterval = 0.1

	/// 最大退避间隔（30s）
	private static let maxBackoff: TimeInterval = 30.0

	/// 最大抖动范围（0~500ms）
	private static let maxJitter: TimeInterval = 0.5

	/// Retry-After 上限（60s）
	private static let maxRetryAfter: TimeInterval = 60.0

	/// 可重试的状态码
	private static let retryableStatusCodes: Set<Int> = [429, 502, 503, 504]

	// MARK: - Init

	init(
		upstreamClient: UpstreamClient,
		circuitBreaker: CircuitBreaker,
		tokenManager: TokenManager,
		telemetryReporter: TelemetryReporter,
		maxRetries: Int = Defaults.maxRetries
	) {
		self.upstreamClient = upstreamClient
		self.circuitBreaker = circuitBreaker
		self.tokenManager = tokenManager
		self.telemetryReporter = telemetryReporter
		self.maxRetries = max(0, maxRetries)
	}

	// MARK: - 执行

	/// 执行带重试的上游请求
	/// - Parameters:
	///   - payload: 上游请求体
	///   - headers: 上游请求头
	///   - model: 模型名称（用于遥测）
	///   - transform: 将上游 SSE 流转换为最终结果的闭包
	/// - Returns: 转换后的结果
	func execute<T>(
		payload: [String: Any],
		headers: [String: String],
		model: String = "",
		transform: (URLSession.AsyncBytes, HTTPURLResponse) async throws -> T
	) async throws -> T {
		var lastError: Error?
		var currentHeaders = headers

		for attempt in 0...maxRetries {
			// 熔断器检查
			if !circuitBreaker.allowRequest() {
				telemetryReporter.reportUpstreamFailure(
					model: model,
					statusCode: 503,
					attempt: attempt + 1,
					maxRetries: maxRetries,
					errMsg: "circuit breaker open"
				)
				throw RetryError.circuitOpen
			}

			do {
				let (bytes, response) = try await upstreamClient.doUpstreamRequest(
					payload: payload,
					headers: currentHeaders
				)

				let statusCode = response.statusCode

				if (200...299).contains(statusCode) {
					// 成功
					circuitBreaker.recordSuccess()
					return try await transform(bytes, response)
				}

				// 读取错误体（最多 1MB）
				var body = Data()
				var count = 0
				for try await byte in bytes {
					if count >= (1 << 20) { break }
					body.append(byte)
					count += 1
				}
				let errorBody = String(data: body, encoding: .utf8) ?? "unknown error"

				// 记录失败
				circuitBreaker.recordFailure()

				// 判断是否可重试
				if Self.retryableStatusCodes.contains(statusCode) {
					var retryAfter: TimeInterval = 0
					if statusCode == 429 {
						retryAfter = parseRetryAfter(from: response)
						// 标记 token 冷却
						let userId = currentHeaders["X-User-Id"] ?? ""
						await MainActor.run {
							tokenManager.markCooldown(
								userID: userId,
								duration: max(retryAfter, 30)
							)
						}
					}

					lastError = UpstreamError(
						statusCode: statusCode,
						message: String(errorBody.prefix(300)),
						retryAfter: retryAfter
					)

					if attempt >= maxRetries {
						os_log(.info, "retry: max retries (%d) exhausted for %s",
							   maxRetries, model)
						telemetryReporter.reportUpstreamFailure(
							model: model,
							statusCode: statusCode,
							attempt: attempt + 1,
							maxRetries: maxRetries,
							errMsg: String(errorBody.prefix(100))
						)
						throw lastError!
					}

					// 计算退避
					let delay = computeBackoff(attempt: attempt, retryAfter: retryAfter)

					telemetryReporter.reportUpstreamRetry(
						model: model,
						statusCode: statusCode,
						attempt: attempt + 1,
						maxRetries: maxRetries,
						delayMs: Int64(delay * 1000)
					)

					os_log(.info, "retry: attempt %d/%d failed (status %d), retrying in %.1fs",
						   attempt + 1, maxRetries + 1, statusCode, delay)

					// 等待退避
					try await Task.sleep(nanoseconds: UInt64(delay * 1_000_000_000))

					// 刷新 Bearer Token
					let newBearer = await MainActor.run {
						tokenManager.nextToken()?.bearer
					}
					if let bearer = newBearer, !bearer.isEmpty {
						currentHeaders["Authorization"] = "Bearer \(bearer)"
					} else {
						telemetryReporter.reportUpstreamFailure(
							model: model,
							statusCode: 401,
							attempt: attempt + 1,
							maxRetries: maxRetries,
							errMsg: "no available bearer token after retry"
						)
						throw RetryError.noBearerToken
					}

					continue
				}

				// 不可重试的错误（401 等）
				if statusCode == 401 {
					let userId = currentHeaders["X-User-Id"] ?? ""
					await MainActor.run {
						tokenManager.markUnavailable(userID: userId)
					}
				}

				telemetryReporter.reportUpstreamFailure(
					model: model,
					statusCode: statusCode,
					attempt: attempt + 1,
					maxRetries: maxRetries,
					errMsg: String(errorBody.prefix(100))
				)

				throw UpstreamError(
					statusCode: statusCode,
					message: String(errorBody.prefix(300))
				)

			} catch let error as UpstreamError {
				// UpstreamError 兜底处理
				lastError = error
				circuitBreaker.recordFailure()

				if !Self.retryableStatusCodes.contains(error.statusCode) {
					telemetryReporter.reportUpstreamFailure(
						model: model,
						statusCode: error.statusCode,
						attempt: attempt + 1,
						maxRetries: maxRetries,
						errMsg: error.message
					)
					throw error
				}

				if attempt >= maxRetries {
					telemetryReporter.reportUpstreamFailure(
						model: model,
						statusCode: error.statusCode,
						attempt: attempt + 1,
						maxRetries: maxRetries,
						errMsg: error.message
					)
					throw error
				}

				let delay = computeBackoff(attempt: attempt, retryAfter: error.retryAfter)
				telemetryReporter.reportUpstreamRetry(
					model: model,
					statusCode: error.statusCode,
					attempt: attempt + 1,
					maxRetries: maxRetries,
					delayMs: Int64(delay * 1000)
				)

				try await Task.sleep(nanoseconds: UInt64(delay * 1_000_000_000))

				// 刷新 Token
				let newBearer = await MainActor.run {
					tokenManager.nextToken()?.bearer
				}
				if let bearer = newBearer, !bearer.isEmpty {
					currentHeaders["Authorization"] = "Bearer \(bearer)"
				} else {
					throw RetryError.noBearerToken
				}

			} catch {
				// 网络错误等 — 可重试
				lastError = error
				circuitBreaker.recordFailure()

				telemetryReporter.reportUpstreamFailure(
					model: model,
					statusCode: 0,
					attempt: attempt + 1,
					maxRetries: maxRetries,
					errMsg: error.localizedDescription
				)

				if attempt >= maxRetries {
					throw error
				}

				let delay = computeBackoff(attempt: attempt, retryAfter: 0)
				telemetryReporter.reportUpstreamRetry(
					model: model,
					statusCode: 0,
					attempt: attempt + 1,
					maxRetries: maxRetries,
					delayMs: Int64(delay * 1000)
				)

				try await Task.sleep(nanoseconds: UInt64(delay * 1_000_000_000))
			}
		}

		throw lastError ?? RetryError.maxRetriesExceeded
	}

	// MARK: - 退避计算

	/// 计算指数退避 + 随机抖动
	/// 公式: 100ms * 2^attempt + random(0, 500ms)，上限 30s
	/// 若 Retry-After > computed（且 <= 60s），则使用 Retry-After
	private func computeBackoff(attempt: Int, retryAfter: TimeInterval) -> TimeInterval {
		let shift = min(attempt, 30) // 防止溢出
		let base = Self.baseBackoff * Double(1 << shift)
		let jitter = Double.random(in: 0...Self.maxJitter)
		var computed = base + jitter

		if computed > Self.maxBackoff {
			computed = Self.maxBackoff
		}

		if retryAfter > 0 {
			let cappedRetryAfter = min(retryAfter, Self.maxRetryAfter)
			if cappedRetryAfter > computed {
				return cappedRetryAfter
			}
		}

		return computed
	}

	// MARK: - 工具方法

	/// 从响应头解析 Retry-After
	private func parseRetryAfter(from response: HTTPURLResponse) -> TimeInterval {
		if let value = response.value(forHTTPHeaderField: "Retry-After") ?? response.value(forHTTPHeaderField: "retry-after"),
		   !value.isEmpty {
			// 尝试解析为秒数
			if let seconds = Double(value), seconds > 0 {
				return min(seconds, Self.maxRetryAfter)
			}
			// 尝试解析为 HTTP 日期
			let formatter = DateFormatter()
			formatter.dateFormat = "E, dd MMM yyyy HH:mm:ss z"
			if let date = formatter.date(from: value) {
				let interval = date.timeIntervalSinceNow
				return interval > 0 ? min(interval, Self.maxRetryAfter) : 0
			}
		}
		return 0
	}
}

// MARK: - 可重试状态码判断

/// 判断 HTTP 状态码是否可重试
/// 仅 429, 502, 503, 504 可重试
func isRetryable(code: Int) -> Bool {
	return [429, 502, 503, 504].contains(code)
}
