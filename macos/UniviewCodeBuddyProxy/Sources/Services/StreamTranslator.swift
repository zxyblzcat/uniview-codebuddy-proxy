import Foundation
import Hummingbird

// ═══════════════════════════════════════════════════════════════
// StreamTranslator — OpenAI → OpenAI SSE 流透传翻译器
// 解析上游 SSE chunk，替换 model/id，清理 choices，输出标准 OpenAI 格式
// ═══════════════════════════════════════════════════════════════

final class StreamTranslator {

    // MARK: - State

    /// 首个有效 token 时间
    private var tfft: Date?
    /// prompt token 计数
    private var promptTokens: Int = 0
    /// completion token 计数
    private var completionTokens: Int = 0
    /// reasoning token 计数（来自 completion_tokens_details）
    private var reasoningTokens: Int = 0

    /// 客户端请求的模型名，用于替换上游返回的 model 字段
    private let requestedModel: String
    /// 替换后的请求 ID
    private let requestID: String

    // MARK: - Init

    init(requestedModel: String) {
        self.requestedModel = requestedModel
        self.requestID = "chatcmpl-" + Self.randomHex(12)
    }

    // MARK: - Process

    /// 处理一行上游 SSE 数据，返回零条或多条要写入客户端的 SSE 字符串
    func process(line: String) -> [String] {
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty else { return [] }

        let (dataStr, done, ok) = Self.parseSSELine(trimmed)
        guard ok else { return [] }

        // 记录 TFFT
        if tfft == nil {
            tfft = Date()
        }

        if done {
            // 流结束 — 可在此报告遥测
            if reasoningTokens > 0 {
                NSLog("stream: model=\(requestedModel) prompt_tokens=\(promptTokens) completion_tokens=\(completionTokens) reasoning_tokens=\(reasoningTokens)")
            }
            return ["data: [DONE]\n\n"]
        }

        guard let data = dataStr.data(using: .utf8),
              var chunk = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            return []
        }

        // 提取 usage
        if let usage = chunk["usage"] as? [String: Any] {
            if let pt = usage["prompt_tokens"] as? Int { promptTokens = pt }
            else if let pt = usage["prompt_tokens"] as? Double { promptTokens = Int(pt) }

            if let ct = usage["completion_tokens"] as? Int { completionTokens = ct }
            else if let ct = usage["completion_tokens"] as? Double { completionTokens = Int(ct) }

            if let details = usage["completion_tokens_details"] as? [String: Any] {
                if let rt = details["reasoning_tokens"] as? Int { reasoningTokens = rt }
                else if let rt = details["reasoning_tokens"] as? Double { reasoningTokens = Int(rt) }
            }
        }

        // 替换 model 和 id
        if chunk["choices"] != nil {
            chunk["model"] = requestedModel
            chunk["id"] = requestID
            Self.cleanChunkChoices(&chunk)
        }

        guard let cleaned = try? JSONSerialization.data(withJSONObject: chunk),
              let cleanedStr = String(data: cleaned, encoding: .utf8) else {
            return []
        }

