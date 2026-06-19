using System;
using System.Collections.Generic;
using System.Linq;
using System.Text;
using System.Text.Json;

namespace UniviewCodeBuddyProxy.Services;

/// <summary>
/// OpenAI to Anthropic Messages SSE state machine translator.
/// Tracks textBlockIdx, thinkingBlockIdx, toolBlockIdxMap.
/// </summary>
public sealed class AnthropicStreamTranslator
{
    // ═══ State ═══

    private int _nextBlockIdx;
    private int? _thinkingBlockIdx;
    private int? _textBlockIdx;
    private readonly Dictionary<int, int> _toolBlockIdxMap = new();
    private readonly Dictionary<int, bool> _toolBlocksStarted = new();
    private int _inputTokens;
    private int _outputTokens;
    private int _cacheCreationTokens;
    private bool _started;
    private bool _finished;

    private readonly string _requestedModel;
    private readonly string _msgID;

    // ═══ Constructor ═══

    public AnthropicStreamTranslator(string requestedModel)
    {
        _requestedModel = requestedModel;
        _msgID = "msg_" + RandomHex(24);
    }

    // ═══ Process ═══

    /// <summary>
    /// Process a single upstream SSE line, returning zero or more Anthropic SSE event strings.
    /// </summary>
    public List<string> ProcessLine(string line)
    {
        var trimmed = line.Trim();
        if (string.IsNullOrEmpty(trimmed)) return [];

        var (dataStr, done, ok) = ParseSSELine(trimmed);
        if (!ok) return [];

        if (done)
        {
            if (!_finished)
            {
                _finished = true;
                var events = CloseOpenBlocks();
                events.Add(AnthropicSSE("message_delta", new Dictionary<string, object?>
                {
                    ["type"] = "message_delta",
                    ["delta"] = new Dictionary<string, object?> { ["stop_reason"] = "end_turn", ["stop_sequence"] = null },
                    ["usage"] = new Dictionary<string, int> { ["input_tokens"] = _inputTokens, ["output_tokens"] = _outputTokens },
                }));
                events.Add(AnthropicSSE("message_stop", new Dictionary<string, object> { ["type"] = "message_stop" }));
                return events;
            }
            return [];
        }

        JsonElement chunk;
        try
        {
            chunk = JsonDocument.Parse(dataStr).RootElement;
        }
        catch
        {
            return [];
        }

        // Extract usage
        ExtractUsage(chunk);

        var events = new List<string>();

        if (!chunk.TryGetProperty("choices", out var choices) || choices.ValueKind != JsonValueKind.Array)
            return [];

        foreach (var choice in choices.EnumerateArray())
        {
            if (!choice.TryGetProperty("delta", out var delta)) continue;
            var fr = choice.TryGetProperty("finish_reason", out var frEl) ? frEl.GetString() ?? "" : "";

            // First content -> send message_start
            if (!_started)
            {
                _started = true;
                events.Add(AnthropicSSE("message_start", new Dictionary<string, object?>
                {
                    ["type"] = "message_start",
                    ["message"] = new Dictionary<string, object?>
                    {
                        ["id"] = _msgID,
                        ["type"] = "message",
                        ["role"] = "assistant",
                        ["content"] = new List<object>(),
                        ["model"] = _requestedModel,
                        ["stop_reason"] = null,
                        ["stop_sequence"] = null,
                        ["usage"] = new Dictionary<string, int>
                        {
                            ["input_tokens"] = _inputTokens,
                            ["output_tokens"] = 1,
                            ["cache_creation_input_tokens"] = 0,
                            ["cache_read_input_tokens"] = _cacheCreationTokens,
                        },
                    },
                }));
            }

            // ── reasoning_content (thinking blocks) ──

            if (delta.TryGetProperty("reasoning_content", out var rcEl))
            {
                var reasoningContent = rcEl.GetString() ?? "";
                if (!string.IsNullOrEmpty(reasoningContent))
                {
                    if (_thinkingBlockIdx == null)
                    {
                        _thinkingBlockIdx = _nextBlockIdx++;
                        events.Add(AnthropicSSE("content_block_start", new Dictionary<string, object>
                        {
                            ["type"] = "content_block_start",
                            ["index"] = _thinkingBlockIdx.Value,
                            ["content_block"] = new Dictionary<string, string> { ["type"] = "thinking", ["thinking"] = "", ["signature"] = "" },
                        }));
                    }
                    events.Add(AnthropicSSE("content_block_delta", new Dictionary<string, object>
                    {
                        ["type"] = "content_block_delta",
                        ["index"] = _thinkingBlockIdx!.Value,
                        ["delta"] = new Dictionary<string, string> { ["type"] = "thinking_delta", ["thinking"] = reasoningContent },
                    }));
                }
            }

            // ── content (text blocks) ──

            if (delta.TryGetProperty("content", out var contentEl))
            {
                var content = contentEl.GetString() ?? "";
                if (!string.IsNullOrEmpty(content))
                {
                    // thinking -> text: close thinking block first
                    if (_thinkingBlockIdx.HasValue)
                    {
                        events.AddRange(EmitThinkingSignatureDelta(_thinkingBlockIdx.Value));
                        events.Add(AnthropicSSE("content_block_stop", new Dictionary<string, object>
                        {
                            ["type"] = "content_block_stop",
                            ["index"] = _thinkingBlockIdx.Value,
                        }));
                        _thinkingBlockIdx = null;
                    }

                    if (_textBlockIdx == null)
                    {
                        _textBlockIdx = _nextBlockIdx++;
                        events.Add(AnthropicSSE("content_block_start", new Dictionary<string, object>
                        {
                            ["type"] = "content_block_start",
                            ["index"] = _textBlockIdx.Value,
                            ["content_block"] = new Dictionary<string, string> { ["type"] = "text", ["text"] = "" },
                        }));
                    }
                    events.Add(AnthropicSSE("content_block_delta", new Dictionary<string, object>
                    {
                        ["type"] = "content_block_delta",
                        ["index"] = _textBlockIdx!.Value,
                        ["delta"] = new Dictionary<string, string> { ["type"] = "text_delta", ["text"] = content },
                    }));
                }
            }

            // ── tool_calls ──

            if (delta.TryGetProperty("tool_calls", out var toolCallsEl) && toolCallsEl.ValueKind == JsonValueKind.Array)
            {
                foreach (var tcMap in toolCallsEl.EnumerateArray())
                {
                    var tcIdx = IntFromMap(tcMap, "index");

                    if (!_toolBlockIdxMap.ContainsKey(tcIdx))
                    {
                        _toolBlockIdxMap[tcIdx] = _nextBlockIdx++;
                        _toolBlocksStarted[tcIdx] = false;
                    }

                    var blockIdx = _toolBlockIdxMap[tcIdx];

                    if (!(_toolBlocksStarted.GetValueOrDefault(tcIdx, false)))
                    {
                        // Close thinking/text block first
                        if (_thinkingBlockIdx.HasValue)
                        {
                            events.AddRange(EmitThinkingSignatureDelta(_thinkingBlockIdx.Value));
                            events.Add(AnthropicSSE("content_block_stop", new Dictionary<string, object>
                            {
                                ["type"] = "content_block_stop",
                                ["index"] = _thinkingBlockIdx.Value,
                            }));
                            _thinkingBlockIdx = null;
                        }
                        if (_textBlockIdx.HasValue)
                        {
                            events.Add(AnthropicSSE("content_block_stop", new Dictionary<string, object>
                            {
                                ["type"] = "content_block_stop",
                                ["index"] = _textBlockIdx.Value,
                            }));
                            _textBlockIdx = null;
                        }

                        if (tcMap.TryGetProperty("id", out var idEl))
                        {
                            var id = idEl.GetString() ?? "";
                            if (!string.IsNullOrEmpty(id))
                            {
                                _toolBlocksStarted[tcIdx] = true;
                                var fnName = "";
                                if (tcMap.TryGetProperty("function", out var fnEl) && fnEl.TryGetProperty("name", out var nameEl))
                                    fnName = nameEl.GetString() ?? "";

                                events.Add(AnthropicSSE("content_block_start", new Dictionary<string, object>
                                {
                                    ["type"] = "content_block_start",
                                    ["index"] = blockIdx,
                                    ["content_block"] = new Dictionary<string, object>
                                    {
                                        ["type"] = "tool_use",
                                        ["id"] = id,
                                        ["name"] = fnName,
                                        ["input"] = new Dictionary<string, object>(),
                                    },
                                }));
                            }
                        }
                    }

                    if (_toolBlocksStarted.GetValueOrDefault(tcIdx, false))
                    {
                        if (tcMap.TryGetProperty("function", out var fnEl) && fnEl.TryGetProperty("arguments", out var argsEl))
                        {
                            var args = argsEl.GetString() ?? "";
                            if (!string.IsNullOrEmpty(args))
                            {
                                events.Add(AnthropicSSE("content_block_delta", new Dictionary<string, object>
                                {
                                    ["type"] = "content_block_delta",
                                    ["index"] = blockIdx,
                                    ["delta"] = new Dictionary<string, string> { ["type"] = "input_json_delta", ["partial_json"] = args },
                                }));
                            }
                        }
                    }
                }
            }

            // ── finish_reason ──

            if (!string.IsNullOrEmpty(fr) && !_finished)
            {
                _finished = true;
                events.AddRange(CloseOpenBlocks());
                events.Add(AnthropicSSE("message_delta", new Dictionary<string, object?>
                {
                    ["type"] = "message_delta",
                    ["delta"] = new Dictionary<string, object?> { ["stop_reason"] = FinishReasonToStopReason(fr), ["stop_sequence"] = null },
                    ["usage"] = new Dictionary<string, int> { ["input_tokens"] = _inputTokens, ["output_tokens"] = _outputTokens },
                }));
                events.Add(AnthropicSSE("message_stop", new Dictionary<string, object> { ["type"] = "message_stop" }));
            }
        }

        return events;
    }

