import Foundation

// ═══════════════════════════════════════════════════════════════
// AnthropicStreamTranslator — OpenAI → Anthropic Messages SSE 状态机
// 逐块将上游 OpenAI Chat Completions SSE 转换为 Anthropic Messages SSE
// ═══════════════════════════════════════════════════════════════

final class AnthropicStreamTranslator {

    // MARK: - State

    /// 下一个 content block 的索引
    /*private*/ var nextBlockIdx: Int = 0
    /// 当前 thinking block 的索引（nil = 未打开）
    private var thinkingBlockIdx: Int? = nil
    /// 当前 text block 的索引（nil = 未打开）
    private var textBlockIdx: Int? = nil
    /// OpenAI tool_call 索引 → Anthropic block 索引的映射
    private var toolBlockIdxMap: [Int: Int] = [:]
    /// OpenAI tool_call 索引 → 是否已发送 content_block_start
    private var toolBlocksStarted: [Int: Bool] = [:]
    /// 输入 token 计数
    /*private*/ var inputTokens: Int = 0
    /// 输出 token 计数
    /*private*/ var outputTokens: Int = 0
    /// 缓存命中 token 计数（prompt_tokens_details.cached_tokens, prompt_cache_hit_tokens = cache hits）
    /*private*/ var cacheReadInputTokens: Int = 0
    /// 缓存创建 token 计数（prompt_cache_miss_tokens, prompt_cache_write_tokens）
    /*private*/ var cacheCreationInputTokens: Int = 0
    /// 请求费用（来自 usage.credit）
    /*private*/ var credit: Double = 0
    /// 是否已发送 message_start
    private var started: Bool = false
    /// 是否已发送 message_delta + message_stop
    private var finished: Bool = false

    /// 客户端请求的模型名
    private let requestedModel: String
    /// 消息 ID
    private let msgID: String

    // MARK: - Init

    init(requestedModel: String) {
        self.requestedModel = requestedModel
        self.msgID = "msg_" + Self.randomHex(24)
    }

    // MARK: - Process

    /// 处理一行上游 SSE 数据，返回零条或多条 Anthropic SSE 事件字符串
    func process(line: String) -> [String] {
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty else { return [] }

        let (dataStr, done, ok) = Self.parseSSELine(trimmed)
        guard ok else { return [] }

        if done {
            if !finished {
                finished = true
                var events = closeOpenBlocks()
                events.append(anthropicSSE("message_delta", [
                    "type": "message_delta",
                    "delta": ["stop_reason": "end_turn", "stop_sequence": NSNull()],
                    "usage": ["input_tokens": inputTokens, "output_tokens": outputTokens],
                ]))
                events.append(anthropicSSE("message_stop", [
                    "type": "message_stop",
                ]))
                return events
            }
            return []
        }

        guard let data = dataStr.data(using: .utf8),
              let chunk = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            return []
        }

        // 提取 usage
        extractUsage(from: chunk)

        var events: [String] = []

        guard let choices = chunk["choices"] as? [[String: Any]] else { return [] }

