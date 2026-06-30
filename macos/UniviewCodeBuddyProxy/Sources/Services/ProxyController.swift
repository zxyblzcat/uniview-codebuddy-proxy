import Foundation
import os
import Hummingbird
import HTTPTypes

/// 代理路由控制器 — 处理所有 /v1/* 请求的格式转换和转发
final class ProxyController {
    let configManager: ConfigManager
    let tokenManager: TokenManager
    let upstreamClient: UpstreamClient
    let retryHandler: RetryHandler
    let cacheManager: CacheManager
    let circuitBreaker: CircuitBreaker
    let telemetryReporter: TelemetryReporter
    let logBuffer: LogBuffer
    let usageStats: UsageStats

    init(
        configManager: ConfigManager,
        tokenManager: TokenManager,
        upstreamClient: UpstreamClient,
        retryHandler: RetryHandler,
        cacheManager: CacheManager,
        circuitBreaker: CircuitBreaker,
        telemetryReporter: TelemetryReporter,
        logBuffer: LogBuffer,
        usageStats: UsageStats
    ) {
        self.configManager = configManager
        self.tokenManager = tokenManager
        self.upstreamClient = upstreamClient
        self.retryHandler = retryHandler
        self.cacheManager = cacheManager
        self.circuitBreaker = circuitBreaker
        self.telemetryReporter = telemetryReporter
        self.logBuffer = logBuffer
        self.usageStats = usageStats
    }

    // MARK: - 图片检测与模型切换

    /// 检测请求体中是否包含图片块
    /// - OpenAI 格式: content 数组中含 type "image_url"
    /// - Anthropic 格式: content 中含 type "image" 且 source.type 为 "base64"/"url"
    private func detectImages(in body: [String: Any]) -> Bool {
        if let messages = body["messages"] as? [[String: Any]] {
            for msg in messages {
                if hasImageInContent(msg["content"]) { return true }
            }
        }
        if body["system"] != nil, hasImageInContent(body["system"]) { return true }
        return false
    }

    private func hasImageInContent(_ content: Any?) -> Bool {
        guard let content = content else { return false }
        guard let array = content as? [[String: Any]] else { return false }
        for item in array {
            guard let type = item["type"] as? String else { continue }
            if type == "image_url" { return true }
            if type == "image",
               let source = item["source"] as? [String: Any],
               let srcType = source["type"] as? String,
               srcType == "base64" || srcType == "url" { return true }
            if type == "tool_result", hasImageInContent(item["content"]) { return true }
        }
        return false
    }

    /// 检测到图片时自动将 model 切换为视觉模型
    private func autoSwitchToVisionModelIfNeeded(_ payload: inout [String: Any]) {
        if configManager.imageAutoSwitchModel && detectImages(in: payload) {
            let visionModel = configManager.visionModel
            os_log("images: detected images, switching model to %{public}@",
                   log: .default, type: .info, visionModel)
            payload["model"] = visionModel
        }
    }

    // MARK: - Helper: collect request body

    private func collectBody(_ request: Request) async throws -> ByteBuffer {
        var request = request
        return try await request.collectBody(upTo: 50 * 1024 * 1024) // 50MB max
    }

    // MARK: - Chat Completions (OpenAI)

