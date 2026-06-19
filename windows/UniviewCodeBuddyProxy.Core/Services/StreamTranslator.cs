using System;
using System.Collections.Generic;
using System.Linq;
using System.Text;
using System.Text.Json;

namespace UniviewCodeBuddyProxy.Services;

/// <summary>
/// OpenAI to OpenAI SSE passthrough translator.
/// Replaces model/id, cleans chunk choices, extracts usage.
/// </summary>
public sealed class StreamTranslator
{
    // ═══ State ═══

    private int _promptTokens;
    private int _completionTokens;
    private int _reasoningTokens;

    private readonly string _requestedModel;
    private readonly string _requestID;

    // ═══ Constructor ═══

    public StreamTranslator(string requestedModel)
    {
        _requestedModel = requestedModel;
        _requestID = "chatcmpl-" + RandomHex(12);
    }

    // ═══ Process ═══

    /// <summary>
    /// Process a single upstream SSE line, returning zero or more output SSE strings.
    /// </summary>
    public List<string> ProcessLine(string line)
    {
        var trimmed = line.Trim();
        if (string.IsNullOrEmpty(trimmed)) return [];

        var (dataStr, done, ok) = ParseSSELine(trimmed);
        if (!ok) return [];

        if (done)
            return ["data: [DONE]\n\n"];

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

        // Replace model and id if there are choices
        string outputJson;
        if (chunk.TryGetProperty("choices", out _))
        {
            var modified = ModifyChunk(chunk);
            outputJson = modified.GetRawText();
        }
        else
        {
            outputJson = chunk.GetRawText();
        }

        return [$"data: {outputJson}\n\n"];
    }

    // ═══ Error Output ═══

    /// <summary>Generate SSE error output (OpenAI format).</summary>
    public static string SseError(string message)
    {
        var msg = EscapeForSSE(message);
        return $"data: {{\"error\":{{\"message\":\"{msg}\",\"type\":\"upstream_error\"}}}}\n\ndata: [DONE]\n\n";
    }

    /// <summary>Generate context limit SSE error (OpenAI format).</summary>
    public static string SseContextLimitError(string message)
    {
        var msg = EscapeForSSE("request too large: " + message);
        return $"data: {{\"error\":{{\"message\":\"{msg}\",\"type\":\"invalid_request_error\"}}}}\n\ndata: [DONE]\n\n";
    }

    // ═══ Collect Chunks (Non-Streaming) ═══

    /// <summary>
    /// Collect all upstream chunks and return a CollectedResult.
    /// </summary>
    public static CollectedResult CollectChunks(System.IO.Stream stream, string requestedModel)
    {
        var result = new CollectedResult { Model = requestedModel };
        var requestID = "chatcmpl-" + RandomHex(12);
        result.ID = requestID;

        using var reader = new System.IO.StreamReader(stream);
        string? line;
        while ((line = reader.ReadLine()) != null)
        {
            var trimmed = line.Trim();
            if (string.IsNullOrEmpty(trimmed)) continue;

            var (dataStr, done, ok) = ParseSSELine(trimmed);
            if (!ok || done) continue;

            JsonElement chunk;
            try
            {
                chunk = JsonDocument.Parse(dataStr).RootElement;
            }
            catch
            {
                continue;
            }

            // Extract choices
            if (chunk.TryGetProperty("choices", out var choices) && choices.ValueKind == JsonValueKind.Array)
            {
                foreach (var choice in choices.EnumerateArray())
                {
                    if (choice.TryGetProperty("delta", out var delta))
                    {
                        if (delta.TryGetProperty("content", out var contentEl))
                        {
                            var content = contentEl.GetString();
                            if (!string.IsNullOrEmpty(content))
                                result.Content += content;
                        }
                        if (delta.TryGetProperty("reasoning_content", out var reasoningEl))
                        {
                            var reasoning = reasoningEl.GetString();
                            if (!string.IsNullOrEmpty(reasoning))
                                result.ReasoningContent += reasoning;
                        }
                        if (delta.TryGetProperty("tool_calls", out var toolCallsEl) && toolCallsEl.ValueKind == JsonValueKind.Array)
                        {
                            foreach (var tcMap in toolCallsEl.EnumerateArray())
                            {
                                var idx = IntFromMap(tcMap, "index");

                                // Ensure toolCalls array is long enough
                                while (result.ToolCalls.Count <= idx)
                                {
                                    result.ToolCalls.Add(new Dictionary<string, object>
                                    {
                                        ["id"] = "",
                                        ["type"] = "function",
                                        ["function"] = new Dictionary<string, object>
                                        {
                                            ["name"] = "",
                                            ["arguments"] = "",
                                        },
                                    });
                                }

                                if (tcMap.TryGetProperty("id", out var idEl))
                                {
                                    var id = idEl.GetString();
                                    if (!string.IsNullOrEmpty(id))
                                        result.ToolCalls[idx]["id"] = id;
                                }
                                if (tcMap.TryGetProperty("function", out var fnEl))
                                {
                                    var tcDict = result.ToolCalls[idx];
                                    var fnDict = (Dictionary<string, object>)tcDict["function"];

                                    if (fnEl.TryGetProperty("name", out var nameEl))
                                    {
                                        var name = nameEl.GetString();
                                        if (!string.IsNullOrEmpty(name))
                                            fnDict["name"] = name;
                                    }
                                    if (fnEl.TryGetProperty("arguments", out var argsEl))
                                    {
                                        var args = argsEl.GetString();
                                        if (!string.IsNullOrEmpty(args))
                                            fnDict["arguments"] = (fnDict.GetValueOrDefault("arguments", "")?.ToString() ?? "") + args;
                                    }
                                }
                            }
                        }
                    }
                    if (choice.TryGetProperty("finish_reason", out var frEl))
                    {
                        var fr = frEl.GetString();
                        if (!string.IsNullOrEmpty(fr))
                            result.FinishReason = fr;
                    }
                }
            }

            // Extract usage
            if (chunk.TryGetProperty("usage", out var usage))
            {
                if (usage.TryGetProperty("prompt_tokens", out var pt))
                    result.PromptTokens = GetInt(pt);
                if (usage.TryGetProperty("completion_tokens", out var ct))
                    result.CompletionTokens = GetInt(ct);
                if (usage.TryGetProperty("prompt_tokens_details", out var ptd))
                {
                    if (ptd.TryGetProperty("cached_tokens", out var cachedEl))
                    {
                        var val = GetInt(cachedEl);
                        if (val > 0) result.CacheCreationTokens = val;
                    }
                    if (ptd.TryGetProperty("cache_creation_tokens", out var cctEl))
                    {
                        var val = GetInt(cctEl);
                        if (val > 0) result.CacheCreationTokens = val;
                    }
                }
                if (usage.TryGetProperty("completion_tokens_details", out var ctd))
                {
                    if (ctd.TryGetProperty("reasoning_tokens", out var rtEl))
                        result.ReasoningTokens = GetInt(rtEl);
                }
            }
        }

        return result;
    }

