import Foundation

// ═══════════════════════════════════════════════
// UpstreamClient — 上游 CodeBuddy API HTTP 客户端
// 负责：URLSession 配置、上游头构建、请求执行、
// 空闲超时包装、FNV-128 Machine-Id 生成
// ═══════════════════════════════════════════════

/// 上游请求错误
struct UpstreamError: Error, LocalizedError {
	let statusCode: Int
	let message: String
	var retryAfter: TimeInterval = 0

	var errorDescription: String? {
		return "upstream \(statusCode): \(message)"
	}
}

/// 上游 CodeBuddy API 客户端
final class UpstreamClient {

	// MARK: - 常量

	/// 上游空闲超时（2 分钟）
	private static let idleTimeout: TimeInterval = 120

	/// 最大错误体读取量（1MB）
	private static let maxErrorBodySize = 1 << 20

	/// HTML 标签正则
	private static let htmlTagPattern = try! NSRegularExpression(pattern: "<[^>]*>")

	/// 受保护头（extraHeaders 不可覆盖）
	private static let protectedHeaders: Set<String> = [
		"Authorization", "X-Machine-Id", "X-User-Id",
		"Content-Type", "Host",
	]

	/// 持久化的 Machine-Id（FNV-128a hex）
	let machineId: String

	// MARK: - URLSession

	/// 上游 HTTP 会话（无总超时，30 分钟响应头超时）
	let urlSession: URLSession

	// MARK: - Init

	init(maxConnsPerHost: Int = Defaults.upstreamMaxConnsPerHost) {
		// 生成 Machine-Id
		self.machineId = Self.computeFNV128()

		// 配置 URLSession
		let config = URLSessionConfiguration.ephemeral
		config.timeoutIntervalForRequest = TimeInterval(Defaults.responseHeaderTimeoutSecs) // 30 分钟响应头超时
		config.timeoutIntervalForResource = 0   // 无总超时
		config.httpMaximumConnectionsPerHost = maxConnsPerHost
		config.urlCache = nil
		config.requestCachePolicy = .reloadIgnoringLocalAndRemoteCacheData

		self.urlSession = URLSession(configuration: config)
	}

	// MARK: - 上游头构建

	/// 构建发送到上游的完整请求头
	func buildUpstreamHeaders(
		model: String,
		intent: UpstreamIntent,
		userId: String,
		machineId: String,
		bearerToken: String
	) -> [String: String] {
		let intentStr = intent.rawValue.isEmpty ? "craft" : intent.rawValue
		let rid = generateRequestID()
		let span = generateSpanID()

		var headers: [String: String] = [
			"Accept":            "text/event-stream",
			"Content-Type":      "application/json",
			"b3":                "\(rid)-\(span)-1-",
			"X-B3-TraceId":      rid,
			"X-B3-ParentSpanId": "",
			"X-B3-SpanId":       span,
			"X-B3-Sampled":      "1",
			"X-Agent-Intent":    intentStr,
			"X-Env-ID":          "production",
			"X-Domain":          Upstream.domain,
			"X-Product":         "SaaS",
			"X-User-Id":         userId,
			"X-Machine-Id":      machineId,
			"X-Request-ID":      rid,
			"X-Conversation-ID": generateRequestID(),
			"X-Session-ID":      generateRequestID(),
			"X-IDE-Type":        "CLI",
			"X-Product-Version": Upstream.productVersion,
			"User-Agent":        Upstream.userAgent,
		]

		if !bearerToken.isEmpty {
			headers["Authorization"] = "Bearer \(bearerToken)"
		}

		return headers
	}

	/// 将客户端提供的 extraHeaders 合并到 base 头中
	func mergeExtraHeaders(base: inout [String: String], extra: [String: String]) {
		for (key, value) in extra {
			if Self.protectedHeaders.contains(key) {
				continue
			}
			if key == "anthropic-beta" {
				if let existing = base[key], !existing.isEmpty {
					base[key] = existing + "," + value
				} else {
					base[key] = value
				}
			} else {
				base[key] = value
			}
		}
	}

	// MARK: - 上游请求