    func handleChatCompletions(_ request: Request, _ context: BasicRequestContext) async throws -> Response {
        let body = try await collectBody(request)

        var payload = try JSONSerialization.jsonObject(with: Data(buffer: body)) as? [String: Any] ?? [:]

        if payload["model"] == nil { payload["model"] = "auto-chat" }

        autoSwitchToVisionModelIfNeeded(&payload)

        let clientRequestsStream = (payload["stream"] as? Bool) ?? true
        payload["stream"] = true
        mergeStreamOptions(&payload)
        ensureMinMessages(&payload)
        sanitizeToolChoice(&payload)

        if isProbe(payload) {
            return probeResponse(model: payload["model"] as? String ?? "probe")
        }

        let model = payload["model"] as? String ?? "auto-chat"

        guard let bearerToken = await tokenManager.getNextToken()?.bearerToken else {
            return errorResponse(status: .serviceUnavailable, message: "no available token")
        }

        let headers = upstreamClient.buildUpstreamHeaders(
            model: model,
            intent: .craft,
            userId: await tokenManager.getCurrentUserID() ?? "",
            machineId: upstreamClient.machineId,
            bearerToken: bearerToken
        )

        telemetryReporter.reportChatRequest(model: model)
        let startTime = Date()

        if clientRequestsStream {
            // 流式响应：逐行翻译 SSE
            let stream = AsyncStream<ByteBuffer> { continuation in
                Task {
                    do {
                        let (bytes, response) = try await upstreamClient.doUpstreamRequest(payload: payload, headers: headers)

                        guard response.statusCode == 200 else {
                            let errorBody = await Self.readErrorBody(from: bytes)
                            continuation.yield(ByteBuffer(string: StreamTranslator.sseError(message: errorBody)))
                            continuation.finish()
                            return
                        }

                        let translator = StreamTranslator(requestedModel: model)
                        for try await line in bytes.lines {
                            let outputLines = translator.process(line: line)
                            for output in outputLines {
                                continuation.yield(ByteBuffer(string: output))
                            }
                            if line.trimmingCharacters(in: .whitespaces) == "data: [DONE]" {
                                break
                            }
                        }
                        let latency = Date().timeIntervalSince(startTime)
                        Task { @MainActor in usageStats.recordRequest(model: model, promptTokens: translator.promptTokens, completionTokens: translator.completionTokens, totalTokens: translator.totalTokens, credit: translator.credit, cacheHitTokens: translator.cacheReadInputTokens, cacheCreationInputTokens: translator.cacheCreationInputTokens, latency: latency, success: true) }
                        continuation.finish()
                    } catch {
                        Task { @MainActor in usageStats.recordRequest(model: model, promptTokens: 0, completionTokens: 0, totalTokens: 0, credit: 0, cacheHitTokens: 0, cacheCreationInputTokens: 0, latency: Date().timeIntervalSince(startTime), success: false) }
                        continuation.yield(ByteBuffer(string: StreamTranslator.sseError(message: error.localizedDescription)))
                        continuation.finish()
                    }
                }
            }

            telemetryReporter.reportChatResponse(model: model, latency: Date().timeIntervalSince(startTime))

            return Response(
                status: .ok,
                headers: [.contentType: "text/event-stream", .cacheControl: "no-cache", .connection: "keep-alive"],
                body: .init(asyncSequence: stream)
            )
        } else {
            // 非流式：收集所有 chunk
            do {
                let result = try await retryHandler.execute(payload: payload, headers: headers) { lines, response in
                    try await StreamTranslator.collectChunks(from: lines, requestedModel: model)
                }

                telemetryReporter.reportChatResponse(model: model, latency: Date().timeIntervalSince(startTime))
                let latency = Date().timeIntervalSince(startTime)
                Task { @MainActor in usageStats.recordRequest(model: model, promptTokens: result.promptTokens, completionTokens: result.completionTokens, totalTokens: result.totalTokens, credit: result.credit, cacheHitTokens: result.cacheReadInputTokens, cacheCreationInputTokens: result.cacheCreationInputTokens, latency: latency, success: true) }

                var responseDict = buildNonStreamingResponse(result)
                let data = try JSONSerialization.data(withJSONObject: responseDict)
                return Response(
                    status: .ok,
                    headers: [.contentType: "application/json"],
                    body: .init(byteBuffer: ByteBuffer(data: data))
                )
            } catch {
                return errorResponse(status: .badGateway, message: error.localizedDescription)
            }
        }
    }

    // MARK: - Anthropic Messages