    // ═══ Close ═══

    /// <summary>Close the stream (on end or error), returning remaining events.</summary>
    public List<string> Close()
    {
        if (_finished) return [];
        _finished = true;

        var events = CloseOpenBlocks();
        events.Add(AnthropicSSE("message_delta", new Dictionary<string, object?>
        {
            ["type"] = "message_delta",
            ["delta"] = new Dictionary<string, object?> { ["stop_reason"] = "stop_sequence", ["stop_sequence"] = "<stream_error>" },
            ["usage"] = new Dictionary<string, int> { ["input_tokens"] = _inputTokens, ["output_tokens"] = _outputTokens },
        }));
        events.Add(AnthropicSSE("message_stop", new Dictionary<string, object> { ["type"] = "message_stop" }));
        return events;
    }

    // ═══ Error Output ═══

    /// <summary>Generate Anthropic format SSE error.</summary>
    public static string SseError(string message)
    {
        var msg = EscapeForSSE(message);
        return "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_error\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"\",\"stop_reason\":null,\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}}\n\n"
             + $"event: error\ndata: {{\"type\":\"error\",\"error\":{{\"type\":\"api_error\",\"message\":\"{msg}\"}}}}\n\n"
             + "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n";
    }

    /// <summary>Generate Anthropic format SSE context limit error.</summary>
    public static string SseContextLimitError(string message)
    {
        var msg = EscapeForSSE("request too large: " + message);
        return "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_ctxlimit\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"\",\"stop_reason\":null,\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}}\n\n"
             + $"event: error\ndata: {{\"type\":\"error\",\"error\":{{\"type\":\"invalid_request_error\",\"message\":\"{msg}\"}}}}\n\n"
             + "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n";
    }