        return ["data: \(cleanedStr)\n\n"]
    }

    // MARK: - Error Output

    /// 生成 SSE 错误输出（OpenAI 格式）
    static func sseError(message: String) -> String {
        let msg = message
            .replacingOccurrences(of: "\"", with: "\\\"")
            .replacingOccurrences(of: "\n", with: "\\n")
        return "data: {\"error\":{\"message\":\"\(msg)\",\"type\":\"upstream_error\"}}\n\ndata: [DONE]\n\n"
    }

    /// 生成上下文超限 SSE 错误（OpenAI 格式）
    static func sseContextLimitError(message: String) -> String {
        let msg = "request too large: " + message
            .replacingOccurrences(of: "\"", with: "\\\"")
            .replacingOccurrences(of: "\n", with: "\\n")
        return "data: {\"error\":{\"message\":\"\(msg)\",\"type\":\"invalid_request_error\"}}\n\ndata: [DONE]\n\n"
    }

    // MARK: - Collect Chunks (Non-Streaming)

    /// 从上游字节流收集所有 chunk，返回 CollectedResult（用于非流式请求）
    static func collectChunks(from bytes: URLSession.AsyncBytes, requestedModel: String) async throws -> CollectedResult {
        var result = CollectedResult(model: requestedModel)
        let requestID = "chatcmpl-" + randomHex(12)
        result.id = requestID

        for try await line in bytes.lines {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            guard !trimmed.isEmpty else { continue }

            let (dataStr, done, ok) = parseSSELine(trimmed)
            guard ok, !done else { continue }

            guard let data = dataStr.data(using: .utf8),
                  let chunk = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
                continue
            }

            // 提取 choices
            if let choices = chunk["choices"] as? [[String: Any]] {
                for choice in choices {
                    if let delta = choice["delta"] as? [String: Any] {
                        if let content = delta["content"] as? String, !content.isEmpty {
                            result.content += content
                        }
                        if let reasoning = delta["reasoning_content"] as? String, !reasoning.isEmpty {
                            result.reasoningContent += reasoning
                        }
                        if let toolCalls = delta["tool_calls"] as? [[String: Any]] {
                            for tcMap in toolCalls {
                                let idx = intFromMap(tcMap, key: "index")

                                // 确保 toolCalls 数组够长
                                while result.toolCalls.count <= idx {
                                    result.toolCalls.append([
                                        "id": "",
                                        "type": "function",
                                        "function": ["name": "", "arguments": ""] as [String: Any],
                                    ] as [String: Any])
                                }

                                if let id = tcMap["id"] as? String, !id.isEmpty {
                                    if var tc = result.toolCalls[idx] as? [String: Any] {
                                        tc["id"] = id
                                        result.toolCalls[idx] = tc
                                    }
                                }
                                if let fn = tcMap["function"] as? [String: Any] {
                                    if var tc = result.toolCalls[idx] as? [String: Any],
                                       var existing = tc["function"] as? [String: Any] {
                                        if let name = fn["name"] as? String, !name.isEmpty {
                                            existing["name"] = name
                                        }
                                        if let args = fn["arguments"] as? String, !args.isEmpty {
                                            let prevArgs = existing["arguments"] as? String ?? ""
                                            existing["arguments"] = prevArgs + args
                                        }
                                        tc["function"] = existing
                                        result.toolCalls[idx] = tc
                                    }
                                }
                            }
                        }
                    }
                    if let fr = choice["finish_reason"] as? String, !fr.isEmpty {
                        result.finishReason = fr
                    }
                }
            }

            // 提取 usage
            if let usage = chunk["usage"] as? [String: Any] {
                if let pt = usage["prompt_tokens"] as? Int { result.promptTokens = pt }
                else if let pt = usage["prompt_tokens"] as? Double { result.promptTokens = Int(pt) }

                if let ct = usage["completion_tokens"] as? Int { result.completionTokens = ct }
                else if let ct = usage["completion_tokens"] as? Double { result.completionTokens = Int(ct) }

                if let details = usage["prompt_tokens_details"] as? [String: Any] {
                    if let ct = details["cached_tokens"] as? Int, ct > 0 { result.cacheCreationTokens = ct }
                    else if let ct = details["cached_tokens"] as? Double, Int(ct) > 0 { result.cacheCreationTokens = Int(ct) }
                    if let cct = details["cache_creation_tokens"] as? Int, cct > 0 { result.cacheCreationTokens = cct }
                    else if let cct = details["cache_creation_tokens"] as? Double, Int(cct) > 0 { result.cacheCreationTokens = Int(cct) }
                }
                if let details = usage["completion_tokens_details"] as? [String: Any] {
                    if let rt = details["reasoning_tokens"] as? Int { result.reasoningTokens = rt }
                    else if let rt = details["reasoning_tokens"] as? Double { result.reasoningTokens = Int(rt) }
                }
            }
        }

        return result
    }

    // MARK: - Private Helpers

    /// 解析 SSE 行，提取 data 内容
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

    /// 清理 chunk choices，只保留标准字段（原地修改）
    private static func cleanChunkChoices(_ chunk: inout [String: Any]) {
        guard var choices = chunk["choices"] as? [[String: Any]] else { return }

        for i in 0..<choices.count {
            var choice = choices[i]

            if var delta = choice["delta"] as? [String: Any] {
                // 只保留 role, content, tool_calls, reasoning_content
                let allowedDeltaKeys: Set<String> = ["role", "content", "tool_calls", "reasoning_content"]
                for key in delta.keys where !allowedDeltaKeys.contains(key) {
                    delta.removeValue(forKey: key)
                }
                // 移除空的 tool_calls 数组
                if let tcs = delta["tool_calls"] as? [[String: Any]], tcs.isEmpty {
                    delta.removeValue(forKey: "tool_calls")
                }
                choice["delta"] = delta
            }

            // 只保留 index, delta, finish_reason
            let allowedChoiceKeys: Set<String> = ["index", "delta", "finish_reason"]
            for key in choice.keys where !allowedChoiceKeys.contains(key) {
                choice.removeValue(forKey: key)
            }

            // 规范化 finish_reason
            if let fr = choice["finish_reason"] as? String, !fr.isEmpty {
                switch fr {
                case "stop", "tool_calls", "length", "content_filter":
                    break
                default:
                    choice["finish_reason"] = "stop"
                }
            }

            choices[i] = choice
        }

        chunk["choices"] = choices
    }

    /// 从字典中提取整数值（兼容 Int 和 Double）
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