    func handleAnthropicMessages(_ request: Request, _ context: BasicRequestContext) async throws -> Response {
        let body = try await collectBody(request)

        var payload = try JSONSerialization.jsonObject(with: Data(buffer: body)) as? [String: Any] ?? [:]

        if payload["model"] == nil { payload["model"] = "deepseek-v3" }

        autoSwitchToVisionModelIfNeeded(&payload)

        let openaiPayload = convertAnthropicToOpenAI(payload)

        if isProbe(openaiPayload) {
            return anthropicProbeResponse()
        }

        let model = openaiPayload["model"] as? String ?? "deepseek-v3"
        let isStream = (payload["stream"] as? Bool) ?? false

        guard let bearerToken = await tokenManager.getNextToken()?.bearerToken else {
            return anthropicErrorResponse(type: "authentication_error", message: "no available token")
        }

        let headers = upstreamClient.buildUpstreamHeaders(
            model: model, intent: .craft,
            userId: await tokenManager.getCurrentUserID() ?? "",
            machineId: upstreamClient.machineId,
            bearerToken: bearerToken
        )

        telemetryReporter.reportChatRequest(model: model)
        let startTime = Date()

        if isStream {
            let stream = AsyncStream<ByteBuffer> { continuation in
                Task {
                    do {
                        let (bytes, response) = try await upstreamClient.doUpstreamRequest(payload: openaiPayload, headers: headers)
                        guard response.statusCode == 200 else {
                            let errorBody = await Self.readErrorBody(from: bytes)
                            continuation.yield(ByteBuffer(string: AnthropicStreamTranslator.sseError(message: errorBody)))
                            continuation.finish()
                            return
                        }
                        let translator = AnthropicStreamTranslator(requestedModel: model)
                        for try await line in bytes.lines {
                            let outputLines = translator.process(line: line)
                            for output in outputLines {
                                continuation.yield(ByteBuffer(string: output))
                            }
                            if line.trimmingCharacters(in: .whitespaces) == "data: [DONE]" { break }
                        }
                        let finalEvents = translator.close()
                        for output in finalEvents {
                            continuation.yield(ByteBuffer(string: output))
                        }
                        Task { @MainActor in usageStats.recordRequest(model: model, promptTokens: translator.inputTokens, completionTokens: translator.outputTokens, totalTokens: translator.inputTokens + translator.outputTokens, credit: translator.credit, cacheHitTokens: translator.cacheReadInputTokens, cacheCreationInputTokens: translator.cacheCreationInputTokens, latency: Date().timeIntervalSince(startTime), success: true) }
                        continuation.finish()
                    } catch {
                        Task { @MainActor in usageStats.recordRequest(model: model, promptTokens: 0, completionTokens: 0, totalTokens: 0, credit: 0, cacheHitTokens: 0, cacheCreationInputTokens: 0, latency: Date().timeIntervalSince(startTime), success: false) }
                        continuation.yield(ByteBuffer(string: AnthropicStreamTranslator.sseError(message: error.localizedDescription)))
                        continuation.finish()
                    }
                }
            }

            telemetryReporter.reportChatResponse(model: model, latency: Date().timeIntervalSince(startTime))

            return Response(
                status: .ok,
                headers: [.contentType: "text/event-stream", .cacheControl: "no-cache", .connection: "keep-alive"],
                body: .init(asyncSequence: stream)
            )
        } else {
            do {
                let result = try await retryHandler.execute(payload: openaiPayload, headers: headers) { lines, response in
                    try await StreamTranslator.collectChunks(from: lines, requestedModel: model)
                }

                telemetryReporter.reportChatResponse(model: model, latency: Date().timeIntervalSince(startTime))
                Task { @MainActor in usageStats.recordRequest(model: model, promptTokens: result.promptTokens, completionTokens: result.completionTokens, totalTokens: result.totalTokens, credit: result.credit, cacheHitTokens: result.cacheReadInputTokens, cacheCreationInputTokens: result.cacheCreationInputTokens, latency: Date().timeIntervalSince(startTime), success: true) }

                let anthropicResponse = convertOpenAIToAnthropic(result, originalPayload: payload)
                let data = try JSONSerialization.data(withJSONObject: anthropicResponse)
                return Response(
                    status: .ok,
                    headers: [.contentType: "application/json"],
                    body: .init(byteBuffer: ByteBuffer(data: data))
                )
            } catch {
                return anthropicErrorResponse(type: "api_error", message: error.localizedDescription)
            }
        }
    }

    // MARK: - Responses API

