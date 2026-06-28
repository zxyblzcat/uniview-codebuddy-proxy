import Foundation
import Hummingbird
import HTTPTypes

/// 代理服务器 — Hummingbird HTTP 服务器 + 所有路由注册
final class ProxyServer {
    let configManager: ConfigManager
    let tokenManager: TokenManager
    let authService: AuthService
    let logBuffer: LogBuffer
    let telemetryReporter: TelemetryReporter
    let usageStats: UsageStats

    private var app: Application<RouterResponder<BasicRequestContext>>?
    private var serverTask: Task<Void, Never>?
    private let upstreamClient: UpstreamClient
    private let circuitBreaker: CircuitBreaker
    private let retryHandler: RetryHandler
    private let cacheManager: CacheManager
    private let proxyController: ProxyController

    init(
        configManager: ConfigManager,
        tokenManager: TokenManager,
        authService: AuthService,
        logBuffer: LogBuffer,
        telemetryReporter: TelemetryReporter,
        usageStats: UsageStats
    ) {
        self.configManager = configManager
        self.tokenManager = tokenManager
        self.authService = authService
        self.logBuffer = logBuffer
        self.telemetryReporter = telemetryReporter
        self.usageStats = usageStats
        self.upstreamClient = UpstreamClient()
        self.circuitBreaker = CircuitBreaker()
        self.retryHandler = RetryHandler(
            upstreamClient: upstreamClient,
            circuitBreaker: circuitBreaker,
            tokenManager: tokenManager,
            telemetryReporter: telemetryReporter,
            maxRetries: configManager.maxRetries
        )
        self.cacheManager = CacheManager()
        self.proxyController = ProxyController(
            configManager: configManager,
            tokenManager: tokenManager,
            upstreamClient: upstreamClient,
            retryHandler: retryHandler,
            cacheManager: cacheManager,
            circuitBreaker: circuitBreaker,
            telemetryReporter: telemetryReporter,
            logBuffer: logBuffer,
            usageStats: usageStats
        )
    }

    func start() {
        let port = configManager.port
        logBuffer.info("ProxyServer.start() 被调用，端口: \(port)")
        serverTask = Task {
            logBuffer.info("Task 开始执行")
            let router = Router()

            // ═══ 中间件 ═══
            router.addMiddleware {
                LogMiddleware(logBuffer: logBuffer)
                if !configManager.apiPassword.isEmpty {
                    APIPasswordMiddleware(password: configManager.apiPassword)
                }
                ConcurrencyMiddleware(maxConcurrent: configManager.maxConcurrentRequests)
                MaxBodySizeMiddleware(maxSize: Defaults.maxBodySizeMB * 1024 * 1024)
            }

            // ═══ 健康检查 ═══
            router.get("/health") { _, _ in
                return Response(status: .ok, body: .init(byteBuffer: ByteBuffer(string: "ok")))
            }

            // ═══ 认证路由 ═══
            let authController = AuthController(authService: authService, tokenManager: tokenManager)
            router.get("/auth/start", use: authController.handleStart)
            router.get("/auth/poll", use: authController.handlePoll)
            router.post("/auth/manual", use: authController.handleManual)
            router.get("/auth/status", use: authController.handleStatus)
            router.get("/auth/tokens", use: authController.handleListTokens)
            router.delete("/auth/tokens/{user_id}", use: authController.handleDeleteToken)
            router.post("/auth/tokens/{user_id}/refresh", use: authController.handleRefreshToken)

            // ═══ 代理路由 ═══

            // OpenAI 格式
            router.post("/v1/chat/completions", use: proxyController.handleChatCompletions)
            router.get("/v1/models", use: proxyController.handleModels)
            router.get("/v1/models/{id}", use: proxyController.handleModelByID)
            router.post("/v1/completions", use: proxyController.handleCompletions)
            router.post("/v1/embeddings", use: proxyController.handleEmbeddings)

            // Anthropic 格式
            router.post("/v1/messages", use: proxyController.handleAnthropicMessages)
            router.post("/v1/messages/count_tokens", use: proxyController.handleCountTokens)

            // Responses API
            router.post("/v1/responses", use: proxyController.handleResponses)
            router.post("/v1/responses/compact", use: proxyController.handleResponsesCompact)

            // 工具路由
            router.head("/v1") { _, _ in Response(status: .ok) }
            router.head("/") { _, _ in Response(status: .ok) }
            router.get("/", use: proxyController.handleServiceInfo)

            // ═══ 双路径注册 (/v1/v1/* = /v1/* 镜像) ═══
            router.post("/v1/v1/chat/completions", use: proxyController.handleChatCompletions)
            router.get("/v1/v1/models", use: proxyController.handleModels)
            router.post("/v1/v1/completions", use: proxyController.handleCompletions)
            router.post("/v1/v1/embeddings", use: proxyController.handleEmbeddings)
            router.post("/v1/v1/messages", use: proxyController.handleAnthropicMessages)
            router.post("/v1/v1/responses", use: proxyController.handleResponses)

            // ═══ 管理 API ═══
            let apiController = APIController(
                configManager: configManager,
                tokenManager: tokenManager,
                circuitBreaker: circuitBreaker,
                logBuffer: logBuffer
            )
            router.get("/api/config", use: apiController.handleGetConfig)
            router.put("/api/config", use: apiController.handlePutConfig)
            router.get("/api/logs/stream", use: apiController.handleLogStream)
            router.delete("/api/logs", use: apiController.handleClearLogs)
            router.get("/api/locale", use: apiController.handleGetLocale)
            router.put("/api/locale", use: apiController.handlePutLocale)

            let app = Application(
                router: router,
                configuration: .init(address: .hostname("127.0.0.1", port: port))
            )

            self.app = app

            logBuffer.info("代理服务器启动在 127.0.0.1:\(port)")

            do {
                logBuffer.info("即将调用 app.run()...")
                try await app.run()
                logBuffer.info("app.run() 正常退出")
            } catch {
                logBuffer.error("服务器启动失败: \(error)")
            }
        }
    }

