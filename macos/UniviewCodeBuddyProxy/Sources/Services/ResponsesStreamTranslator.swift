import Foundation

// ═══════════════════════════════════════════════════════════════
// ResponsesStreamTranslator — OpenAI → Responses API SSE 状态机
// 逐块将上游 OpenAI Chat Completions SSE 转换为 Responses API SSE
// ═══════════════════════════════════════════════════════════════

final class ResponsesStreamTranslator {

    // MARK: - Tool Call State

    private struct ToolCallState {
        var id: String = ""
        var name: String = ""
        var arguments: String = ""
        var outputIndex: Int = 0
        var started: Bool = false
    }

    // MARK: - State

    /// 是否已发送 response.created + response.in_progress
    private var started: Bool = false
    /// 是否已发送 response.completed
    private var finished: Bool = false
    /// 文本输出是否已开始
    private var textStarted: Bool = false
    /// 当前 content index（用于 output_text 事件）
    private var contentIndex: Int = 0
    /// prompt token 计数
    private var promptTokens: Int = 0
    /// completion token 计数
    private var completionTokens: Int = 0
    /// finish reason
    private var finishReason: String? = nil
    /// 累积的完整文本内容
    private var fullContent: String = ""
    /// tool_call 状态跟踪（按 OpenAI tool_call index）
    private var toolCalls: [Int: ToolCallState] = [:]
    /// 维护 tool_call 的出现顺序
    private var toolCallOrder: [Int] = []

    /// 客户端请求的模型名
    private let requestedModel: String
    /// response ID
    private let respID: String
    /// 文本输出项 ID
    private let outputItemID: String

    // MARK: - Init

    init(requestedModel: String) {
        self.requestedModel = requestedModel
        self.respID = "resp_" + Self.randomHex(24)
        self.outputItemID = "msg_" + Self.randomHex(24)
    }

    // MARK: - Process

    /// 处理一行上游 SSE 数据，返回零条或多条 Responses API SSE 事件字符串
    func process(line: String) -> [String] {
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty else { return [] }

        let (dataStr, done, ok) = Self.parseSSELine(trimmed)
        guard ok else { return [] }

        if done {
            if !finished {
                finished = true
                return emitFinish()
            }
            return []
        }

        guard let data = dataStr.data(using: .utf8),
              let chunk = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            return []
        }

        // 提取 usage
        if let usage = chunk["usage"] as? [String: Any] {
            if let pt = usage["prompt_tokens"] as? Int { promptTokens = pt }
            else if let pt = usage["prompt_tokens"] as? Double { promptTokens = Int(pt) }

            if let ct = usage["completion_tokens"] as? Int { completionTokens = ct }
            else if let ct = usage["completion_tokens"] as? Double { completionTokens = Int(ct) }
        }

        var events: [String] = []

        guard let choices = chunk["choices"] as? [[String: Any]] else { return [] }

        for choice in choices {
            guard let delta = choice["delta"] as? [String: Any] else { continue }

            // 首次收到有效内容 → 发送 response.created + response.in_progress
            if !started {
                started = true
                events.append(contentsOf: emitResponseCreated())
            }

            // ── content（text）──

            if let content = delta["content"] as? String, !content.isEmpty {
                events.append(contentsOf: emitTextStartIfNeeded())
                fullContent += content
                events.append(responsesSSE("response.output_text.delta", [
                    "type": "response.output_text.delta",
                    "output_index": 0,
                    "content_index": contentIndex,
                    "delta": content,
                ]))
            }

            // ── tool_calls ──

            if let toolCallDeltas = delta["tool_calls"] as? [[String: Any]] {
                for tcMap in toolCallDeltas {
                    let tcIdx = Self.intFromMap(tcMap, key: "index")

                    // 首次出现此 tool_call 索引
                    if toolCalls[tcIdx] == nil {
                        toolCalls[tcIdx] = ToolCallState()
                        toolCallOrder.append(tcIdx)
                    }

                    var tcState = toolCalls[tcIdx]!

                    // 有 ID → 首次出现此 tool_call
                    if let id = tcMap["id"] as? String, !id.isEmpty {
                        tcState.id = id
                        if !tcState.started {
                            tcState.started = true
                            var fnName = ""
                            if let fn = tcMap["function"] as? [String: Any] {
                                fnName = fn["name"] as? String ?? ""
                                tcState.name = fnName
                            }

                            let outputIdx = computeToolOutputIndex(tcIdx: tcIdx)

                            // function_call 输出项添加
                            events.append(responsesSSE("response.output_item.added", [
                                "type": "response.output_item.added",
                                "output_index": outputIdx,
                                "item": [
                                    "type": "function_call",
                                    "id": id,
                                    "call_id": id,
                                    "name": fnName,
                                    "status": "in_progress",
                                ],
                            ]))
                        }
                    }

                    // function arguments delta
                    if let fn = tcMap["function"] as? [String: Any] {
                        if let args = fn["arguments"] as? String, !args.isEmpty {
                            tcState.arguments += args
                            // 仅在 output_item.added 已发送后才发送 arguments delta
                            if tcState.started {
                                let outputIdx = computeToolOutputIndex(tcIdx: tcIdx)
                                events.append(responsesSSE("response.function_call_arguments.delta", [
                                    "type": "response.function_call_arguments.delta",
                                    "output_index": outputIdx,
                                    "item_id": tcState.id,
                                    "delta": args,
                                ]))
                            }
                        }
                        // 也提取 name（可能在后续 chunk 中出现）
                        if let name = fn["name"] as? String, !name.isEmpty, tcState.name.isEmpty {
                            tcState.name = name
                        }
                    }

                    toolCalls[tcIdx] = tcState
                }
            }

            // ── finish_reason ──

            if let fr = choice["finish_reason"] as? String, !fr.isEmpty, !finished {
                finishReason = fr
            }
        }