	/// 发送 POST 请求到上游，返回 URLSession 字节流
	func doUpstreamRequest(
		payload: [String: Any],
		headers: [String: String]
	) async throws -> (URLSession.AsyncBytes, HTTPURLResponse) {
		let payloadData = try JSONSerialization.data(withJSONObject: payload)

		guard let url = URL(string: Upstream.baseURL + Upstream.chatURL) else {
			throw UpstreamError(statusCode: 500, message: "invalid upstream URL")
		}

		var request = URLRequest(url: url)
		request.httpMethod = "POST"
		request.httpBody = payloadData

		for (key, value) in headers {
			request.setValue(value, forHTTPHeaderField: key)
		}

		let (bytes, response) = try await urlSession.bytes(for: request)

		guard let httpResponse = response as? HTTPURLResponse else {
			throw UpstreamError(statusCode: 500, message: "invalid response type")
		}

		guard httpResponse.statusCode == 200 else {
			// 读取错误体（最多 1MB）
			var body = Data()
			var count = 0
			for try await byte in bytes {
				if count >= Self.maxErrorBodySize { break }
				body.append(byte)
				count += 1
			}

			let errText = Self.stripHTML(String(data: body, encoding: .utf8) ?? "")

			var retryAfter: TimeInterval = 0
			if httpResponse.statusCode == 429 {
				retryAfter = Self.parseRetryAfterHeader(httpResponse.allHeaderFields)
			}

			throw UpstreamError(
				statusCode: httpResponse.statusCode,
				message: String(errText.prefix(300)),
				retryAfter: retryAfter
			)
		}

		return (bytes, httpResponse)
	}

	/// 执行上游请求并用空闲超时包装响应体
	func doUpstreamRequestWithIdleTimeout(
		payload: [String: Any],
		headers: [String: String]
	) async throws -> (IdleTimeoutLines, HTTPURLResponse) {
		let (bytes, response) = try await doUpstreamRequest(payload: payload, headers: headers)
		let wrapped = IdleTimeoutLines(source: bytes.lines, idleTimeout: Self.idleTimeout)
		return (wrapped, response)
	}

	/// 收集上游所有 chunk（用于非流式请求）
	func collectUpstreamChunks(
		payload: [String: Any],
		bearer: String,
		timeout: TimeInterval
	) -> UpstreamCollectedResult {
		let sem = DispatchSemaphore(value: 0)
		var result = UpstreamCollectedResult()

		Task {
			do {
				let headers = buildUpstreamHeaders(
					model: payload["model"] as? String ?? "",
					intent: .craft,
					userId: "",
					machineId: machineId,
					bearerToken: bearer
				)

				let (bytes, response) = try await doUpstreamRequest(payload: payload, headers: headers)
				result.statusCode = response.statusCode

				guard response.statusCode == 200 else {
					var body = Data()
					var bodyCount = 0
					for try await byte in bytes {
						body.append(byte)
						bodyCount += 1
						if bodyCount >= Self.maxErrorBodySize { break }
					}
					result.errorText = Self.stripHTML(String(data: body, encoding: .utf8) ?? "")
					sem.signal()
					return
				}

				var contentParts: [String] = []
				var promptTokens = 0
				var completionTokens = 0

				for try await line in bytes.lines {
					let trimmed = line.trimmingCharacters(in: .whitespaces)
					guard trimmed.hasPrefix("data: "), trimmed != "data: [DONE]" else { continue }
					let jsonStr = String(trimmed.dropFirst(6))
					guard let data = jsonStr.data(using: .utf8),
						  let chunk = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { continue }

					// 提取 content
					if let choices = chunk["choices"] as? [[String: Any]],
					   let delta = choices.first?["delta"] as? [String: Any],
					   let content = delta["content"] as? String, !content.isEmpty {
						contentParts.append(content)
					}

					// 提取 usage
					if let usage = chunk["usage"] as? [String: Any] {
						promptTokens = usage["prompt_tokens"] as? Int ?? promptTokens
						completionTokens = usage["completion_tokens"] as? Int ?? completionTokens
					}
				}

				result.contentParts = contentParts
				result.promptTokens = promptTokens
				result.completionTokens = completionTokens
			} catch {
				result.statusCode = 500
				result.errorText = error.localizedDescription
			}
			sem.signal()
		}

		let timeoutSecs = Int(timeout)
		_ = sem.wait(timeout: .now() + .seconds(timeoutSecs))

		return result
	}

	/// 获取 Machine-Id
	func getMachineId() -> String {
		return machineId
	}

	// MARK: - FNV-128a Hash

	/// 基于 hostname + homeDir 生成稳定的 FNV-128a 哈希作为 Machine-Id
	private static func computeFNV128() -> String {
		let hostname = Host.current().localizedName ?? "unknown-host"
		let homeDir = FileManager.default.homeDirectoryForCurrentUser.path
		let seed = hostname + "|" + homeDir

		// FNV-128a 参数（与 Go hash/fnv 一致）
		let offsetBasis: [UInt64] = [0x6295c58d, 0x07bb0142, 0x62b82175, 0x6c62272e]
		let prime: [UInt64] = [0x0000013b, 0x00000000, 0x00000000, 0x00000001]

		var hash = offsetBasis

		for byte in seed.utf8 {
			// hash ^= byte (XOR into least significant limb)
			hash[3] ^= UInt64(byte)

			// hash *= prime (128-bit multiplication using 4 x 64-bit limbs, little-endian)
			var carry: UInt64 = 0
			var newHash: [UInt64] = [0, 0, 0, 0]

			for i in 0..<4 {
				var product = hash[i] &* prime[0]
				var lo = product &+ carry
				carry = (product < lo) ? 1 : 0
				carry += (product >> 63) >> 1  // handle overflow
				newHash[i] = lo

				// Cross terms with higher prime limbs
				for j in 1..<4 {
					if i >= j {
						product = hash[i - j] &* prime[j]
						var sum = newHash[i] &+ product
						if sum < newHash[i] { carry &+= 1 }
						newHash[i] = sum
					}
				}
			}

			hash = newHash
		}

		// 转为十六进制字符串（大端序输出）
		let hex = String(format: "%016llx%016llx%016llx%016llx",
						 hash[3], hash[2], hash[1], hash[0])
		return hex
	}