    // ═══ Private Helpers ═══

    private List<string> CloseOpenBlocks()
    {
        var events = new List<string>();

        // 1. Close thinking block (emit signature_delta first)
        if (_thinkingBlockIdx.HasValue)
        {
            events.AddRange(EmitThinkingSignatureDelta(_thinkingBlockIdx.Value));
            events.Add(AnthropicSSE("content_block_stop", new Dictionary<string, object>
            {
                ["type"] = "content_block_stop",
                ["index"] = _thinkingBlockIdx.Value,
            }));
            _thinkingBlockIdx = null;
        }

        // 2. Close text block
        if (_textBlockIdx.HasValue)
        {
            events.Add(AnthropicSSE("content_block_stop", new Dictionary<string, object>
            {
                ["type"] = "content_block_stop",
                ["index"] = _textBlockIdx.Value,
            }));
            _textBlockIdx = null;
        }

        // 3. Close tool blocks (sorted by index)
        foreach (var tcIdx in _toolBlocksStarted.Keys.OrderBy(k => k))
        {
            if (_toolBlocksStarted[tcIdx] && _toolBlockIdxMap.TryGetValue(tcIdx, out var blockIdx))
            {
                events.Add(AnthropicSSE("content_block_stop", new Dictionary<string, object>
                {
                    ["type"] = "content_block_stop",
                    ["index"] = blockIdx,
                }));
            }
        }

        return events;
    }