    func handleResponses(_ request: Request, _ context: BasicRequestContext) async throws -> Response {
        let body = try await collectBody(request)
        var payload = try JSONSerialization.jsonObject(with: Data(buffer: body)) as? [String: Any] ?? [:]
        autoSwitchToVisionModelIfNeeded(&payload)

        let openaiPayload = convertResponsesToOpenAI(payload)
        let model = openaiPayload["model"] as? String ?? "auto-chat"
        let isStream = (payload["stream"] as? Bool) ?? false

        guard let bearerToken = await tokenManager.getNextToken()?.bearerToken else {
            return errorResponse(status: .serviceUnavailable, message: "no available token")
        }
        let headers = upstreamClient.buildUpstreamHeaders(
            model: model, intent: .craft,
            userId: await tokenManager.getCurrentUserID() ?? "",
            machineId: upstreamClient.machineId,
            bearerToken: bearerToken
        )

        telemetryReporter.reportResponsesRequest(model: model)

        if isStream {
            let stream = AsyncStream<ByteBuffer> { continuation in
                Task {
                    do {
                        let (bytes, response) = try await upstreamClient.doUpstreamRequest(payload: openaiPayload, headers: headers)
                        guard response.statusCode == 200 else {
                            continuation.yield(ByteBuffer(string: #"event: error\ndata: {"type":"error","error":{"type":"server_error","message":"upstream error"}}\n\n"#))
                            continuation.finish()
                            return
                        }
                        let translator = ResponsesStreamTranslator(requestedModel: model)
                        for try await line in bytes.lines {
                            let outputLines = translator.process(line: line)
                            for output in outputLines {
                                continuation.yield(ByteBuffer(string: output))
                            }
                            if line.trimmingCharacters(in: .whitespaces) == "data: [DONE]" { break }
                        }
                        let finalEvents = translator.close()
                        for output in finalEvents {
                            continuation.yield(ByteBuffer(string: output))
                        }
                        continuation.finish()
                    } catch {
                        continuation.finish()
                    }
                }
            }
            return Response(
                status: .ok,
                headers: [.contentType: "text/event-stream", .cacheControl: "no-cache"],
                body: .init(asyncSequence: stream)
            )
        } else {
            do {
                let result = try await retryHandler.execute(payload: openaiPayload, headers: headers) { lines, response in
                    try await StreamTranslator.collectChunks(from: lines, requestedModel: model)
                }
                let responsesResult = convertOpenAIToResponses(result, originalPayload: payload)
                let data = try JSONSerialization.data(withJSONObject: responsesResult)
                return Response(status: .ok, headers: [.contentType: "application/json"], body: .init(byteBuffer: ByteBuffer(data: data)))
            } catch {
                return errorResponse(status: .badGateway, message: error.localizedDescription)
            }
        }
    }

    func handleResponsesCompact(_ request: Request, _ context: BasicRequestContext) async throws -> Response {
        let body = try await collectBody(request)
        var payload = try JSONSerialization.jsonObject(with: Data(buffer: body)) as? [String: Any] ?? [:]
        var openaiPayload = convertResponsesToOpenAI(payload)

        let compactInstruction = "You are a context compaction assistant. Summarize the conversation so far, preserving all important context, decisions, and code references. Keep the summary concise but complete."
        if var messages = openaiPayload["messages"] as? [[String: Any]] {
            messages.insert(["role": "system", "content": compactInstruction], at: 0)
            openaiPayload["messages"] = messages
        }
        openaiPayload["max_tokens"] = 4096

        let model = openaiPayload["model"] as? String ?? "auto-chat"
        guard let bearerToken = await tokenManager.getNextToken()?.bearerToken else {
            return errorResponse(status: .serviceUnavailable, message: "no available token")
        }
        let headers = upstreamClient.buildUpstreamHeaders(
            model: model, intent: .craft,
            userId: await tokenManager.getCurrentUserID() ?? "",
            machineId: upstreamClient.machineId,
            bearerToken: bearerToken
        )
        do {
            let result = try await retryHandler.execute(payload: openaiPayload, headers: headers) { lines, response in
                try await StreamTranslator.collectChunks(from: lines, requestedModel: model)
            }
            let responsesResult = convertOpenAIToResponses(result, originalPayload: payload)
            let data = try JSONSerialization.data(withJSONObject: responsesResult)
            return Response(status: .ok, headers: [.contentType: "application/json"], body: .init(byteBuffer: ByteBuffer(data: data)))
        } catch {
            return errorResponse(status: .badGateway, message: error.localizedDescription)
        }
    }

    // MARK: - Models

    func handleModels(_ request: Request, _ context: BasicRequestContext) async throws -> Response {
        let models = extraModels.map { ["id": $0.name, "object": "model", "owned_by": $0.ownedBy] }
        let data = try JSONSerialization.data(withJSONObject: ["object": "list", "data": models])
        return Response(status: .ok, headers: [.contentType: "application/json"], body: .init(byteBuffer: ByteBuffer(data: data)))
    }

    func handleModelByID(_ request: Request, _ context: BasicRequestContext) async throws -> Response {
        guard let id = context.parameters.get("id") else {
            return errorResponse(status: .badRequest, message: "missing model id")
        }
        let contextWindow = getModelContextWindow(model: id)
        let maxOutput = max(min(contextWindow / 4, contextWindow - 1), 8192)
        let response: [String: Any] = [
            "id": id,
            "object": "model",
            "owned_by": inferOwnedBy(model: id),
            "max_input_tokens": contextWindow,
            "max_output_tokens": maxOutput,
            "capabilities": [
                "context_management": ["supports_compact": true],
                "effort": ["supported_values": ["high", "low", "max", "medium"]],
                "thinking": ["is_supported": false],
                "image_input": ["is_supported": false],
                "pdf_input": ["is_supported": false],
                "structured_outputs": ["is_supported": false],
            ],
        ]
        let data = try JSONSerialization.data(withJSONObject: response)
        return Response(status: .ok, headers: [.contentType: "application/json"], body: .init(byteBuffer: ByteBuffer(data: data)))
    }

    // MARK: - Completions & Embeddings

    func handleCompletions(_ request: Request, _ context: BasicRequestContext) async throws -> Response {
        let body = try await collectBody(request)
        var payload = try JSONSerialization.jsonObject(with: Data(buffer: body)) as? [String: Any] ?? [:]
        payload["stream"] = true
        mergeStreamOptions(&payload)
        ensureMinMessages(&payload)

        guard let bearerToken = await tokenManager.getNextToken()?.bearerToken else {
            return errorResponse(status: .serviceUnavailable, message: "no available token")
        }
        let model = payload["model"] as? String ?? "auto-chat"
        let headers = upstreamClient.buildUpstreamHeaders(
            model: model, intent: .codeCompletion,
            userId: await tokenManager.getCurrentUserID() ?? "",
            machineId: upstreamClient.machineId,
            bearerToken: bearerToken
        )

        do {
            let result = try await retryHandler.execute(payload: payload, headers: headers) { bytes, response in
                var text = ""
                var model = ""
                var finishReason = ""
                for try await line in bytes.lines {
                    let trimmed = line.trimmingCharacters(in: .whitespaces)
                    guard trimmed.hasPrefix("data: "), trimmed != "data: [DONE]" else { continue }
                    let jsonString = String(trimmed.dropFirst(6))
                    guard let data = jsonString.data(using: .utf8),
                          let chunk = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                          let choices = chunk["choices"] as? [[String: Any]],
                          let delta = choices.first?["text"] as? String else { continue }
                    text += delta
                    model = chunk["model"] as? String ?? model
                    finishReason = choices.first?["finish_reason"] as? String ?? finishReason
                }
                return (text, model, finishReason)
            }
            let response: [String: Any] = [
                "id": "cmpl-\(UUID().uuidString.prefix(8))",
                "object": "text_completion",
                "created": Int(Date().timeIntervalSince1970),
                "model": result.1,
                "choices": [["text": result.0, "index": 0, "finish_reason": result.2]],
            ]
            let data = try JSONSerialization.data(withJSONObject: response)
            return Response(status: .ok, headers: [.contentType: "application/json"], body: .init(byteBuffer: ByteBuffer(data: data)))
        } catch {
            return errorResponse(status: .badGateway, message: error.localizedDescription)
        }
    }

    func handleEmbeddings(_ request: Request, _ context: BasicRequestContext) async throws -> Response {
        let body = try await collectBody(request)
        let payload = try JSONSerialization.jsonObject(with: Data(buffer: body)) as? [String: Any] ?? [:]
        guard let bearerToken = await tokenManager.getNextToken()?.bearerToken else {
            return errorResponse(status: .serviceUnavailable, message: "no available token")
        }
        let model = payload["model"] as? String ?? "codebuddy-embed"
        let headers = upstreamClient.buildUpstreamHeaders(
            model: model, intent: .embedding,
            userId: await tokenManager.getCurrentUserID() ?? "",
            machineId: upstreamClient.machineId,
            bearerToken: bearerToken
        )
        do {
            let (bytes, response) = try await upstreamClient.doUpstreamRequest(payload: payload, headers: headers)
            var responseBody = Data()
            for try await line in bytes.lines {
                if let data = line.data(using: .utf8) { responseBody.append(data) }
            }
            return Response(status: .ok, headers: [.contentType: "application/json"], body: .init(byteBuffer: ByteBuffer(data: responseBody)))
        } catch {
            return errorResponse(status: .badGateway, message: error.localizedDescription)
        }
    }

    func handleCountTokens(_ request: Request, _ context: BasicRequestContext) async throws -> Response {
        let body = try await collectBody(request)
        let payload = try JSONSerialization.jsonObject(with: Data(buffer: body)) as? [String: Any] ?? [:]
        let messages = payload["messages"] as? [[String: Any]] ?? []
        let totalChars = messages.compactMap { $0["content"] as? String }.reduce(0) { $0 + $1.count }
        let estimatedTokens = totalChars / 4
        let response: [String: Any] = ["input_tokens": estimatedTokens]
        let data = try JSONSerialization.data(withJSONObject: response)
        return Response(status: .ok, headers: [.contentType: "application/json"], body: .init(byteBuffer: ByteBuffer(data: data)))
    }

    func handleServiceInfo(_ request: Request, _ context: BasicRequestContext) async throws -> Response {
        let info: [String: Any] = [
            "name": "Uniview CodeBuddy Proxy",
            "version": Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "0.1.0",
            "port": configManager.port,
        ]
        let data = try JSONSerialization.data(withJSONObject: info)
        return Response(status: .ok, headers: [.contentType: "application/json"], body: .init(byteBuffer: ByteBuffer(data: data)))
    }

    // MARK: - 辅助方法

    private func mergeStreamOptions(_ payload: inout [String: Any]) {
        if var so = payload["stream_options"] as? [String: Any] {
            so["include_usage"] = true
            payload["stream_options"] = so
        } else {
            payload["stream_options"] = ["include_usage": true]
        }
    }

    private func ensureMinMessages(_ payload: inout [String: Any]) {
        guard var messages = payload["messages"] as? [[String: Any]] else { return }
        if messages.count < 2 {
            messages.insert(["role": "system", "content": "You are a helpful assistant."], at: 0)
            payload["messages"] = messages
        }
    }

    private func sanitizeToolChoice(_ payload: inout [String: Any]) {
        guard let tc = payload["tool_choice"] else { return }
        if tc is String { return }
        if let dict = tc as? [String: Any] {
            if let type = dict["type"] as? String, ["function", "tool"].contains(type) {
                payload["tool_choice"] = "required"
            }
        }
    }

    private func isProbe(_ payload: [String: Any]) -> Bool {
        guard let maxTokens = payload["max_tokens"] as? Int, maxTokens == 1 else { return false }
        guard let stream = payload["stream"] as? Bool, stream == true else { return false }
        return true
    }

    private func probeResponse(model: String) -> Response {
        let response: [String: Any] = [
            "id": "chatcmpl-probe",
            "object": "chat.completion",
            "created": 0,
            "model": model,
            "choices": [["index": 0, "message": ["role": "assistant", "content": "ok"], "finish_reason": "stop"]],
            "usage": ["prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0],
        ]
        let data = try! JSONSerialization.data(withJSONObject: response)
        return Response(status: .ok, headers: [.contentType: "application/json"], body: .init(byteBuffer: ByteBuffer(data: data)))
    }

    private func anthropicProbeResponse() -> Response {
        let events = [
            "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_probe\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"probe\",\"stop_reason\":null,\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}}\n\n",
            "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
            "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n",
            "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
            "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n",
            "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
        ]
        let body = events.joined()
        return Response(
            status: .ok,
            headers: [.contentType: "text/event-stream", .cacheControl: "no-cache"],
            body: .init(byteBuffer: ByteBuffer(string: body))
        )
    }

    /// 构建非流式 OpenAI 响应体
    private func buildNonStreamingResponse(_ result: CollectedResult) -> [String: Any] {
        var responseDict: [String: Any] = [
            "id": result.id,
            "object": "chat.completion",
            "created": Int(Date().timeIntervalSince1970),
            "model": result.model,
            "choices": [[
                "index": 0,
                "message": [
                    "role": "assistant",
                    "content": result.content,
                ],
                "finish_reason": result.finishReason,
            ]],
            "usage": [
                "prompt_tokens": result.promptTokens,
                "completion_tokens": result.completionTokens,
                "total_tokens": result.totalTokens > 0 ? result.totalTokens : result.promptTokens + result.completionTokens,
            ],
        ]
        if result.reasoningTokens > 0 {
            responseDict["completion_tokens_details"] = ["reasoning_tokens": result.reasoningTokens]
        }
        if result.cacheReadInputTokens > 0 {
            if var usage = responseDict["usage"] as? [String: Any] {
                usage["prompt_tokens_details"] = ["cached_tokens": result.cacheReadInputTokens]
                responseDict["usage"] = usage
            }
        }
        if !result.toolCalls.isEmpty {
            var choices = responseDict["choices"] as? [[String: Any]] ?? []
            if var choice = choices.first,
               var message = choice["message"] as? [String: Any] {
                message["tool_calls"] = result.toolCalls
                choice["message"] = message
                choices[0] = choice
                responseDict["choices"] = choices
            }
        }
        return responseDict
    }

    private func errorResponse(status: HTTPResponse.Status, message: String) -> Response {
        let response: [String: Any] = [
            "error": [
                "message": message,
                "type": "invalid_request_error",
                "code": "proxy_error",
            ],
        ]
        let data = (try? JSONSerialization.data(withJSONObject: response)) ?? Data()
        return Response(status: status, headers: [.contentType: "application/json"], body: .init(byteBuffer: ByteBuffer(data: data)))
    }

    private func anthropicErrorResponse(type: String, message: String) -> Response {
        let response: [String: Any] = [
            "type": "error",
            "error": ["type": type, "message": message],
        ]
        let data = (try? JSONSerialization.data(withJSONObject: response)) ?? Data()
        return Response(status: .badRequest, headers: [.contentType: "application/json"], body: .init(byteBuffer: ByteBuffer(data: data)))
    }

    // MARK: - 读取错误体

    private static func readErrorBody(from bytes: URLSession.AsyncBytes) async -> String {
        var body = ""
        do {
            for try await line in bytes.lines {
                body += line
                if body.count > 500 { break }
            }
        } catch {
            // Ignore read errors
        }
        return body.replacingOccurrences(of: "<[^>]+>", with: "", options: .regularExpression)
    }

    // MARK: - 格式转换

    private func convertAnthropicToOpenAI(_ payload: [String: Any]) -> [String: Any] {
        var result: [String: Any] = [:]
        result["model"] = payload["model"] ?? "deepseek-v3"
        result["stream"] = true
        result["stream_options"] = ["include_usage": true]

        if let maxTokens = payload["max_tokens"] as? Int {
            result["max_tokens"] = maxTokens
        } else {
            result["max_tokens"] = 4096
        }

        if let temp = payload["temperature"] as? Double {
            result["temperature"] = temp
        }

        if let messages = payload["messages"] as? [[String: Any]] {
            result["messages"] = convertAnthropicMessagesToOpenAI(messages)
        }

        if let tools = payload["tools"] as? [[String: Any]] {
            result["tools"] = convertAnthropicToolsToOpenAI(tools)
        }

        if let tc = payload["tool_choice"] {
            result["tool_choice"] = convertAnthropicToolChoiceToOpenAI(tc)
        }

        if let stop = payload["stop_sequences"] as? [String] {
            result["stop"] = stop
        }

        ensureMinMessages(&result)
        return result
    }

    private func convertAnthropicMessagesToOpenAI(_ messages: [[String: Any]]) -> [[String: Any]] {
        var result: [[String: Any]] = []

        for msg in messages {
            let role = msg["role"] as? String ?? "user"
            let content = msg["content"]

            if let contentStr = content as? String {
                result.append(["role": role, "content": contentStr])
            } else if let contentArray = content as? [[String: Any]] {
                var parts: [[String: Any]] = []
                var textContent = ""

                for block in contentArray {
                    let type = block["type"] as? String ?? ""
                    switch type {
                    case "text":
                        if let text = block["text"] as? String {
                            parts.append(["type": "text", "text": text])
                            textContent += text
                        }
                    case "image":
                        if let source = block["source"] as? [String: Any],
                           let sourceType = source["type"] as? String {
                            if sourceType == "base64", let data = source["data"] as? String,
                               let mediaType = source["media_type"] as? String {
                                let url = "data:\(mediaType);base64,\(data)"
                                parts.append(["type": "image_url", "image_url": ["url": url]])
                            } else if sourceType == "url", let url = source["url"] as? String {
                                parts.append(["type": "image_url", "image_url": ["url": url]])
                            }
                        }
                    case "tool_use":
                        if let id = block["id"] as? String, let name = block["name"] as? String,
                           let input = block["input"] {
                            result.append([
                                "role": "assistant",
                                "tool_calls": [[
                                    "id": id,
                                    "type": "function",
                                    "function": ["name": name, "arguments": jsonString(from: input)],
                                ]],
                            ])
                        }
                    case "tool_result":
                        if let toolUseId = block["tool_use_id"] as? String {
                            var toolContent: Any = ""
                            if let resultContent = block["content"] as? String {
                                toolContent = resultContent
                            } else if let resultArray = block["content"] as? [[String: Any]] {
                                toolContent = resultArray.compactMap { $0["text"] as? String }.joined()
                            }
                            result.append([
                                "role": "tool",
                                "tool_call_id": toolUseId,
                                "content": toolContent,
                            ])
                        }
                    default:
                        break
                    }
                }

                if !parts.isEmpty {
                    if parts.count == 1, let first = parts.first, first["type"] as? String == "text" {
                        result.append(["role": role, "content": textContent])
                    } else {
                        result.append(["role": role, "content": parts])
                    }
                }
            }
        }

        return result
    }

    private func convertAnthropicToolsToOpenAI(_ tools: [[String: Any]]) -> [[String: Any]] {
        return tools.compactMap { tool -> [String: Any]? in
            guard let name = tool["name"] as? String else { return nil }
            return [
                "type": "function",
                "function": [
                    "name": name,
                    "description": tool["description"] as? String ?? "",
                    "parameters": tool["input_schema"] ?? [:],
                ],
            ]
        }
    }

    private func convertAnthropicToolChoiceToOpenAI(_ tc: Any) -> Any {
        if let str = tc as? String {
            switch str {
            case "auto": return "auto"
            case "any": return "required"
            case "none": return "none"
            default: return "auto"
            }
        }
        if let dict = tc as? [String: Any], dict["type"] as? String == "tool" {
            return "required"
        }
        return "auto"
    }

    private func convertOpenAIToAnthropic(_ result: CollectedResult, originalPayload: [String: Any]) -> [String: Any] {
        var content: [[String: Any]] = []

        if !result.content.isEmpty {
            content.append(["type": "text", "text": result.content])
        }

        for toolCall in result.toolCalls {
            if let tc = toolCall as? [String: Any],
               let id = tc["id"] as? String,
               let function = tc["function"] as? [String: Any],
               let name = function["name"] as? String {
                content.append([
                    "type": "tool_use",
                    "id": id,
                    "name": name,
                    "input": function["arguments"] as? String ?? "{}",
                ])
            }
        }

        let stopReason: String
        switch result.finishReason {
        case "stop": stopReason = "end_turn"
        case "tool_calls": stopReason = "tool_use"
        case "length": stopReason = "max_tokens"
        default: stopReason = "end_turn"
        }

        return [
            "id": "msg_\(UUID().uuidString.prefix(24))",
            "type": "message",
            "role": "assistant",
            "content": content,
            "model": result.model,
            "stop_reason": stopReason,
            "stop_sequence": NSNull(),
            "usage": [
                "input_tokens": result.promptTokens,
                "output_tokens": result.completionTokens,
                "cache_creation_input_tokens": result.cacheCreationInputTokens,
                "cache_read_input_tokens": result.cacheReadInputTokens,
            ],
        ]
    }

    private func convertResponsesToOpenAI(_ payload: [String: Any]) -> [String: Any] {
        var result: [String: Any] = [:]
        result["model"] = payload["model"] ?? "auto-chat"
        result["stream"] = true
        result["stream_options"] = ["include_usage": true]

        if let maxTokens = payload["max_output_tokens"] as? Int {
            result["max_tokens"] = maxTokens
        } else {
            result["max_tokens"] = 4096
        }

        if let temp = payload["temperature"] as? Double {
            result["temperature"] = temp
        }

        if let input = payload["input"] {
            result["messages"] = convertResponsesInputToMessages(input)
        }

        if let instructions = payload["instructions"] as? String, !instructions.isEmpty {
            if var messages = result["messages"] as? [[String: Any]] {
                messages.insert(["role": "system", "content": instructions], at: 0)
                result["messages"] = messages
            }
        }

        if let tools = payload["tools"] as? [[String: Any]] {
            result["tools"] = convertResponsesToolsToOpenAI(tools)
        }

        if let reasoning = payload["reasoning"] as? [String: Any],
           let effort = reasoning["effort"] as? String {
            switch effort {
            case "high": result["temperature"] = 0.7
            case "low": result["temperature"] = 0.3
            case "medium": result["temperature"] = 0.5
            default: break
            }
        }

        ensureMinMessages(&result)
        return result
    }

    private func convertResponsesInputToMessages(_ input: Any) -> [[String: Any]] {
        var messages: [[String: Any]] = []

        if let inputStr = input as? String {
            messages.append(["role": "user", "content": inputStr])
        } else if let inputArray = input as? [Any] {
            for item in inputArray {
                if let str = item as? String {
                    messages.append(["role": "user", "content": str])
                } else if let dict = item as? [String: Any] {
                    let role = dict["role"] as? String ?? "user"
                    if let content = dict["content"] {
                        messages.append(["role": role, "content": content])
                    }
                }
            }
        }

        return messages
    }

    private func convertResponsesToolsToOpenAI(_ tools: [[String: Any]]) -> [[String: Any]] {
        return tools.compactMap { tool -> [String: Any]? in
            let type = tool["type"] as? String ?? ""
            switch type {
            case "function", "function_call":
                guard let name = tool["name"] as? String else { return nil }
                return [
                    "type": "function",
                    "function": [
                        "name": name,
                        "description": tool["description"] as? String ?? "",
                        "parameters": tool["parameters"] ?? tool["input_schema"] ?? [:],
                    ],
                ]
            default:
                return nil
            }
        }
    }

    private func convertOpenAIToResponses(_ result: CollectedResult, originalPayload: [String: Any]) -> [String: Any] {
        var output: [[String: Any]] = []

        if !result.content.isEmpty {
            output.append([
                "type": "message",
                "id": "msg_\(UUID().uuidString.prefix(24))",
                "role": "assistant",
                "content": [["type": "output_text", "text": result.content]],
            ])
        }

        for toolCall in result.toolCalls {
            if let tc = toolCall as? [String: Any],
               let id = tc["id"] as? String,
               let function = tc["function"] as? [String: Any],
               let name = function["name"] as? String {
                output.append([
                    "type": "function_call",
                    "id": id,
                    "call_id": id,
                    "name": name,
                    "arguments": function["arguments"] as? String ?? "{}",
                ])
            }
        }

        let stopReason: String
        switch result.finishReason {
        case "stop": stopReason = "completed"
        case "tool_calls": stopReason = "completed"
        case "length": stopReason = "incomplete"
        default: stopReason = "completed"
        }

        return [
            "id": "resp_\(UUID().uuidString.prefix(24))",
            "object": "response",
            "created_at": Int(Date().timeIntervalSince1970),
            "status": "completed",
            "model": result.model,
            "output": output,
            "usage": [
                "input_tokens": result.promptTokens,
                "output_tokens": result.completionTokens,
                "total_tokens": result.totalTokens > 0 ? result.totalTokens : result.promptTokens + result.completionTokens,
            ],
        ]
    }

    private func jsonString(from object: Any) -> String {
        guard let data = try? JSONSerialization.data(withJSONObject: object),
              let string = String(data: data, encoding: .utf8) else {
            return "{}"
        }
        return string
    }

    private func getModelContextWindow(model: String) -> Int {
        let contextWindows: [String: Int] = [
            "glm-5.1": 128000, "glm-5.0": 128000, "glm-4.7": 128000, "glm-4.6": 128000,
            "deepseek-r1": 65536, "deepseek-v3": 65536, "deepseek-v3-1-lkeap": 65536,
            "minimax-m2.7": 256000, "minimax-m2.5": 256000,
            "kimi-k2.5": 128000,
            "hunyuan-2.0-instruct": 32768,
        ]
        if let exact = contextWindows[model] { return exact }
        for (prefix, window) in contextWindows where model.hasPrefix(prefix) {
            return window
        }
        return 200000
    }
}

/// 收集结果结构体
struct CollectedResult {
    var id: String = ""
    var model: String = ""
    var content: String = ""
    var reasoningContent: String = ""
    var toolCalls: [Any] = []
    var finishReason: String = ""
    var promptTokens: Int = 0
    var completionTokens: Int = 0
    var reasoningTokens: Int = 0
    var cacheReadInputTokens: Int = 0
    var cacheCreationInputTokens: Int = 0
    var thinkingTokens: Int = 0
    var totalTokens: Int = 0
    var credit: Double = 0
}

/// 上游请求意图
enum UpstreamIntent: String {
    case craft = "craft"
    case codeCompletion = "CodeCompletion"
    case embedding = "embedding"
}