        for choice in choices {
            guard let delta = choice["delta"] as? [String: Any] else { continue }
            let fr = choice["finish_reason"] as? String ?? ""

            // 首次收到有效内容 → 发送 message_start
            if !started {
                started = true
                events.append(anthropicSSE("message_start", [
                    "type": "message_start",
                    "message": [
                        "id": msgID,
                        "type": "message",
                        "role": "assistant",
                        "content": [] as [Any],
                        "model": requestedModel,
                        "stop_reason": NSNull(),
                        "stop_sequence": NSNull(),
                        "usage": [
                            "input_tokens": inputTokens,
                            "output_tokens": 1,
                            "cache_creation_input_tokens": cacheCreationInputTokens,
                            "cache_read_input_tokens": cacheReadInputTokens,
                        ],
                    ],
                ]))
            }

            // ── reasoning_content（thinking blocks）──

            if let reasoningContent = delta["reasoning_content"] as? String, !reasoningContent.isEmpty {
                // 无 thinking block → 打开新 thinking block
                if thinkingBlockIdx == nil {
                    thinkingBlockIdx = nextBlockIdx
                    nextBlockIdx += 1
                    events.append(anthropicSSE("content_block_start", [
                        "type": "content_block_start",
                        "index": thinkingBlockIdx!,
                        "content_block": [
                            "type": "thinking",
                            "thinking": "",
                            "signature": "",
                        ],
                    ]))
                }
                events.append(anthropicSSE("content_block_delta", [
                    "type": "content_block_delta",
                    "index": thinkingBlockIdx!,
                    "delta": [
                        "type": "thinking_delta",
                        "thinking": reasoningContent,
                    ],
                ]))
            }

            // ── content（text blocks）──

            if let content = delta["content"] as? String, !content.isEmpty {
                // thinking→text 切换：先关闭 thinking block
                if let tIdx = thinkingBlockIdx {
                    events.append(contentsOf: emitThinkingSignatureDelta(blockIdx: tIdx))
                    events.append(anthropicSSE("content_block_stop", [
                        "type": "content_block_stop",
                        "index": tIdx,
                    ]))
                    thinkingBlockIdx = nil
                }
                // 无 text block → 打开新 text block
                if textBlockIdx == nil {
                    textBlockIdx = nextBlockIdx
                    nextBlockIdx += 1
                    events.append(anthropicSSE("content_block_start", [
                        "type": "content_block_start",
                        "index": textBlockIdx!,
                        "content_block": [
                            "type": "text",
                            "text": "",
                        ],
                    ]))
                }
                events.append(anthropicSSE("content_block_delta", [
                    "type": "content_block_delta",
                    "index": textBlockIdx!,
                    "delta": [
                        "type": "text_delta",
                        "text": content,
                    ],
                ]))
            }

            // ── tool_calls ──

            if let toolCallDeltas = delta["tool_calls"] as? [[String: Any]] {
                for tcMap in toolCallDeltas {
                    let tcIdx = Self.intFromMap(tcMap, key: "index")

                    // 首次出现此 tool_call 索引：分配 block index
                    if toolBlockIdxMap[tcIdx] == nil {
                        toolBlockIdxMap[tcIdx] = nextBlockIdx
                        nextBlockIdx += 1
                        toolBlocksStarted[tcIdx] = false
                    }

                    let blockIdx = toolBlockIdxMap[tcIdx]!

                    // 还没发送过 content_block_start
                    if !(toolBlocksStarted[tcIdx] ?? false) {
                        // 先关闭前面的 thinking/text block（必须在 id 检查之前执行）
                        if let tIdx = thinkingBlockIdx {
                            events.append(contentsOf: emitThinkingSignatureDelta(blockIdx: tIdx))
                            events.append(anthropicSSE("content_block_stop", [
                                "type": "content_block_stop",
                                "index": tIdx,
                            ]))
                            thinkingBlockIdx = nil
                        }
                        if let txtIdx = textBlockIdx {
                            events.append(anthropicSSE("content_block_stop", [
                                "type": "content_block_stop",
                                "index": txtIdx,
                            ]))
                            textBlockIdx = nil
                        }

                        // 有 id → 正式开始此 tool block
                        if let id = tcMap["id"] as? String, !id.isEmpty {
                            toolBlocksStarted[tcIdx] = true
                            var fnName = ""
                            if let fn = tcMap["function"] as? [String: Any] {
                                fnName = fn["name"] as? String ?? ""
                            }
                            events.append(anthropicSSE("content_block_start", [
                                "type": "content_block_start",
                                "index": blockIdx,
                                "content_block": [
                                    "type": "tool_use",
                                    "id": id,
                                    "name": fnName,
                                    "input": [:] as [String: Any],
                                ],
                            ]))
                        }
                    }

                    // 已发送 content_block_start → 发送 input_json_delta
                    if toolBlocksStarted[tcIdx] == true {
                        if let fn = tcMap["function"] as? [String: Any],
                           let args = fn["arguments"] as? String, !args.isEmpty {
                            events.append(anthropicSSE("content_block_delta", [
                                "type": "content_block_delta",
                                "index": blockIdx,
                                "delta": [
                                    "type": "input_json_delta",
                                    "partial_json": args,
                                ],
                            ]))
                        }
                    }
                }
            }

            // ── finish_reason ──

            if !fr.isEmpty && !finished {
                finished = true
                events.append(contentsOf: closeOpenBlocks())
                events.append(anthropicSSE("message_delta", [
                    "type": "message_delta",
                    "delta": ["stop_reason": Self.finishReasonToStopReason(fr), "stop_sequence": NSNull()],
                    "usage": ["input_tokens": inputTokens, "output_tokens": outputTokens],
                ]))
                events.append(anthropicSSE("message_stop", [
                    "type": "message_stop",
                ]))
            }
        }

