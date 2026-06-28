import os
import Combine
import Foundation

// ═══════════════════════════════════════════════
// TelemetryReporter — 批量事件上报服务
// 按固定间隔或批量大小上报事件到 /v2/report 端点
// ═══════════════════════════════════════════════

/// 事件 code 常量
enum EventCode {
	static let chatRequestSend      = "chat_request_send"
	static let chatMessageResponse  = "chat_message_response"
	static let completionTrigger    = "completion_trigger"
	static let completionResponse   = "completion_response"
	static let completionAction     = "completion_action"
	static let responsesRequestSend    = "responses_request_send"
	static let responsesMessageResponse = "responses_message_response"
	static let upstreamRetry        = "upstream_retry"
	static let upstreamFailure      = "upstream_failure"
}

/// Completion action 值
enum CompletionAction {
	static let accept = "accept"
	static let reject = "reject"
}

/// 待上报的事件
struct TelemetryEvent {
	let eventCode: String
	let timestamp: Date
	let model: String?
	let properties: [String: Any]

	/// 序列化为上报 JSON 格式
	func toJSON(reportDelay: Int64) -> [String: Any] {
		var json: [String: Any] = [
			"eventCode": eventCode,
			"timestamp": Int64(timestamp.timeIntervalSince1970 * 1000),
			"reportDelay": reportDelay,
		]
		if let model = model {
			json["model"] = model
		}
		if !properties.isEmpty {
			json["data"] = properties
		}
		return json
	}
}

/// 批量事件上报服务
final class TelemetryReporter: ObservableObject {

	// MARK: - 常量

	/// 批量发送间隔（秒）
	private static let fireDelay: TimeInterval = 2.0

	/// 单次最大批量
	private static let maxBatchSize = 50

	/// HTTP 请求超时（秒）
	private static let requestTimeout: TimeInterval = 10

	// MARK: - 依赖

	private let configManager: ConfigManager
	private let tokenManager: TokenManager

	// MARK: - 状态

	@Published private(set) var isEnabled: Bool = false
	private var events: [TelemetryEvent] = []
	private var fireTimer: Timer?
	private var isStopped = false
	private let urlSession: URLSession

	// MARK: - Init

	init(configManager: ConfigManager, tokenManager: TokenManager) {
		self.configManager = configManager
		self.tokenManager = tokenManager
		self.isEnabled = configManager.telemetryEnabled

		let config = URLSessionConfiguration.default
		config.timeoutIntervalForRequest = Self.requestTimeout
		self.urlSession = URLSession(configuration: config)

		// 监听配置变更
		setupConfigObserver()

		// 启动定时发送
		startFireLoop()
	}

	/// 无参数便利初始化器（用于 @StateObject 等需要无参构造的场景）
	@MainActor
	convenience init() {
		self.init(configManager: ConfigManager(), tokenManager: TokenManager())
	}
	


	deinit {
		stopFireLoop()
	}

	// MARK: - 配置监听

	private func setupConfigObserver() {
		configManager.$telemetryEnabled
			.receive(on: DispatchQueue.main)
			.sink { [weak self] enabled in
				self?.isEnabled = enabled
			}
			.store(in: &cancellables)
	}

	private var cancellables = Set<AnyCancellable>()

	// MARK: - 定时发送

	/// 启动 fireLoop 定时器
	private func startFireLoop() {
		stopFireLoop()

		DispatchQueue.main.async { [weak self] in
			guard let self = self else { return }
			self.fireTimer = Timer.scheduledTimer(
				withTimeInterval: Self.fireDelay,
				repeats: true
			) { [weak self] _ in
				self?.fireBatch()
			}
		}
	}

	/// 停止 fireLoop 定时器
	private func stopFireLoop() {
		DispatchQueue.main.async { [weak self] in
			self?.fireTimer?.invalidate()
			self?.fireTimer = nil
		}
	}

	// MARK: - 事件添加

	/// 通用事件添加入口
	func report(eventCode: String, model: String? = nil, properties: [String: Any] = [:]) {
		guard configManager.telemetryEnabled else { return }
		guard !isStopped else { return }

		let event = TelemetryEvent(
			eventCode: eventCode,
			timestamp: Date(),
			model: model,
			properties: properties
		)

		DispatchQueue.global(qos: .utility).async { [weak self] in
			self?.addEvent(event)
		}
	}

