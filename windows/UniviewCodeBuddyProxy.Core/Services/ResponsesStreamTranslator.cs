using System;
using System.Collections.Generic;
using System.Linq;
using System.Text;
using System.Text.Json;

namespace UniviewCodeBuddyProxy.Services;

/// <summary>
/// OpenAI to Responses API SSE state machine translator.
/// Emits response.created/in_progress/output_item.added/content_part.added/delta/done events.
/// </summary>
public sealed class ResponsesStreamTranslator
{
    // ═══ Tool Call State ═══

    private sealed class ToolCallState
    {
        public string ID { get; set; } = "";
        public string Name { get; set; } = "";
        public string Arguments { get; set; } = "";
        public int OutputIndex { get; set; }
        public bool Started { get; set; }
    }

    // ═══ State ═══

    private bool _started;
    private bool _finished;
    private bool _textStarted;
    private int _contentIndex;
    private int _promptTokens;
    private int _completionTokens;
    private int _totalTokens;
    private int _cacheReadInputTokens;
    private int _cacheCreationInputTokens;
    private double _credit;
    private string? _finishReason;
    private string _fullContent = "";
    private readonly Dictionary<int, ToolCallState> _toolCalls = new();
    private readonly List<int> _toolCallOrder = new();

    private readonly string _requestedModel;
    private readonly string _respID;
    private readonly string _outputItemID;

    // ═══ Constructor ═══

    public ResponsesStreamTranslator(string requestedModel)
    {
        _requestedModel = requestedModel;
        _respID = "resp_" + RandomHex(24);
        _outputItemID = "msg_" + RandomHex(24);
    }

    // ═══ Process ═══

    /// <summary>
    /// Process a single upstream SSE line, returning zero or more Responses API SSE event strings.
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
                return EmitFinish();
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
        if (chunk.TryGetProperty("usage", out var usage))
        {
            if (usage.TryGetProperty("prompt_tokens", out var pt))
                _promptTokens = GetInt(pt);
            if (usage.TryGetProperty("completion_tokens", out var ct))
                _completionTokens = GetInt(ct);
            if (usage.TryGetProperty("total_tokens", out var tt) && tt.ValueKind == JsonValueKind.Number)
                _totalTokens = tt.GetInt32();
            if (usage.TryGetProperty("credit", out var cr) && cr.ValueKind == JsonValueKind.Number)
                _credit = cr.GetDouble();
            if (usage.TryGetProperty("prompt_tokens_details", out var ptd))
            {
                if (ptd.TryGetProperty("cached_tokens", out var cachedEl))
                {
                    var v = GetInt(cachedEl);
                    if (v > 0) _cacheReadInputTokens = v;
                }
            }
            if (usage.TryGetProperty("prompt_cache_hit_tokens", out var hit) && hit.ValueKind == JsonValueKind.Number)
            {
                var v = hit.GetInt32();
                if (v > 0) _cacheReadInputTokens = v;
            }
            if (usage.TryGetProperty("prompt_cache_miss_tokens", out var miss) && miss.ValueKind == JsonValueKind.Number)
            {
                var v = miss.GetInt32();
                if (v > 0) _cacheCreationInputTokens = v;
            }
            if (usage.TryGetProperty("prompt_cache_write_tokens", out var write) && write.ValueKind == JsonValueKind.Number)
            {
                var v = write.GetInt32();
                if (v > 0) _cacheCreationInputTokens = v;
            }
        }

        var events = new List<string>();

        if (!chunk.TryGetProperty("choices", out var choices) || choices.ValueKind != JsonValueKind.Array)
            return [];