        return events
    }

    // MARK: - Close (for stream end / errors)

    /// 关闭流（在流结束或出错时调用），返回剩余事件
    func close() -> [String] {
        if finished { return [] }
        finished = true

        var events: [String] = []

        if started {
            events.append(contentsOf: closeTextBlock())
            events.append(contentsOf: closeToolCallBlocks())
        } else {
            // 未收到任何内容就出错 → 先发送 response.created
            events.append(contentsOf: emitResponseCreated())
        }

        // truncated completed
        events.append(responsesSSE("response.completed", [
            "type": "response.completed",
            "response": buildResponsesSSEObject(status: "incomplete", outputItems: nil),
        ]))

        return events
    }

    // MARK: - Error Output

    /// 生成 Responses API 格式 SSE 错误
    static func sseError(message: String) -> String {
        let msg = message
            .replacingOccurrences(of: "\"", with: "\\\"")
            .replacingOccurrences(of: "\n", with: "\\n")
        return "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"server_error\",\"message\":\"\(msg)\"}}\n\n"
    }

    /// 生成 Responses API 格式 SSE 上下文超限错误
    static func sseContextLimitError(message: String) -> String {
        let msg = ("request too large: " + message)
            .replacingOccurrences(of: "\"", with: "\\\"")
            .replacingOccurrences(of: "\n", with: "\\n")
        return "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"message\":\"\(msg)\"}}\n\n"
    }

    // MARK: - Private Event Emitters

    /// 发送 response.created + response.in_progress
    private func emitResponseCreated() -> [String] {
        let createdResp = buildResponsesSSEObject(status: "in_progress", outputItems: nil)
        return [
            responsesSSE("response.created", [
                "type": "response.created",
                "response": createdResp,
            ]),
            responsesSSE("response.in_progress", [
                "type": "response.in_progress",
                "response": createdResp,
            ]),
        ]
    }

    /// 首次有文本内容时发送文本输出开始事件
    private func emitTextStartIfNeeded() -> [String] {
        guard !textStarted else { return [] }
        textStarted = true

        return [
            // 输出项添加
            responsesSSE("response.output_item.added", [
                "type": "response.output_item.added",
                "output_index": 0,
                "item": [
                    "type": "message",
                    "id": outputItemID,
                    "status": "in_progress",
                    "role": "assistant",
                    "content": [] as [Any],
                ],
            ]),
            // 内容部分添加
            responsesSSE("response.content_part.added", [
                "type": "response.content_part.added",
                "output_index": 0,
                "content_index": contentIndex,
                "part": [
                    "type": "output_text",
                    "text": "",
                ],
            ]),
        ]
    }

    /// 流正常结束：关闭文本块 + 工具块 + 发送 completed
    private func emitFinish() -> [String] {
        var events: [String] = []
        events.append(contentsOf: closeTextBlock())
        events.append(contentsOf: closeToolCallBlocks())
        events.append(contentsOf: emitCompleted())
        return events
    }

    /// 关闭文本输出块
    private func closeTextBlock() -> [String] {
        guard textStarted else { return [] }

        return [
            // output_text.done
            responsesSSE("response.output_text.done", [
                "type": "response.output_text.done",
                "output_index": 0,
                "content_index": contentIndex,
                "text": fullContent,
            ]),
            // content_part.done
            responsesSSE("response.content_part.done", [
                "type": "response.content_part.done",
                "output_index": 0,
                "content_index": contentIndex,
                "part": [
                    "type": "output_text",
                    "text": fullContent,
                    "annotations": [] as [Any],
                ],
            ]),
            // output_item.done
            responsesSSE("response.output_item.done", [
                "type": "response.output_item.done",
                "output_index": 0,
                "item": [
                    "type": "message",
                    "id": outputItemID,
                    "status": "completed",
                    "role": "assistant",
                    "content": [
                        [
                            "type": "output_text",
                            "text": fullContent,
                            "annotations": [] as [Any],
                        ],
                    ],
                ],
            ]),
        ]
    }

    /// 关闭所有 tool_call 输出块
    private func closeToolCallBlocks() -> [String] {
        var events: [String] = []

        for tcIdx in toolCallOrder {
            guard let tc = toolCalls[tcIdx], tc.started else { continue }
            let outputIdx = computeToolOutputIndex(tcIdx: tcIdx)

            // function_call_arguments.done
            events.append(responsesSSE("response.function_call_arguments.done", [
                "type": "response.function_call_arguments.done",
                "output_index": outputIdx,
                "item_id": tc.id,
                "arguments": tc.arguments,
            ]))
            // output_item.done
            events.append(responsesSSE("response.output_item.done", [
                "type": "response.output_item.done",
                "output_index": outputIdx,
                "item": [
                    "type": "function_call",
                    "id": tc.id,
                    "call_id": tc.id,
                    "name": tc.name,
                    "arguments": tc.arguments,
                    "status": "completed",
                ],
            ]))
        }

        return events
    }

    /// 发送 response.completed 事件
    private func emitCompleted() -> [String] {
        let status = (finishReason == "length") ? "incomplete" : "completed"

        // 构建输出项列表
        var outputItems: [[String: Any]] = []

        if textStarted {
            outputItems.append([
                "type": "message",
                "id": outputItemID,
                "status": "completed",
                "role": "assistant",
                "content": [
                    [
                        "type": "output_text",
                        "text": fullContent,
                        "annotations": [] as [Any],
                    ],
                ],
            ])
        }

        for tcIdx in toolCallOrder {
            guard let tc = toolCalls[tcIdx] else { continue }
            outputItems.append([
                "type": "function_call",
                "id": tc.id,
                "call_id": tc.id,
                "name": tc.name,
                "arguments": tc.arguments,
                "status": "completed",
            ])
        }

        let completedResp = buildResponsesSSEObject(status: status, outputItems: outputItems)
        return [responsesSSE("response.completed", [
            "type": "response.completed",
            "response": completedResp,
        ])]
    }

    // MARK: - Private Helpers

    /// 计算 tool_call 的 output_index
    /// text 输出占 index 0，tool_call 从 textStarted 后开始偏移
    private func computeToolOutputIndex(tcIdx: Int) -> Int {
        let base = textStarted ? 1 : 0
        return base + Self.indexOfInt(toolCallOrder, tcIdx)
    }

    /// 构建用于 SSE 事件的 response 对象
    private func buildResponsesSSEObject(status: String, outputItems: [[String: Any]]?) -> [String: Any] {
        return [
            "id": respID,
            "object": "response",
            "created_at": Int(Date().timeIntervalSince1970),
            "model": requestedModel,
            "status": status,
            "output": outputItems ?? [] as [Any],
            "usage": [
                "input_tokens": promptTokens,
                "output_tokens": completionTokens,
                "total_tokens": promptTokens + completionTokens,
            ],
            "metadata": [:] as [String: Any],
        ]
    }

    /// 生成 Responses API SSE 事件字符串
    private func responsesSSE(_ event: String, _ data: [String: Any]) -> String {
        guard let jsonData = try? JSONSerialization.data(withJSONObject: data),
              let jsonStr = String(data: jsonData, encoding: .utf8) else {
            return ""
        }
        return "event: \(event)\ndata: \(jsonStr)\n\n"
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

    /// 返回值在数组中的索引，不存在返回 -1
    private static func indexOfInt(_ slice: [Int], _ val: Int) -> Int {
        for (i, v) in slice.enumerated() {
            if v == val { return i }
        }
        return -1
    }

    /// 生成随机十六进制字符串
    private static func randomHex(_ n: Int) -> String {
        var bytes = [UInt8](repeating: 0, count: n)
        _ = SecRandomCopyBytes(kSecRandomDefault, n, &bytes)
        return bytes.map { String(format: "%02x", $0) }.joined()
    }
}