	private func addEvent(_ event: TelemetryEvent) {
		objc_sync_enter(self)
		defer { objc_sync_exit(self) }

		guard !isStopped else { return }

		events.append(event)

		// 缓冲事件数超过阈值时立即触发发送
		if events.count >= Self.maxBatchSize {
			fireBatch()
		}
	}

	// MARK: - 批量发送

	/// 交换缓冲区并发送
	private func fireBatch() {
		let batch: [TelemetryEvent]
		objc_sync_enter(self)
		guard !events.isEmpty else {
			objc_sync_exit(self)
			return
		}
		batch = events
		events = []
		objc_sync_exit(self)

		send(batch)
	}

	/// 发送一批事件到 /v2/report
	private func send(_ events: [TelemetryEvent], userID: String? = nil, bearerToken: String? = nil) {
		guard !events.isEmpty else { return }

		// 计算 reportDelay
		let now = Int64(Date().timeIntervalSince1970 * 1000)

		let payloadArray = events.map { event in
			let eventTimestamp = Int64(event.timestamp.timeIntervalSince1970 * 1000)
			let reportDelay = now - eventTimestamp
			return event.toJSON(reportDelay: reportDelay)
		}

		guard let payloadData = try? JSONSerialization.data(withJSONObject: payloadArray) else {
			os_log("telemetry: marshal events error", log: .default, type: .error)
			return
		}

		// 构建请求
		let reportURL = Upstream.baseURL + Upstream.reportURL
		guard let url = URL(string: reportURL) else { return }

		var request = URLRequest(url: url)
		request.httpMethod = "POST"
		request.httpBody = payloadData
		request.setValue("application/json", forHTTPHeaderField: "Content-Type")
		request.setValue("SaaS", forHTTPHeaderField: "X-Product")
		request.setValue(Upstream.domain, forHTTPHeaderField: "X-Domain")
		request.setValue(Upstream.userAgent, forHTTPHeaderField: "User-Agent")

		// 设置认证头
		if let userID = userID, !userID.isEmpty {
			request.setValue(userID, forHTTPHeaderField: "X-User-Id")
		}
		if let bearer = bearerToken, !bearer.isEmpty {
			request.setValue("Bearer \(bearer)", forHTTPHeaderField: "Authorization")
		}

		// 发送请求
		urlSession.dataTask(with: request) { _, response, error in
			if let error = error {
				os_log("telemetry: send events error: %{public}@",
					   log: .default, type: .error, error.localizedDescription)
				return
			}
			if let httpResponse = response as? HTTPURLResponse, httpResponse.statusCode != 200 {
				os_log("telemetry: send events returned %d",
					   log: .default, type: .error, httpResponse.statusCode)
			}
		}.resume()
	}

	// MARK: - 便捷方法

	/// 上报 chat_request_send 事件
	func reportChatRequest(
		conversationID: String,
		requestID: String,
		modelID: String,
		modelName: String,
		traceID: String,
		inputLength: Int
	) {
		report(eventCode: EventCode.chatRequestSend, model: modelName, properties: [
			"mode": "craft",
			"conversationId": conversationID,
			"requestId": requestID,
			"inputLength": inputLength,
			"inputType": "text",
			"modelId": modelID,
			"modelName": modelName,
			"traceId": traceID,
		])
	}

	/// 上报 chat_message_response 事件
	func reportChatResponse(
		conversationID: String,
		requestID: String,
		modelID: String,
		modelName: String,
		traceID: String,
		inputToken: Int,
		outputToken: Int
	) {
		report(eventCode: EventCode.chatMessageResponse, model: modelName, properties: [
			"conversationId": conversationID,
			"requestId": requestID,
			"inputToken": inputToken,
			"outputToken": outputToken,
			"totalToken": inputToken + outputToken,
			"modelId": modelID,
			"modelName": modelName,
			"traceId": traceID,
		])
	}

	/// 上报 completion_trigger 事件
	func reportCompletionTrigger(
		conversationID: String,
		modelID: String,
		modelName: String,
		traceID: String
	) {
		report(eventCode: EventCode.completionTrigger, model: modelName, properties: [
			"source": "auto",
			"conversationId": conversationID,
			"modelId": modelID,
			"modelName": modelName,
			"traceId": traceID,
		])
	}