    private List<string> EmitThinkingSignatureDelta(int blockIdx)
    {
        return
        [
            AnthropicSSE("content_block_delta", new Dictionary<string, object>
            {
                ["type"] = "content_block_delta",
                ["index"] = blockIdx,
                ["delta"] = new Dictionary<string, string> { ["type"] = "signature_delta", ["signature"] = "" },
            }),
        ];
    }

    private void ExtractUsage(JsonElement chunk)
    {
        if (!chunk.TryGetProperty("usage", out var usage)) return;

        if (usage.TryGetProperty("prompt_tokens", out var pt))
        {
            var v = GetInt(pt);
            if (v > 0) _inputTokens = v;
        }
        if (usage.TryGetProperty("completion_tokens", out var ct))
        {
            var v = GetInt(ct);
            if (v > 0) _outputTokens = v;
        }
        if (usage.TryGetProperty("prompt_tokens_details", out var ptd) &&
            ptd.TryGetProperty("cached_tokens", out var cachedEl))
        {
            var v = GetInt(cachedEl);
            if (v > 0) _cacheCreationTokens = v;
        }
    }

    private static string FinishReasonToStopReason(string finishReason) => finishReason switch
    {
        "stop" => "end_turn",
        "tool_calls" => "tool_use",
        "length" => "max_tokens",
        "content_filter" => "end_turn",
        _ => "end_turn",
    };

    private string AnthropicSSE(string eventType, Dictionary<string, object?> data)
    {
        var json = JsonSerializer.Serialize(data, new JsonSerializerOptions
        {
            DefaultIgnoreCondition = System.Text.Json.Serialization.JsonIgnoreCondition.WhenWritingNull,
        });
        return $"event: {eventType}\ndata: {json}\n\n";
    }

    private static (string Data, bool Done, bool Ok) ParseSSELine(string line)
    {
        var dataStr = line;
        if (line.StartsWith("data: "))
            dataStr = line[6..];
        else if (line.StartsWith("data:"))
            dataStr = line[5..];
        else
            return ("", false, false);

        dataStr = dataStr.Trim();
        if (dataStr == "[DONE]") return ("", true, true);
        if (string.IsNullOrEmpty(dataStr)) return ("", false, false);
        return (dataStr, false, true);
    }

    private static int IntFromMap(JsonElement map, string key)
    {
        if (!map.TryGetProperty(key, out var el)) return 0;
        if (el.ValueKind == JsonValueKind.Number) return el.GetInt32();
        return 0;
    }

    private static int GetInt(JsonElement el)
    {
        if (el.ValueKind == JsonValueKind.Number) return el.GetInt32();
        return 0;
    }

    private static string RandomHex(int n)
    {
        var bytes = new byte[n];
        Random.Shared.NextBytes(bytes);
        return Convert.ToHexString(bytes).ToLowerInvariant();
    }

    private static string EscapeForSSE(string s) =>
        s.Replace("\\", "\\\\").Replace("\"", "\\\"").Replace("\n", "\\n");
}