	// MARK: - ID 生成

	/// 生成 16 字节随机请求 ID（32 hex chars）
	private func generateRequestID() -> String {
		var bytes = [UInt8](repeating: 0, count: 16)
		_ = SecRandomCopyBytes(kSecRandomDefault, 16, &bytes)
		return bytes.map { String(format: "%02x", $0) }.joined()
	}

	/// 生成 8 字节随机 Span ID（16 hex chars）
	private func generateSpanID() -> String {
		var bytes = [UInt8](repeating: 0, count: 8)
		_ = SecRandomCopyBytes(kSecRandomDefault, 8, &bytes)
		return bytes.map { String(format: "%02x", $0) }.joined()
	}

	// MARK: - 工具方法

	/// 去除 HTML 标签
	private static func stripHTML(_ s: String) -> String {
		let range = NSRange(s.startIndex..., in: s)
		let result = htmlTagPattern.stringByReplacingMatches(
			in: s,
			options: [],
			range: range,
			withTemplate: ""
		)
		return result.trimmingCharacters(in: .whitespaces)
	}

	/// 解析 Retry-After 头
	private static func parseRetryAfterHeader(_ headers: [AnyHashable: Any]) -> TimeInterval {
		guard let value = headers["Retry-After"] as? String ?? headers["retry-after"] as? String,
			  !value.isEmpty else {
			return 0
		}

		// 尝试解析为秒数
		if let seconds = Double(value), seconds > 0 {
			return min(seconds, 60) // 上限 60s
		}

		// 尝试解析为 HTTP 日期
		let formatter = DateFormatter()
		formatter.dateFormat = "E, dd MMM yyyy HH:mm:ss z"
		if let date = formatter.date(from: value) {
			let interval = date.timeIntervalSinceNow
			return interval > 0 ? min(interval, 60) : 0
		}

		return 0
	}
}

// ═══════════════════════════════════════════════
// IdleTimeoutLines — 2 分钟空闲超时行序列包装器
// ═══════════════════════════════════════════════

/// 带空闲超时的行序列包装
struct IdleTimeoutLines: AsyncSequence {
	typealias Element = String

	let source: AsyncLineSequence<URLSession.AsyncBytes>
	let idleTimeout: TimeInterval

	/// 异步迭代器
	struct Iterator: AsyncIteratorProtocol {
		private let source: AsyncLineSequence<URLSession.AsyncBytes>
		private let idleTimeout: TimeInterval
		private let stream: AsyncStream<String>
		private var streamIterator: AsyncStream<String>.Iterator
		private let backgroundTask: Task<Void, Never>

		init(source: AsyncLineSequence<URLSession.AsyncBytes>, idleTimeout: TimeInterval) {
			self.source = source
			self.idleTimeout = idleTimeout

			let (localStream, continuation) = AsyncStream<String>.makeStream(of: String.self)

			self.stream = localStream
			self.streamIterator = localStream.makeAsyncIterator()

			self.backgroundTask = Task {
				var lastActivity = Date()
				let timeoutTask = Task {
					while !Task.isCancelled {
						try? await Task.sleep(nanoseconds: 1_000_000_000)
						if Date().timeIntervalSince(lastActivity) >= idleTimeout {
							continuation.finish()
							return
						}
					}
				}

				do {
					for try await line in source {
						lastActivity = Date()
						continuation.yield(line)
					}
				} catch {
					// 上游错误，直接结束
				}
				timeoutTask.cancel()
				continuation.finish()
			}
		}

		mutating func next() async -> String? {
			return await streamIterator.next()
		}
	}

	func makeAsyncIterator() -> Iterator {
		Iterator(source: source, idleTimeout: idleTimeout)
	}
}

// ═══════════════════════════════════════════════
// UpstreamCollectedResult — 上游 chunk 收集结果
// ═══════════════════════════════════════════════

struct UpstreamCollectedResult {
	var statusCode: Int = 0
	var errorText: String = ""
	var contentParts: [String] = []
	var promptTokens: Int = 0
	var completionTokens: Int = 0
}
