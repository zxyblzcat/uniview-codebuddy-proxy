import CryptoKit
import Foundation

// ═══════════════════════════════════════════════
// CacheManager — 基于 actor 的内存 TTL 响应缓存
// ═══════════════════════════════════════════════

/// 缓存条目
private struct CacheEntry {
	let data: Data
	let expiration: Date
}

/// 基于 actor 的线程安全 TTL 内存缓存
actor CacheManager {

	// MARK: - 常量

	/// 默认 TTL（秒）
	static let defaultTTL: TimeInterval = 300

	/// 触发惰性清理的条目数阈值
	private static let cleanupThreshold = 10_000

	// MARK: - 属性

	private var store: [String: CacheEntry] = [:]
	private var ttl: TimeInterval
	private var enabled: Bool = false
	private var isCleaningUp: Bool = false

	// MARK: - Init

	init(ttl: TimeInterval = CacheManager.defaultTTL) {
		self.ttl = ttl
	}

	// MARK: - 启用/禁用

	/// 启用或禁用缓存
	func setEnabled(_ enabled: Bool) {
		self.enabled = enabled
		if enabled && store.isEmpty {
			store = [:]
		}
	}

	/// 缓存是否启用
	func isEnabled() -> Bool {
		enabled
	}

	/// 设置缓存 TTL
	func setTTL(_ ttl: TimeInterval) {
		self.ttl = ttl
	}

	// MARK: - 缓存 Key 构建

	/// 根据 model、messages、tools、temperature、maxTokens 构建缓存 key
	/// 使用 SHA-256 哈希保证 key 长度固定且唯一
	static func key(
		model: String,
		messages: Any?,
		tools: Any?,
		temperature: Double,
		maxTokens: Int
	) -> String {
		var hasher = SHA256()
		hasher.update(data: Data(model.utf8))

		if let messages = messages {
			if let data = try? JSONSerialization.data(withJSONObject: messages) {
				hasher.update(data: data)
			}
		}

		if let tools = tools {
			if let data = try? JSONSerialization.data(withJSONObject: tools) {
				hasher.update(data: data)
			}
		}

		hasher.update(data: Data(String(temperature).utf8))
		hasher.update(data: Data(String(maxTokens).utf8))

		let digest = hasher.finalize()
		return digest.compactMap { String(format: "%02x", $0) }.joined()
	}

	// MARK: - Get / Set

	/// 获取缓存条目，过期返回 nil
	func get(key: String) -> Data? {
		guard enabled else { return nil }

		guard let entry = store[key] else {
			return nil
		}

		guard Date() < entry.expiration else {
			// 过期：异步触发清理（actor 内部直接调用即可）
			triggerCleanup()
			return nil
		}

		return entry.data
	}

	/// 存储缓存条目
	func set(key: String, value: Data, ttl: TimeInterval? = nil) {
		guard enabled else { return }

		let effectiveTTL = ttl ?? self.ttl
		let expiration = Date().addingTimeInterval(effectiveTTL)

		store[key] = CacheEntry(data: value, expiration: expiration)

		// 惰性清理：条目数超过阈值时触发
		if store.count > Self.cleanupThreshold {
			triggerCleanup()
		}
	}

	// MARK: - 失效/清空

	/// 使匹配前缀的缓存条目失效
	func invalidate(prefix: String) {
		store = store.filter { !$0.key.hasPrefix(prefix) }
	}

	/// 清空所有缓存
	func clear() {
		store = [:]
	}

	// MARK: - 清理

	/// 触发过期条目清理（防止并发 cleanup）
	private func triggerCleanup() {
		guard !isCleaningUp else { return }
		isCleaningUp = true
		cleanup()
		isCleaningUp = false
	}

	/// 删除所有过期条目
	private func cleanup() {
		let now = Date()
		store = store.filter { now < $0.value.expiration }
	}
}