        return events
    }

    // MARK: - Close (for stream end / errors)

    /// 关闭流（在流结束或出错时调用），返回剩余事件
    func close() -> [String] {
        if finished { return [] }
        finished = true

        var events = closeOpenBlocks()
        events.append(anthropicSSE("message_delta", [
            "type": "message_delta",
            "delta": ["stop_reason": "stop_sequence", "stop_sequence": "<stream_error>"],
            "usage": ["input_tokens": inputTokens, "output_tokens": outputTokens],
        ]))
        events.append(anthropicSSE("message_stop", [
            "type": "message_stop",
        ]))
        return events
    }

    // MARK: - Error Output

    /// 生成 Anthropic 格式 SSE 错误（api_error 类型）
    static func sseError(message: String) -> String {
        let msg = message
            .replacingOccurrences(of: "\"", with: "\\\"")
            .replacingOccurrences(of: "\n", with: "\\n")
        return "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_error\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"\",\"stop_reason\":null,\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}}\n\n"
            + "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"api_error\",\"message\":\"\(msg)\"}}\n\n"
            + "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
    }

    /// 生成 Anthropic 格式 SSE 上下文超限错误（invalid_request_error 类型，触发 Claude Code autocompact）
    static func sseContextLimitError(message: String) -> String {
        let msg = ("request too large: " + message)
            .replacingOccurrences(of: "\"", with: "\\\"")
            .replacingOccurrences(of: "\n", with: "\\n")
        return "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_ctxlimit\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"\",\"stop_reason\":null,\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}}\n\n"
            + "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"message\":\"\(msg)\"}}\n\n"
            + "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
    }

    // MARK: - Private Helpers

    /// 关闭所有已开启的 content block
    private func closeOpenBlocks() -> [String] {
        var events: [String] = []

        // 1. 关闭 thinking block（先发 signature_delta）
        if let tIdx = thinkingBlockIdx {
            events.append(contentsOf: emitThinkingSignatureDelta(blockIdx: tIdx))
            events.append(anthropicSSE("content_block_stop", [
                "type": "content_block_stop",
                "index": tIdx,
            ]))
            thinkingBlockIdx = nil
        }

        // 2. 关闭 text block
        if let txtIdx = textBlockIdx {
            events.append(anthropicSSE("content_block_stop", [
                "type": "content_block_stop",
                "index": txtIdx,
            ]))
            textBlockIdx = nil
        }

        // 3. 关闭 tool blocks（按索引排序）
        let sortedTcIndices = toolBlocksStarted.keys.sorted()
        for tcIdx in sortedTcIndices {
            if toolBlocksStarted[tcIdx] == true, let blockIdx = toolBlockIdxMap[tcIdx] {
                events.append(anthropicSSE("content_block_stop", [
                    "type": "content_block_stop",
                    "index": blockIdx,
                ]))
            }
        }

        return events
    }

    /// 发送 thinking block 的 signature_delta 事件
    /// Anthropic SSE 协议要求在 content_block_stop 之前发送 signature_delta
    private func emitThinkingSignatureDelta(blockIdx: Int) -> [String] {
        return [anthropicSSE("content_block_delta", [
            "type": "content_block_delta",
            "index": blockIdx,
            "delta": [
                "type": "signature_delta",
                "signature": "",
            ],
        ])]
    }

    /// 从上游 chunk 提取 usage 数据
    private func extractUsage(from chunk: [String: Any]) {
        guard let usage = chunk["usage"] as? [String: Any] else { return }

        if let pt = usage["prompt_tokens"] as? Int, pt > 0 { inputTokens = pt }
        else if let pt = usage["prompt_tokens"] as? Double, Int(pt) > 0 { inputTokens = Int(pt) }

        if let ct = usage["completion_tokens"] as? Int, ct > 0 { outputTokens = ct }
        else if let ct = usage["completion_tokens"] as? Double, Int(ct) > 0 { outputTokens = Int(ct) }

        if let c = usage["credit"] as? Double, c > 0 { credit = c }
        else if let c = usage["credit"] as? Int, Double(c) > 0 { credit = Double(c) }

        // prompt_tokens_details.cached_tokens → cache read (hits)
        if let details = usage["prompt_tokens_details"] as? [String: Any] {
            if let ct = details["cached_tokens"] as? Int, ct > 0 { cacheReadInputTokens = ct }
            else if let ct = details["cached_tokens"] as? Double, Int(ct) > 0 { cacheReadInputTokens = Int(ct) }
            if let cct = details["cache_creation_tokens"] as? Int, cct > 0 { cacheCreationInputTokens = cct }
            else if let cct = details["cache_creation_tokens"] as? Double, Int(cct) > 0 { cacheCreationInputTokens = Int(cct) }
        }

        // 顶层 prompt_cache_hit_tokens → cache read (hits)
        if let hit = usage["prompt_cache_hit_tokens"] as? Int, hit > 0 { cacheReadInputTokens = hit }
        else if let hit = usage["prompt_cache_hit_tokens"] as? Double, Int(hit) > 0 { cacheReadInputTokens = Int(hit) }

        // 顶层 prompt_cache_miss_tokens / prompt_cache_write_tokens → cache creation (misses)
        if let miss = usage["prompt_cache_miss_tokens"] as? Int, miss > 0 { cacheCreationInputTokens = miss }
        else if let miss = usage["prompt_cache_miss_tokens"] as? Double, Int(miss) > 0 { cacheCreationInputTokens = Int(miss) }

        if let write = usage["prompt_cache_write_tokens"] as? Int, write > 0 { cacheCreationInputTokens = write }
        else if let write = usage["prompt_cache_write_tokens"] as? Double, Int(write) > 0 { cacheCreationInputTokens = Int(write) }
    }

    /// 生成 Anthropic SSE 事件字符串
    private func anthropicSSE(_ eventType: String, _ data: [String: Any]) -> String {
        guard let jsonData = try? JSONSerialization.data(withJSONObject: data),
              let jsonStr = String(data: jsonData, encoding: .utf8) else {
            return ""
        }
        return "event: \(eventType)\ndata: \(jsonStr)\n\n"
    }

    /// finish_reason → Anthropic stop_reason 映射
    private static func finishReasonToStopReason(_ finishReason: String) -> String {
        switch finishReason {
        case "stop":           return "end_turn"
        case "tool_calls":     return "tool_use"
        case "length":         return "max_tokens"
        case "content_filter": return "end_turn"
        default:               return "end_turn"
        }
    }

    /// 解析 SSE 行
    private static func parseSSELine(_ line: String) -> (data: String, done: Bool, ok: Bool) {
        var dataStr = line
        if line.hasPrefix("data: ") {
            dataStr = String(line.dropFirst(6))
        } else if line.hasPrefix("data:") {
            dataStr = String(line.dropFirst(5))
        } else {
            return ("", false, false)
        }

        dataStr = dataStr.trimmingCharacters(in: .whitespaces)

        if dataStr == "[DONE]" {
            return ("", true, true)
        }
        if dataStr.isEmpty {
            return ("", false, false)
        }

        return (dataStr, false, true)
    }

    /// 从字典中提取整数值
    private static func intFromMap(_ map: [String: Any], key: String) -> Int {
        if let v = map[key] as? Int { return v }
        if let v = map[key] as? Double { return Int(v) }
        return 0
    }

    /// 生成随机十六进制字符串
    private static func randomHex(_ n: Int) -> String {
        var bytes = [UInt8](repeating: 0, count: n)
        _ = SecRandomCopyBytes(kSecRandomDefault, n, &bytes)
        return bytes.map { String(format: "%02x", $0) }.joined()
    }
}