        foreach (var choice in choices.EnumerateArray())
        {
            if (!choice.TryGetProperty("delta", out var delta)) continue;

            // First content -> send response.created + response.in_progress
            if (!_started)
            {
                _started = true;
                events.AddRange(EmitResponseCreated());
            }

            // ── content (text) ──

            if (delta.TryGetProperty("content", out var contentEl))
            {
                var content = contentEl.GetString() ?? "";
                if (!string.IsNullOrEmpty(content))
                {
                    events.AddRange(EmitTextStartIfNeeded());
                    _fullContent += content;
                    events.Add(ResponsesSSE("response.output_text.delta", new Dictionary<string, object>
                    {
                        ["type"] = "response.output_text.delta",
                        ["output_index"] = 0,
                        ["content_index"] = _contentIndex,
                        ["delta"] = content,
                    }));
                }
            }

            // ── tool_calls ──

            if (delta.TryGetProperty("tool_calls", out var toolCallsEl) && toolCallsEl.ValueKind == JsonValueKind.Array)
            {
                foreach (var tcMap in toolCallsEl.EnumerateArray())
                {
                    var tcIdx = IntFromMap(tcMap, "index");

                    if (!_toolCalls.ContainsKey(tcIdx))
                    {
                        _toolCalls[tcIdx] = new ToolCallState();
                        _toolCallOrder.Add(tcIdx);
                    }

                    var tcState = _toolCalls[tcIdx];

                    // Has ID -> first appearance of this tool_call
                    if (tcMap.TryGetProperty("id", out var idEl))
                    {
                        var id = idEl.GetString() ?? "";
                        if (!string.IsNullOrEmpty(id) && !tcState.Started)
                        {
                            tcState.Started = true;
                            tcState.ID = id;
                            var fnName = "";
                            if (tcMap.TryGetProperty("function", out var fnEl) && fnEl.TryGetProperty("name", out var nameEl))
                            {
                                fnName = nameEl.GetString() ?? "";
                                tcState.Name = fnName;
                            }

                            var outputIdx = ComputeToolOutputIndex(tcIdx);
                            events.Add(ResponsesSSE("response.output_item.added", new Dictionary<string, object>
                            {
                                ["type"] = "response.output_item.added",
                                ["output_index"] = outputIdx,
                                ["item"] = new Dictionary<string, object>
                                {
                                    ["type"] = "function_call",
                                    ["id"] = id,
                                    ["call_id"] = id,
                                    ["name"] = fnName,
                                    ["status"] = "in_progress",
                                },
                            }));
                        }
                    }

                    // Function arguments delta
                    if (tcMap.TryGetProperty("function", out var fnEl2))
                    {
                        if (fnEl2.TryGetProperty("arguments", out var argsEl))
                        {
                            var args = argsEl.GetString() ?? "";
                            if (!string.IsNullOrEmpty(args))
                            {
                                tcState.Arguments += args;
                                if (tcState.Started)
                                {
                                    var outputIdx = ComputeToolOutputIndex(tcIdx);
                                    events.Add(ResponsesSSE("response.function_call_arguments.delta", new Dictionary<string, object>
                                    {
                                        ["type"] = "response.function_call_arguments.delta",
                                        ["output_index"] = outputIdx,
                                        ["item_id"] = tcState.ID,
                                        ["delta"] = args,
                                    }));
                                }
                            }
                        }
                        // Also extract name (may appear in subsequent chunks)
                        if (fnEl2.TryGetProperty("name", out var nameEl2))
                        {
                            var name = nameEl2.GetString() ?? "";
                            if (!string.IsNullOrEmpty(name) && string.IsNullOrEmpty(tcState.Name))
                                tcState.Name = name;
                        }
                    }
                }
            }

            // ── finish_reason ──

            if (choice.TryGetProperty("finish_reason", out var frEl))
            {
                var fr = frEl.GetString() ?? "";
                if (!string.IsNullOrEmpty(fr) && !_finished)
                    _finishReason = fr;
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

        var events = new List<string>();

        if (_started)
        {
            events.AddRange(CloseTextBlock());
            events.AddRange(CloseToolCallBlocks());
        }
        else
        {
            events.AddRange(EmitResponseCreated());
        }

        events.Add(ResponsesSSE("response.completed", new Dictionary<string, object>
        {
            ["type"] = "response.completed",
            ["response"] = BuildResponsesSSEObject("incomplete", null),
        }));

        return events;
    }

    // ═══ Error Output ═══

    /// <summary>Generate Responses API format SSE error.</summary>
    public static string SseError(string message)
    {
        var msg = EscapeForSSE(message);
        return $"event: error\ndata: {{\"type\":\"error\",\"error\":{{\"type\":\"server_error\",\"message\":\"{msg}\"}}}}\n\n";
    }

    /// <summary>Generate Responses API format SSE context limit error.</summary>
    public static string SseContextLimitError(string message)
    {
        var msg = EscapeForSSE("request too large: " + message);
        return $"event: error\ndata: {{\"type\":\"error\",\"error\":{{\"type\":\"invalid_request_error\",\"message\":\"{msg}\"}}}}\n\n";
    }

    // ═══ Private Event Emitters ═══

    private List<string> EmitResponseCreated()
    {
        var createdResp = BuildResponsesSSEObject("in_progress", null);
        return
        [
            ResponsesSSE("response.created", new Dictionary<string, object>
            {
                ["type"] = "response.created",
                ["response"] = createdResp,
            }),
            ResponsesSSE("response.in_progress", new Dictionary<string, object>
            {
                ["type"] = "response.in_progress",
                ["response"] = createdResp,
            }),
        ];
    }

    private List<string> EmitTextStartIfNeeded()
    {
        if (_textStarted) return [];
        _textStarted = true;

        return
        [
            ResponsesSSE("response.output_item.added", new Dictionary<string, object>
            {
                ["type"] = "response.output_item.added",
                ["output_index"] = 0,
                ["item"] = new Dictionary<string, object>
                {
                    ["type"] = "message",
                    ["id"] = _outputItemID,
                    ["status"] = "in_progress",
                    ["role"] = "assistant",
                    ["content"] = new List<object>(),
                },
            }),
            ResponsesSSE("response.content_part.added", new Dictionary<string, object>
            {
                ["type"] = "response.content_part.added",
                ["output_index"] = 0,
                ["content_index"] = _contentIndex,
                ["part"] = new Dictionary<string, string> { ["type"] = "output_text", ["text"] = "" },
            }),
        ];
    }

    private List<string> EmitFinish()
    {
        var events = new List<string>();
        events.AddRange(CloseTextBlock());
        events.AddRange(CloseToolCallBlocks());
        events.AddRange(EmitCompleted());
        return events;
    }

    private List<string> CloseTextBlock()
    {
        if (!_textStarted) return [];

        return
        [
            ResponsesSSE("response.output_text.done", new Dictionary<string, object>
            {
                ["type"] = "response.output_text.done",
                ["output_index"] = 0,
                ["content_index"] = _contentIndex,
                ["text"] = _fullContent,
            }),
            ResponsesSSE("response.content_part.done", new Dictionary<string, object>
            {
                ["type"] = "response.content_part.done",
                ["output_index"] = 0,
                ["content_index"] = _contentIndex,
                ["part"] = new Dictionary<string, object>
                {
                    ["type"] = "output_text",
                    ["text"] = _fullContent,
                    ["annotations"] = new List<object>(),
                },
            }),
            ResponsesSSE("response.output_item.done", new Dictionary<string, object>
            {
                ["type"] = "response.output_item.done",
                ["output_index"] = 0,
                ["item"] = new Dictionary<string, object>
                {
                    ["type"] = "message",
                    ["id"] = _outputItemID,
                    ["status"] = "completed",
                    ["role"] = "assistant",
                    ["content"] = new List<object>
                    {
                        new Dictionary<string, object>
                        {
                            ["type"] = "output_text",
                            ["text"] = _fullContent,
                            ["annotations"] = new List<object>(),
                        },
                    },
                },
            }),
        ];
    }

    private List<string> CloseToolCallBlocks()
    {
        var events = new List<string>();
        foreach (var tcIdx in _toolCallOrder)
        {
            var tc = _toolCalls.GetValueOrDefault(tcIdx);
            if (tc == null || !tc.Started) continue;
            var outputIdx = ComputeToolOutputIndex(tcIdx);

            events.Add(ResponsesSSE("response.function_call_arguments.done", new Dictionary<string, object>
            {
                ["type"] = "response.function_call_arguments.done",
                ["output_index"] = outputIdx,
                ["item_id"] = tc.ID,
                ["arguments"] = tc.Arguments,
            }));
            events.Add(ResponsesSSE("response.output_item.done", new Dictionary<string, object>
            {
                ["type"] = "response.output_item.done",
                ["output_index"] = outputIdx,
                ["item"] = new Dictionary<string, object>
                {
                    ["type"] = "function_call",
                    ["id"] = tc.ID,
                    ["call_id"] = tc.ID,
                    ["name"] = tc.Name,
                    ["arguments"] = tc.Arguments,
                    ["status"] = "completed",
                },
            }));
        }
        return events;
    }

    private List<string> EmitCompleted()
    {
        var status = _finishReason == "length" ? "incomplete" : "completed";

        var outputItems = new List<Dictionary<string, object>>();
        if (_textStarted)
        {
            outputItems.Add(new Dictionary<string, object>
            {
                ["type"] = "message",
                ["id"] = _outputItemID,
                ["status"] = "completed",
                ["role"] = "assistant",
                ["content"] = new List<object>
                {
                    new Dictionary<string, object>
                    {
                        ["type"] = "output_text",
                        ["text"] = _fullContent,
                        ["annotations"] = new List<object>(),
                    },
                },
            });
        }

        foreach (var tcIdx in _toolCallOrder)
        {
            var tc = _toolCalls.GetValueOrDefault(tcIdx);
            if (tc == null) continue;
            outputItems.Add(new Dictionary<string, object>
            {
                ["type"] = "function_call",
                ["id"] = tc.ID,
                ["call_id"] = tc.ID,
                ["name"] = tc.Name,
                ["arguments"] = tc.Arguments,
                ["status"] = "completed",
            });
        }

        var completedResp = BuildResponsesSSEObject(status, outputItems);
        return
        [
            ResponsesSSE("response.completed", new Dictionary<string, object>
            {
                ["type"] = "response.completed",
                ["response"] = completedResp,
            }),
        ];
    }

    // ═══ Private Helpers ═══

    private int ComputeToolOutputIndex(int tcIdx)
    {
        var @base = _textStarted ? 1 : 0;
        return @base + IndexOfInt(_toolCallOrder, tcIdx);
    }

    private Dictionary<string, object> BuildResponsesSSEObject(string status, List<Dictionary<string, object>>? outputItems)
    {
        return new Dictionary<string, object>
        {
            ["id"] = _respID,
            ["object"] = "response",
            ["created_at"] = (int)DateTimeOffset.UtcNow.ToUnixTimeSeconds(),
            ["model"] = _requestedModel,
            ["status"] = status,
            ["output"] = outputItems ?? new List<Dictionary<string, object>>(),
            ["usage"] = new Dictionary<string, int>
            {
                ["input_tokens"] = _promptTokens,
                ["output_tokens"] = _completionTokens,
                ["total_tokens"] = _totalTokens > 0 ? _totalTokens : _promptTokens + _completionTokens,
            },
            ["metadata"] = new Dictionary<string, object>(),
        };
    }

    private string ResponsesSSE(string eventType, Dictionary<string, object> data)
    {
        var json = JsonSerializer.Serialize(data);
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

    private static int IndexOfInt(List<int> list, int value)
    {
        for (var i = 0; i < list.Count; i++)
            if (list[i] == value) return i;
        return -1;
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