	/// 上报 completion_response 事件
	func reportCompletionResponse(
		conversationID: String,
		modelID: String,
		modelName: String,
		traceID: String,
		inputToken: Int,
		outputToken: Int
	) {
		report(eventCode: EventCode.completionResponse, model: modelName, properties: [
			"conversationId": conversationID,
			"inputToken": inputToken,
			"outputToken": outputToken,
			"modelId": modelID,
			"modelName": modelName,
			"traceId": traceID,
			"intent": "inline",
		])
	}

	/// 上报 completion_action(accept) 事件
	func reportCompletionAccept(
		conversationID: String,
		modelID: String,
		modelName: String,
		traceID: String
	) {
		report(eventCode: EventCode.completionAction, model: modelName, properties: [
			"action": CompletionAction.accept,
			"source": "tab",
			"acceptMode": "full",
			"conversationId": conversationID,
			"modelId": modelID,
			"modelName": modelName,
			"traceId": traceID,
		])
	}

	/// 上报 responses_request_send 事件
	func reportResponsesRequest(
		conversationID: String,
		requestID: String,
		modelID: String,
		modelName: String,
		traceID: String,
		inputLength: Int
	) {
		report(eventCode: EventCode.responsesRequestSend, model: modelName, properties: [
			"mode": "craft",
			"conversationId": conversationID,
			"requestId": requestID,
			"inputLength": inputLength,
			"inputType": "text",
			"modelId": modelID,
			"modelName": modelName,
			"traceId": traceID,
		])
	}

	/// 上报 responses_message_response 事件
	func reportResponsesResponse(
		conversationID: String,
		requestID: String,
		modelID: String,
		modelName: String,
		traceID: String,
		inputToken: Int,
		outputToken: Int
	) {
		report(eventCode: EventCode.responsesMessageResponse, model: modelName, properties: [
			"conversationId": conversationID,
			"requestId": requestID,
			"inputToken": inputToken,
			"outputToken": outputToken,
			"totalToken": inputToken + outputToken,
			"modelId": modelID,
			"modelName": modelName,
			"traceId": traceID,
		])
	}

	/// 上报 upstream_retry 事件
	func reportUpstreamRetry(
		model: String,
		statusCode: Int,
		attempt: Int,
		maxRetries: Int,
		delayMs: Int64
	) {
		report(eventCode: EventCode.upstreamRetry, model: model, properties: [
			"model": model,
			"statusCode": statusCode,
			"attempt": attempt,
			"maxRetries": maxRetries,
			"delayMs": delayMs,
		])
	}

	/// 上报 upstream_failure 事件
	func reportUpstreamFailure(
		model: String,
		statusCode: Int,
		attempt: Int,
		maxRetries: Int,
		errMsg: String
	) {
		report(eventCode: EventCode.upstreamFailure, model: model, properties: [
			"model": model,
			"statusCode": statusCode,
			"attempt": attempt,
			"maxRetries": maxRetries,
			"errMsg": errMsg,
		])
	}


	// MARK: - 简化便捷方法（匹配 ProxyController 调用模式）

	/// 上报 chat_request_send 事件（简化版）
	func reportChatRequest(model: String) {
		report(eventCode: EventCode.chatRequestSend, model: model, properties: [
			"mode": "craft",
			"inputType": "text",
		])
	}

	/// 上报 chat_message_response 事件（简化版）
	func reportChatResponse(model: String, latency: TimeInterval, credit: Double = 0) {
		var props: [String: Any] = ["latency": latency]
		if credit > 0 { props["credit"] = credit }
		report(eventCode: EventCode.chatMessageResponse, model: model, properties: props)
	}

	/// 上报 responses_request_send 事件（简化版）
	func reportResponsesRequest(model: String) {
		report(eventCode: EventCode.responsesRequestSend, model: model, properties: [
			"mode": "craft",
			"inputType": "text",
		])
	}

	// MARK: - 生命周期

	/// 停止上报器，发送剩余事件
	func shutdown() {
		objc_sync_enter(self)
		guard !isStopped else {
			objc_sync_exit(self)
			return
		}
		isStopped = true
		objc_sync_exit(self)

		stopFireLoop()
		fireBatch() // 发送剩余事件
	}
}