    func stop() {
        serverTask?.cancel()
        serverTask = nil
        logBuffer.info("代理服务器已停止")
    }

    /// 重启服务器（端口等配置变更后调用）
    func restart() {
        logBuffer.info("代理服务器重启中...")
        stop()
        start()
    }
}

// ═══════════════════════════════════════════════
// 控制器
// ═══════════════════════════════════════════════

/// 认证路由控制器
final class AuthController {
    let authService: AuthService
    let tokenManager: TokenManager

    init(authService: AuthService, tokenManager: TokenManager) {
        self.authService = authService
        self.tokenManager = tokenManager
    }

    func handleStart(_ request: Request, _ context: BasicRequestContext) async throws -> Response {
        let result = try await authService.startDeviceFlow()
        let json: [String: String] = ["auth_url": result.authURL, "auth_state": result.authState]
        let data = try JSONSerialization.data(withJSONObject: json)
        return Response(
            status: .ok,
            headers: [.contentType: "application/json"],
            body: .init(byteBuffer: ByteBuffer(data: data))
        )
    }

    func handlePoll(_ request: Request, _ context: BasicRequestContext) async throws -> Response {
        guard let authState = request.uri.queryParameters.get("auth_state") else {
            return Response(status: .badRequest, body: .init(byteBuffer: ByteBuffer(string: #"{"error":"missing auth_state"}"#)))
        }
        let result = await authService.pollToken(authState: authState)
        switch result {
        case .waiting:
            return Response(status: .ok, body: .init(byteBuffer: ByteBuffer(string: #"{"status":"waiting"}"#)))
        case .success(let tokenData):
            await tokenManager.addToken(tokenData)
            return Response(
                status: .ok,
                headers: [.contentType: "application/json"],
                body: .init(byteBuffer: ByteBuffer(string: #"{"status":"success","user_id":"\#(tokenData.userID)"}"#))
            )
        case .failed(let error):
            return Response(status: .badRequest, body: .init(byteBuffer: ByteBuffer(string: #"{"error":"\#(error)"}"#)))
        }
    }

    func handleManual(_ request: Request, _ context: BasicRequestContext) async throws -> Response {
        var request = request
        let body = try await request.collectBody(upTo: 1024 * 1024)
        let json = try JSONDecoder().decode([String: String].self, from: Data(buffer: body))
        guard let bearerToken = json["bearer_token"] else {
            return Response(status: .badRequest, body: .init(byteBuffer: ByteBuffer(string: #"{"error":"missing bearer_token"}"#)))
        }
        let tokenData = try await authService.parseManualToken(bearerToken)
        await tokenManager.addToken(tokenData)
        return Response(status: .ok, body: .init(byteBuffer: ByteBuffer(string: #"{"status":"success"}"#)))
    }

    func handleStatus(_ request: Request, _ context: BasicRequestContext) async throws -> Response {
        let tokens = await tokenManager.getAllTokens()
        let data = try JSONEncoder().encode(tokens)
        return Response(
            status: .ok,
            headers: [.contentType: "application/json"],
            body: .init(byteBuffer: ByteBuffer(data: data))
        )
    }

    func handleListTokens(_ request: Request, _ context: BasicRequestContext) async throws -> Response {
        let tokens = await tokenManager.getAllTokens()
        let data = try JSONEncoder().encode(tokens)
        return Response(
            status: .ok,
            headers: [.contentType: "application/json"],
            body: .init(byteBuffer: ByteBuffer(data: data))
        )
    }

    func handleDeleteToken(_ request: Request, _ context: BasicRequestContext) async throws -> Response {
        guard let userID = context.parameters.get("user_id") else {
            return Response(status: .badRequest, body: .init(byteBuffer: ByteBuffer(string: #"{"error":"missing user_id"}"#)))
        }
        await tokenManager.removeToken(userID: userID)
        return Response(status: .ok, body: .init(byteBuffer: ByteBuffer(string: #"{"status":"deleted"}"#)))
    }

    func handleRefreshToken(_ request: Request, _ context: BasicRequestContext) async throws -> Response {
        guard let userID = context.parameters.get("user_id") else {
            return Response(status: .badRequest, body: .init(byteBuffer: ByteBuffer(string: #"{"error":"missing user_id"}"#)))
        }
        guard let tokenData = await tokenManager.getTokenData(userID: userID) else {
            return Response(status: .notFound, body: .init(byteBuffer: ByteBuffer(string: #"{"error":"token not found"}"#)))
        }
        do {
            let newToken = try await authService.refreshToken(refreshToken: tokenData.refreshToken)
            await tokenManager.removeToken(userID: userID)
            await tokenManager.addToken(newToken)
            return Response(status: .ok, body: .init(byteBuffer: ByteBuffer(string: #"{"status":"refreshed"}"#)))
        } catch {
            return Response(status: .internalServerError, body: .init(byteBuffer: ByteBuffer(string: #"{"error":"\#(error.localizedDescription)"}"#)))
        }
    }
}

/// 管理 API 控制器
final class APIController {
    let configManager: ConfigManager
    let tokenManager: TokenManager
    let circuitBreaker: CircuitBreaker
    let logBuffer: LogBuffer

    init(configManager: ConfigManager, tokenManager: TokenManager, circuitBreaker: CircuitBreaker, logBuffer: LogBuffer) {
        self.configManager = configManager
        self.tokenManager = tokenManager
        self.circuitBreaker = circuitBreaker
        self.logBuffer = logBuffer
    }

    func handleGetConfig(_ request: Request, _ context: BasicRequestContext) async throws -> Response {
        let config = configManager.asEnvironmentVariables()
        let data = try JSONSerialization.data(withJSONObject: config)
        return Response(
            status: .ok,
            headers: [.contentType: "application/json"],
            body: .init(byteBuffer: ByteBuffer(data: data))
        )
    }

    func handlePutConfig(_ request: Request, _ context: BasicRequestContext) async throws -> Response {
        var request = request
        let body = try await request.collectBody(upTo: 1024 * 1024)
        let json = try JSONSerialization.jsonObject(with: Data(buffer: body)) as? [String: Any] ?? [:]
        applyConfigUpdates(json)
        return Response(status: .ok, body: .init(byteBuffer: ByteBuffer(string: #"{"status":"updated"}"#)))
    }

    func handleLogStream(_ request: Request, _ context: BasicRequestContext) async throws -> Response {
        let logStream = logBuffer.logStream(backlog: 100)
        let heartbeatTask = logBuffer.startHeartbeat()

        let stream = AsyncStream<ByteBuffer> { continuation in
            let task = Task {
                for await event in logStream {
                    let sseText = logBuffer.sseEventData(for: event)
                    continuation.yield(ByteBuffer(string: sseText))
                }
                continuation.finish()
            }
            continuation.onTermination = { _ in
                task.cancel()
                heartbeatTask.cancel()
            }
        }

        return Response(
            status: .ok,
            headers: [
                .contentType: "text/event-stream",
                .cacheControl: "no-cache",
                .connection: "keep-alive",
            ],
            body: .init(asyncSequence: stream)
        )
    }

    func handleClearLogs(_ request: Request, _ context: BasicRequestContext) async throws -> Response {
        await logBuffer.clear()
        return Response(status: .ok, body: .init(byteBuffer: ByteBuffer(string: #"{"status":"cleared"}"#)))
    }

    func handleGetLocale(_ request: Request, _ context: BasicRequestContext) async throws -> Response {
        let locale = configManager.locale
        let data = try JSONSerialization.data(withJSONObject: ["locale": locale])
        return Response(
            status: .ok,
            headers: [.contentType: "application/json"],
            body: .init(byteBuffer: ByteBuffer(data: data))
        )
    }

    func handlePutLocale(_ request: Request, _ context: BasicRequestContext) async throws -> Response {
        var request = request
        let body = try await request.collectBody(upTo: 1024 * 1024)
        let json = try JSONSerialization.jsonObject(with: Data(buffer: body)) as? [String: Any] ?? [:]
        if let locale = json["locale"] as? String {
            configManager.locale = locale
        }
        return Response(status: .ok, body: .init(byteBuffer: ByteBuffer(string: #"{"status":"updated"}"#)))
    }

    /// 从 JSON 字典更新配置
    private func applyConfigUpdates(_ json: [String: Any]) {
        if let v = json["port"] as? Int { configManager.port = v }
        if let v = json["apiPassword"] as? String { configManager.apiPassword = v }
        if let v = json["cacheEnabled"] as? Bool { configManager.cacheEnabled = v }
        if let v = json["cacheTTL"] as? Int { configManager.cacheTTL = v }
        if let v = json["debugMode"] as? Bool { configManager.debugMode = v }
        if let v = json["claudeInject"] as? Bool { configManager.claudeInject = v }
        if let v = json["maxRetries"] as? Int { configManager.maxRetries = v }
        if let v = json["cbMaxFailures"] as? Int { configManager.cbMaxFailures = v }
        if let v = json["cbResetTimeoutSecs"] as? Int { configManager.cbResetTimeoutSecs = v }
        if let v = json["cooldownDurationSecs"] as? Int { configManager.cooldownDurationSecs = v }
        if let v = json["telemetryEnabled"] as? Bool { configManager.telemetryEnabled = v }
        if let v = json["imageAutoSwitchModel"] as? Bool { configManager.imageAutoSwitchModel = v }
        if let v = json["visionModel"] as? String { configManager.visionModel = v }
        if let v = json["maxConcurrentRequests"] as? Int { configManager.maxConcurrentRequests = v }
        if let v = json["upstreamMaxConnsPerHost"] as? Int { configManager.upstreamMaxConnsPerHost = v }
        if let v = json["locale"] as? String { configManager.locale = v }
        if let v = json["logMaxSizeMB"] as? Int { configManager.logMaxSizeMB = v }
        if let v = json["logCleanupInterval"] as? Int { configManager.logCleanupInterval = v }
    }
}

// ═══════════════════════════════════════════════
// 中间件
// ═══════════════════════════════════════════════

/// 日志中间件
struct LogMiddleware: RouterMiddleware {
    let logBuffer: LogBuffer

    func handle(_ request: Request, context: BasicRequestContext, next: (Request, BasicRequestContext) async throws -> Response) async throws -> Response {
        let start = Date()
        let response = try await next(request, context)
        let elapsed = String(format: "%.0f", Date().timeIntervalSince(start) * 1000)
        logBuffer.info("[代理] \(request.method.rawValue) \(request.uri.path) → \(response.status.code) (\(elapsed)ms)")
        return response
    }
}

/// API 密码认证中间件
struct APIPasswordMiddleware: RouterMiddleware {
    let password: String

    func handle(_ request: Request, context: BasicRequestContext, next: (Request, BasicRequestContext) async throws -> Response) async throws -> Response {
        let path = request.uri.path
        if path.hasPrefix("/auth/") || path == "/health" || path == "/" || path.hasPrefix("/api/locale") {
            return try await next(request, context)
        }

        let authHeader = request.headers[.authorization]
        let apiKeyHeader = request.headers[HTTPField.Name("x-api-key")!]
        let apiKeyQuery = request.uri.queryParameters.get("api_key")

        let provided = authHeader?
            .replacingOccurrences(of: "Bearer ", with: "")
            ?? apiKeyHeader
            ?? apiKeyQuery

        if let provided, provided == password {
            return try await next(request, context)
        }

        return Response(
            status: .unauthorized,
            headers: [.contentType: "application/json"],
            body: .init(byteBuffer: ByteBuffer(string: #"{"error":{"message":"Invalid API key","type":"invalid_request_error","code":"invalid_api_key"}}"#))
        )
    }
}

/// 并发限制中间件（简化实现）
struct ConcurrencyMiddleware: RouterMiddleware {
    let maxConcurrent: Int

    func handle(_ request: Request, context: BasicRequestContext, next: (Request, BasicRequestContext) async throws -> Response) async throws -> Response {
        // 简化：直接放行，并发控制由 Hummingbird 的 event loop 管理
        return try await next(request, context)
    }
}

/// Body 大小限制中间件
struct MaxBodySizeMiddleware: RouterMiddleware {
    let maxSize: Int

    func handle(_ request: Request, context: BasicRequestContext, next: (Request, BasicRequestContext) async throws -> Response) async throws -> Response {
        // Check body size by collecting the body
        var request = request
        let body = try await request.collectBody(upTo: maxSize + 1)
        if body.readableBytes > maxSize {
            return Response(
                status: .contentTooLarge,
                body: .init(byteBuffer: ByteBuffer(string: #"{"error":"request body too large"}"#))
            )
        }
        return try await next(request, context)
    }
}