    // ═══ Private Helpers ═══

    private void ExtractUsage(JsonElement chunk)
    {
        if (!chunk.TryGetProperty("usage", out var usage)) return;

        if (usage.TryGetProperty("prompt_tokens", out var pt))
            _promptTokens = GetInt(pt);
        if (usage.TryGetProperty("completion_tokens", out var ct))
            _completionTokens = GetInt(ct);
        if (usage.TryGetProperty("completion_tokens_details", out var details) &&
            details.TryGetProperty("reasoning_tokens", out var rt))
            _reasoningTokens = GetInt(rt);
    }

    private JsonElement ModifyChunk(JsonElement chunk)
    {
        // We need to deserialize, modify, and re-serialize
        var dict = JsonSerializer.Deserialize<Dictionary<string, JsonElement>>(chunk.GetRawText()) ?? new();
        dict["model"] = JsonSerializer.Deserialize<JsonElement>($"\"{_requestedModel}\"");
        dict["id"] = JsonSerializer.Deserialize<JsonElement>($"\"{_requestID}\"");
        CleanChunkChoices(dict);
        var json = JsonSerializer.Serialize(dict);
        return JsonDocument.Parse(json).RootElement;
    }

    private static void CleanChunkChoices(Dictionary<string, JsonElement> chunk)
    {
        if (!chunk.TryGetValue("choices", out var choicesEl)) return;
        var choices = JsonSerializer.Deserialize<List<Dictionary<string, JsonElement>>>(choicesEl.GetRawText());
        if (choices == null) return;

        var allowedDeltaKeys = new HashSet<string> { "role", "content", "tool_calls", "reasoning_content" };
        var allowedChoiceKeys = new HashSet<string> { "index", "delta", "finish_reason" };
        var validFinishReasons = new HashSet<string> { "stop", "tool_calls", "length", "content_filter" };

        for (var i = 0; i < choices.Count; i++)
        {
            var choice = choices[i];

            // Clean delta
            if (choice.TryGetValue("delta", out var deltaEl))
            {
                var deltaDict = JsonSerializer.Deserialize<Dictionary<string, JsonElement>>(deltaEl.GetRawText()) ?? new();
                var deltaKeysToRemove = deltaDict.Keys.Where(k => !allowedDeltaKeys.Contains(k)).ToList();
                foreach (var key in deltaKeysToRemove)
                    deltaDict.Remove(key);

                // Remove empty tool_calls arrays
                if (deltaDict.TryGetValue("tool_calls", out var tcsEl) && tcsEl.ValueKind == JsonValueKind.Array && tcsEl.GetArrayLength() == 0)
                    deltaDict.Remove("tool_calls");

                choice["delta"] = JsonSerializer.Deserialize<JsonElement>(JsonSerializer.Serialize(deltaDict));
            }

            // Remove non-standard choice keys
            var choiceKeysToRemove = choice.Keys.Where(k => !allowedChoiceKeys.Contains(k)).ToList();
            foreach (var key in choiceKeysToRemove)
                choice.Remove(key);

            // Normalize finish_reason
            if (choice.TryGetValue("finish_reason", out var frEl) && frEl.ValueKind == JsonValueKind.String)
            {
                var fr = frEl.GetString() ?? "";
                if (!string.IsNullOrEmpty(fr) && !validFinishReasons.Contains(fr))
                {
                    choice["finish_reason"] = JsonSerializer.Deserialize<JsonElement>("\"stop\"");
                }
            }

            choices[i] = choice;
        }

        chunk["choices"] = JsonSerializer.Deserialize<JsonElement>(JsonSerializer.Serialize(choices));
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

        if (dataStr == "[DONE]")
            return ("", true, true);

        if (string.IsNullOrEmpty(dataStr))
            return ("", false, false);

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

    private static string EscapeForSSE(string s)
    {
        return s.Replace("\\", "\\\\").Replace("\"", "\\\"").Replace("\n", "\\n");
    }
}

/// <summary>Collected result from non-streaming upstream chunks.</summary>
public sealed class CollectedResult
{
    public string ID { get; set; } = "";
    public string Model { get; set; } = "";
    public string Content { get; set; } = "";
    public string ReasoningContent { get; set; } = "";
    public List<Dictionary<string, object>> ToolCalls { get; set; } = [];
    public string FinishReason { get; set; } = "";
    public int PromptTokens { get; set; }
    public int CompletionTokens { get; set; }
    public int ReasoningTokens { get; set; }
    public int CacheCreationTokens { get; set; }
}
