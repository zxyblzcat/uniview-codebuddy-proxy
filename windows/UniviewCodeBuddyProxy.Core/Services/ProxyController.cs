using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Text;
using System.Text.Json;
using System.Threading.Tasks;
using Microsoft.AspNetCore.Http;

namespace UniviewCodeBuddyProxy.Services;

/// <summary>
/// Main proxy route handlers — format conversion, probe detection,
/// minimum message guarantee, tool choice normalization.
/// Uses ASP.NET Core's HttpContext for request/response.
/// </summary>
public sealed class ProxyController
{
    // ═══ Dependencies ═══

    private readonly ConfigManager _configManager;
    private readonly TokenManager _tokenManager;
    private readonly UpstreamClient _upstreamClient;
    private readonly RetryHandler _retryHandler;
    private readonly CacheManager _cacheManager;
    private readonly CircuitBreaker _circuitBreaker;
    private readonly TelemetryReporter _telemetryReporter;
    private readonly LogBuffer _logBuffer;

    public ProxyController(
        ConfigManager configManager,
        TokenManager tokenManager,
        UpstreamClient upstreamClient,
        RetryHandler retryHandler,
        CacheManager cacheManager,
        CircuitBreaker circuitBreaker,
        TelemetryReporter telemetryReporter,
        LogBuffer logBuffer)
    {
        _configManager = configManager;
        _tokenManager = tokenManager;
        _upstreamClient = upstreamClient;
        _retryHandler = retryHandler;
        _cacheManager = cacheManager;
        _circuitBreaker = circuitBreaker;
        _telemetryReporter = telemetryReporter;
        _logBuffer = logBuffer;
    }

    // ═══ Image Detection & Model Switch ═══

    /// <summary>
    /// Detect images in the request body (OpenAI and Anthropic formats).
    /// </summary>
    private static bool DetectImages(Dictionary<string, object> body)
    {
        if (body.TryGetValue("messages", out var messagesObj) && messagesObj is JsonElement messagesEl)
        {
            if (messagesEl.ValueKind == JsonValueKind.Array)
            {
                foreach (var msg in messagesEl.EnumerateArray())
                {
                    if (msg.TryGetProperty("content", out var content) && HasImageInContent(content))
                        return true;
                }
            }
        }

        if (body.TryGetValue("system", out var system) && system is JsonElement systemEl)
        {
            if (HasImageInContent(systemEl))
                return true;
        }

        return false;
    }

    private static bool HasImageInContent(JsonElement content)
    {
        if (content.ValueKind != JsonValueKind.Array) return false;

        foreach (var item in content.EnumerateArray())
        {
            if (!item.TryGetProperty("type", out var typeEl)) continue;
            var type = typeEl.GetString() ?? "";

            if (type == "image_url") return true;

            if (type == "image")
            {
                if (item.TryGetProperty("source", out var source) &&
                    source.TryGetProperty("type", out var srcType))
                {
                    var srcTypeStr = srcType.GetString() ?? "";
                    if (srcTypeStr == "base64" || srcTypeStr == "url")
                        return true;
                }
            }

            if (type == "tool_result" && item.TryGetProperty("content", out var innerContent))
            {
                if (HasImageInContent(innerContent))
                    return true;
            }
        }

        return false;
    }

    /// <summary>
    /// When images are detected, switch the model to the configured vision model.
    /// </summary>
    private void AutoSwitchToVisionModelIfNeeded(Dictionary<string, object> payload)
    {
        if (_configManager.ImageAutoSwitchModel && DetectImages(payload))
        {
            var visionModel = _configManager.VisionModel;
            payload["model"] = visionModel;
        }
    }

    // ═══ Chat Completions (OpenAI) ═══

    public async Task HandleChatCompletionsAsync(HttpContext ctx)
    {
        var payload = await ParseBodyAsync(ctx);
        if (payload == null) { await WriteErrorAsync(ctx, 400, "missing body"); return; }

        if (!payload.ContainsKey("model")) payload["model"] = "auto-chat";

        AutoSwitchToVisionModelIfNeeded(payload);

        var clientRequestsStream = payload.TryGetValue("stream", out var streamVal)
            ? streamVal is bool b ? b : true
            : true;
        payload["stream"] = true;
        MergeStreamOptions(payload);
        EnsureMinMessages(payload);
        SanitizeToolChoice(payload);

        if (IsProbe(payload))
        {
            await WriteProbeResponseAsync(ctx, payload.GetValueOrDefault("model", "auto-chat")?.ToString() ?? "probe");
            return;
        }

        var model = payload.GetValueOrDefault("model", "auto-chat")?.ToString() ?? "auto-chat";

        var bearerToken = _tokenManager.NextToken()?.Bearer;
        if (string.IsNullOrEmpty(bearerToken))
        {
            await WriteErrorAsync(ctx, 503, "no available token");
            return;
        }

        var headers = _upstreamClient.BuildUpstreamHeaders(
            model, UpstreamIntent.Craft,
            _tokenManager.CurrentUserID ?? "",
            _upstreamClient.MachineId,
            bearerToken);

        _telemetryReporter.ReportChatRequest(model);
        var startTime = DateTime.UtcNow;

        if (clientRequestsStream)
        {
            ctx.Response.StatusCode = 200;
            ctx.Response.ContentType = "text/event-stream";
            ctx.Response.Headers.CacheControl = "no-cache";
            ctx.Response.Headers.Connection = "keep-alive";

            try
            {
                var (stream, response) = await _upstreamClient.DoUpstreamRequestAsync(payload, headers);

                if (!response.IsSuccessStatusCode)
                {
                    var errorBody = await ReadErrorBodyAsync(stream);
                    var sseError = StreamTranslator.SseError(errorBody);
                    await ctx.Response.WriteAsync(sseError);
                    await ctx.Response.Body.FlushAsync();
                    return;
                }

                var translator = new StreamTranslator(model);
                using var reader = new StreamReader(stream);
                string? line;
                while ((line = await reader.ReadLineAsync()) != null)
                {
                    var outputLines = translator.ProcessLine(line);
                    foreach (var output in outputLines)
                        await ctx.Response.WriteAsync(output);

                    if (line.Trim() == "data: [DONE]")
                        break;
                }

                await ctx.Response.Body.FlushAsync();
            }
            catch (Exception ex)
            {
                var sseError = StreamTranslator.SseError(ex.Message);
                try { await ctx.Response.WriteAsync(sseError); await ctx.Response.Body.FlushAsync(); }
                catch { /* client disconnected */ }
            }

            var latency = (DateTime.UtcNow - startTime).TotalSeconds;
            _telemetryReporter.ReportChatResponse(model, latency);
        }
        else
        {
            try
            {
                var result = await _retryHandler.ExecuteAsync(payload, headers, model,
                    (stream, _) => Task.FromResult(StreamTranslator.CollectChunks(stream, model)));

                var latency = (DateTime.UtcNow - startTime).TotalSeconds;
                _telemetryReporter.ReportChatResponse(model, latency);

                var responseDict = BuildNonStreamingResponse(result);
                await WriteJsonAsync(ctx, 200, responseDict);
            }
            catch (Exception ex)
            {
                await WriteErrorAsync(ctx, 502, ex.Message);
            }
        }
    }

    // ═══ Anthropic Messages ═══

    public async Task HandleAnthropicMessagesAsync(HttpContext ctx)
    {
        var payload = await ParseBodyAsync(ctx);
        if (payload == null) { await WriteErrorAsync(ctx, 400, "missing body"); return; }

        if (!payload.ContainsKey("model")) payload["model"] = "deepseek-v3";

        AutoSwitchToVisionModelIfNeeded(payload);

        var openaiPayload = ConvertAnthropicToOpenAI(payload);

        if (IsProbe(openaiPayload))
        {
            await WriteAnthropicProbeResponseAsync(ctx);
            return;
        }

        var model = openaiPayload.GetValueOrDefault("model", "deepseek-v3")?.ToString() ?? "deepseek-v3";
        var isStream = payload.TryGetValue("stream", out var streamVal) && streamVal is bool b && b;

        var bearerToken = _tokenManager.NextToken()?.Bearer;
        if (string.IsNullOrEmpty(bearerToken))
        {
            await WriteAnthropicErrorAsync(ctx, "authentication_error", "no available token");
            return;
        }

        var headers = _upstreamClient.BuildUpstreamHeaders(
            model, UpstreamIntent.Craft,
            _tokenManager.CurrentUserID ?? "",
            _upstreamClient.MachineId,
            bearerToken);

        _telemetryReporter.ReportChatRequest(model);
        var startTime = DateTime.UtcNow;

        if (isStream)
        {
            ctx.Response.StatusCode = 200;
            ctx.Response.ContentType = "text/event-stream";
            ctx.Response.Headers.CacheControl = "no-cache";
            ctx.Response.Headers.Connection = "keep-alive";

            try
            {
                var (stream, response) = await _upstreamClient.DoUpstreamRequestAsync(openaiPayload, headers);
                if (!response.IsSuccessStatusCode)
                {
                    var errorBody = await ReadErrorBodyAsync(stream);
                    var sseError = AnthropicStreamTranslator.SseError(errorBody);
                    await ctx.Response.WriteAsync(sseError);
                    await ctx.Response.Body.FlushAsync();
                    return;
                }

                var translator = new AnthropicStreamTranslator(model);
                using var reader = new StreamReader(stream);
                string? line;
                while ((line = await reader.ReadLineAsync()) != null)
                {
                    var outputLines = translator.ProcessLine(line);
                    foreach (var output in outputLines)
                        await ctx.Response.WriteAsync(output);
                    if (line.Trim() == "data: [DONE]") break;
                }
                var finalEvents = translator.Close();
                foreach (var output in finalEvents)
                    await ctx.Response.WriteAsync(output);

                await ctx.Response.Body.FlushAsync();
            }
            catch (Exception ex)
            {
                var sseError = AnthropicStreamTranslator.SseError(ex.Message);
                try { await ctx.Response.WriteAsync(sseError); await ctx.Response.Body.FlushAsync(); }
                catch { /* client disconnected */ }
            }

            var latency = (DateTime.UtcNow - startTime).TotalSeconds;
            _telemetryReporter.ReportChatResponse(model, latency);
        }
        else
        {
            try
            {
                var result = await _retryHandler.ExecuteAsync(openaiPayload, headers, model,
                    (stream, _) => Task.FromResult(StreamTranslator.CollectChunks(stream, model)));

                var latency = (DateTime.UtcNow - startTime).TotalSeconds;
                _telemetryReporter.ReportChatResponse(model, latency);

                var anthropicResponse = ConvertOpenAIToAnthropic(result, payload);
                await WriteJsonAsync(ctx, 200, anthropicResponse);
            }
            catch (Exception ex)
            {
                await WriteAnthropicErrorAsync(ctx, "api_error", ex.Message);
            }
        }
    }

    // ═══ Responses API ═══

    public async Task HandleResponsesAsync(HttpContext ctx)
    {
        var payload = await ParseBodyAsync(ctx);
        if (payload == null) { await WriteErrorAsync(ctx, 400, "missing body"); return; }

        AutoSwitchToVisionModelIfNeeded(payload);

        var openaiPayload = ConvertResponsesToOpenAI(payload);
        var model = openaiPayload.GetValueOrDefault("model", "auto-chat")?.ToString() ?? "auto-chat";
        var isStream = payload.TryGetValue("stream", out var streamVal) && streamVal is bool b && b;

        var bearerToken = _tokenManager.NextToken()?.Bearer;
        if (string.IsNullOrEmpty(bearerToken))
        {
            await WriteErrorAsync(ctx, 503, "no available token");
            return;
        }

        var headers = _upstreamClient.BuildUpstreamHeaders(
            model, UpstreamIntent.Craft,
            _tokenManager.CurrentUserID ?? "",
            _upstreamClient.MachineId,
            bearerToken);

        _telemetryReporter.ReportResponsesRequest(model);

        if (isStream)
        {
            ctx.Response.StatusCode = 200;
            ctx.Response.ContentType = "text/event-stream";
            ctx.Response.Headers.CacheControl = "no-cache";

            try
            {
                var (stream, response) = await _upstreamClient.DoUpstreamRequestAsync(openaiPayload, headers);
                if (!response.IsSuccessStatusCode)
                {
                    var sseError = ResponsesStreamTranslator.SseError("upstream error");
                    await ctx.Response.WriteAsync(sseError);
                    await ctx.Response.Body.FlushAsync();
                    return;
                }

                var translator = new ResponsesStreamTranslator(model);
                using var reader = new StreamReader(stream);
                string? line;
                while ((line = await reader.ReadLineAsync()) != null)
                {
                    var outputLines = translator.ProcessLine(line);
                    foreach (var output in outputLines)
                        await ctx.Response.WriteAsync(output);
                    if (line.Trim() == "data: [DONE]") break;
                }
                var finalEvents = translator.Close();
                foreach (var output in finalEvents)
                    await ctx.Response.WriteAsync(output);

                await ctx.Response.Body.FlushAsync();
            }
            catch
            {
                // client disconnected or upstream error
            }
        }
        else
        {
            try
            {
                var result = await _retryHandler.ExecuteAsync(openaiPayload, headers, model,
                    (stream, _) => Task.FromResult(StreamTranslator.CollectChunks(stream, model)));

                var responsesResult = ConvertOpenAIToResponses(result, payload);
                await WriteJsonAsync(ctx, 200, responsesResult);
            }
            catch (Exception ex)
            {
                await WriteErrorAsync(ctx, 502, ex.Message);
            }
        }
    }

    public async Task HandleResponsesCompactAsync(HttpContext ctx)
    {
        var payload = await ParseBodyAsync(ctx);
        if (payload == null) { await WriteErrorAsync(ctx, 400, "missing body"); return; }

        var openaiPayload = ConvertResponsesToOpenAI(payload);
        var compactInstruction = "You are a context compaction assistant. Summarize the conversation so far, preserving all important context, decisions, and code references. Keep the summary concise but complete.";

        if (openaiPayload.TryGetValue("messages", out var msgsObj) && msgsObj is List<Dictionary<string, object>> messages)
        {
            messages.Insert(0, new Dictionary<string, object>
            {
                ["role"] = "system",
                ["content"] = compactInstruction,
            });
            openaiPayload["messages"] = messages;
        }
        openaiPayload["max_tokens"] = 4096;

        var model = openaiPayload.GetValueOrDefault("model", "auto-chat")?.ToString() ?? "auto-chat";
        var bearerToken = _tokenManager.NextToken()?.Bearer;
        if (string.IsNullOrEmpty(bearerToken))
        {
            await WriteErrorAsync(ctx, 503, "no available token");
            return;
        }

        var headers = _upstreamClient.BuildUpstreamHeaders(
            model, UpstreamIntent.Craft,
            _tokenManager.CurrentUserID ?? "",
            _upstreamClient.MachineId,
            bearerToken);

        try
        {
            var result = await _retryHandler.ExecuteAsync(openaiPayload, headers, model,
                (stream, _) => Task.FromResult(StreamTranslator.CollectChunks(stream, model)));

            var responsesResult = ConvertOpenAIToResponses(result, payload);
            await WriteJsonAsync(ctx, 200, responsesResult);
        }
        catch (Exception ex)
        {
            await WriteErrorAsync(ctx, 502, ex.Message);
        }
    }

    // ═══ Models ═══

    public async Task HandleModelsAsync(HttpContext ctx)
    {
        var models = Constants.ExtraModels
            .Select(m => new Dictionary<string, string>
            {
                ["id"] = m.Name,
                ["object"] = "model",
                ["owned_by"] = m.OwnedBy,
            })
            .ToList();

        // Try fetching from upstream
        try
        {
            var upstreamModels = await _upstreamClient.FetchModelsAsync();
            foreach (var m in upstreamModels)
            {
                if (!models.Exists(e => e["id"] == m["id"]))
                    models.Add(m);
            }
        }
        catch { /* use hardcoded only */ }

        await WriteJsonAsync(ctx, 200, new Dictionary<string, object>
        {
            ["object"] = "list",
            ["data"] = models,
        });
    }

    public async Task HandleModelByIDAsync(HttpContext ctx, string id)
    {
        var contextWindow = Constants.GetModelContextWindow(id);
        var maxOutput = Math.Max(Math.Min(contextWindow / 4, contextWindow - 1), 8192);
        var response = new Dictionary<string, object>
        {
            ["id"] = id,
            ["object"] = "model",
            ["owned_by"] = Constants.InferOwnedBy(id),
            ["max_input_tokens"] = contextWindow,
            ["max_output_tokens"] = maxOutput,
            ["capabilities"] = new Dictionary<string, object>
            {
                ["context_management"] = new Dictionary<string, object> { ["supports_compact"] = true },
                ["effort"] = new Dictionary<string, object> { ["supported_values"] = new[] { "high", "low", "max", "medium" } },
                ["thinking"] = new Dictionary<string, object> { ["is_supported"] = false },
                ["image_input"] = new Dictionary<string, object> { ["is_supported"] = false },
                ["pdf_input"] = new Dictionary<string, object> { ["is_supported"] = false },
                ["structured_outputs"] = new Dictionary<string, object> { ["is_supported"] = false },
            },
        };
        await WriteJsonAsync(ctx, 200, response);
    }

    // ═══ Completions ═══

    public async Task HandleCompletionsAsync(HttpContext ctx)
    {
        var payload = await ParseBodyAsync(ctx);
        if (payload == null) { await WriteErrorAsync(ctx, 400, "missing body"); return; }

        payload["stream"] = true;
        MergeStreamOptions(payload);
        EnsureMinMessages(payload);

        var bearerToken = _tokenManager.NextToken()?.Bearer;
        if (string.IsNullOrEmpty(bearerToken))
        {
            await WriteErrorAsync(ctx, 503, "no available token");
            return;
        }

        var model = payload.GetValueOrDefault("model", "auto-chat")?.ToString() ?? "auto-chat";
        var headers = _upstreamClient.BuildUpstreamHeaders(
            model, UpstreamIntent.CodeCompletion,
            _tokenManager.CurrentUserID ?? "",
            _upstreamClient.MachineId,
            bearerToken);

        try
        {
            var (text, respModel, finishReason) = await _retryHandler.ExecuteAsync(payload, headers, model,
                async (stream, _) =>
                {
                    var t = "";
                    var m = "";
                    var fr = "";
                    using var reader = new StreamReader(stream);
                    string? line;
                    while ((line = await reader.ReadLineAsync()) != null)
                    {
                        var trimmed = line.Trim();
                        if (!trimmed.StartsWith("data: ") || trimmed == "data: [DONE]") continue;
                        var jsonStr = trimmed[6..];
                        try
                        {
                            using var doc = JsonDocument.Parse(jsonStr);
                            var root = doc.RootElement;
                            if (root.TryGetProperty("choices", out var choices) && choices.GetArrayLength() > 0)
                            {
                                var first = choices[0];
                                if (first.TryGetProperty("text", out var textEl))
                                    t += textEl.GetString() ?? "";
                                m = root.TryGetProperty("model", out var modelEl) ? modelEl.GetString() ?? m : m;
                                fr = first.TryGetProperty("finish_reason", out var frEl) ? frEl.GetString() ?? fr : fr;
                            }
                        }
                        catch { /* skip */ }
                    }
                    return (t, m, fr);
                });

            var response = new Dictionary<string, object>
            {
                ["id"] = $"cmpl-{Guid.NewGuid():N}"[..20],
                ["object"] = "text_completion",
                ["created"] = (int)DateTimeOffset.UtcNow.ToUnixTimeSeconds(),
                ["model"] = respModel,
                ["choices"] = new[]
                {
                    new Dictionary<string, object>
                    {
                        ["text"] = text,
                        ["index"] = 0,
                        ["finish_reason"] = finishReason,
                    },
                },
            };
            await WriteJsonAsync(ctx, 200, response);
        }
        catch (Exception ex)
        {
            await WriteErrorAsync(ctx, 502, ex.Message);
        }
    }

    // ═══ Embeddings ═══

    public async Task HandleEmbeddingsAsync(HttpContext ctx)
    {
        var payload = await ParseBodyAsync(ctx);
        if (payload == null) { await WriteErrorAsync(ctx, 400, "missing body"); return; }

        var bearerToken = _tokenManager.NextToken()?.Bearer;
        if (string.IsNullOrEmpty(bearerToken))
        {
            await WriteErrorAsync(ctx, 503, "no available token");
            return;
        }

        var model = payload.GetValueOrDefault("model", "codebuddy-embed")?.ToString() ?? "codebuddy-embed";
        var headers = _upstreamClient.BuildUpstreamHeaders(
            model, UpstreamIntent.Embedding,
            _tokenManager.CurrentUserID ?? "",
            _upstreamClient.MachineId,
            bearerToken);

        try
        {
            var (stream, response) = await _upstreamClient.DoUpstreamRequestAsync(payload, headers);
            using var reader = new StreamReader(stream);
            var body = await reader.ReadToEndAsync();
            ctx.Response.StatusCode = 200;
            ctx.Response.ContentType = "application/json";
            await ctx.Response.WriteAsync(body);
        }
        catch (Exception ex)
        {
            await WriteErrorAsync(ctx, 502, ex.Message);
        }
    }

    // ═══ Count Tokens ═══

    public async Task HandleCountTokensAsync(HttpContext ctx)
    {
        var payload = await ParseBodyAsync(ctx);
        if (payload == null) { await WriteErrorAsync(ctx, 400, "missing body"); return; }

        var messages = payload.GetValueOrDefault("messages") as List<Dictionary<string, object>> ?? [];
        var totalChars = messages
            .Where(m => m.ContainsKey("content"))
            .Sum(m => m["content"]?.ToString()?.Length ?? 0);
        var estimatedTokens = totalChars / 4;

        await WriteJsonAsync(ctx, 200, new Dictionary<string, int> { ["input_tokens"] = estimatedTokens });
    }

    // ═══ Service Info ═══

    public async Task HandleServiceInfoAsync(HttpContext ctx)
    {
        var info = new Dictionary<string, object>
        {
            ["name"] = "CodeBuddy Proxy",
            ["version"] = "0.1.0",
            ["port"] = _configManager.Port,
        };
        await WriteJsonAsync(ctx, 200, info);
    }

    // ═══ Helper Methods ═══

    private static void MergeStreamOptions(Dictionary<string, object> payload)
    {
        if (payload.TryGetValue("stream_options", out var soObj) && soObj is Dictionary<string, object> so)
            so["include_usage"] = true;
        else
            payload["stream_options"] = new Dictionary<string, object> { ["include_usage"] = true };
    }

    private static void EnsureMinMessages(Dictionary<string, object> payload)
    {
        if (!payload.TryGetValue("messages", out var msgsObj)) return;
        if (msgsObj is List<Dictionary<string, object>> messages && messages.Count < 2)
        {
            messages.Insert(0, new Dictionary<string, object>
            {
                ["role"] = "system",
                ["content"] = "You are a helpful assistant.",
            });
            payload["messages"] = messages;
        }
    }

    private static void SanitizeToolChoice(Dictionary<string, object> payload)
    {
        if (!payload.ContainsKey("tool_choice")) return;
        var tc = payload["tool_choice"];
        if (tc is string) return;
        if (tc is Dictionary<string, object> dict)
        {
            if (dict.TryGetValue("type", out var type) && (type?.ToString() == "function" || type?.ToString() == "tool"))
                payload["tool_choice"] = "required";
        }
    }

    private static bool IsProbe(Dictionary<string, object> payload)
    {
        if (payload.TryGetValue("max_tokens", out var mt) && mt is int maxTokens && maxTokens == 1)
        {
            if (payload.TryGetValue("stream", out var s) && s is bool stream && stream)
                return true;
        }
        return false;
    }

    // ═══ Probe Responses ═══

    private static async Task WriteProbeResponseAsync(HttpContext ctx, string model)
    {
        var response = new Dictionary<string, object>
        {
            ["id"] = "chatcmpl-probe",
            ["object"] = "chat.completion",
            ["created"] = 0,
            ["model"] = model,
            ["choices"] = new[]
            {
                new Dictionary<string, object>
                {
                    ["index"] = 0,
                    ["message"] = new Dictionary<string, string> { ["role"] = "assistant", ["content"] = "ok" },
                    ["finish_reason"] = "stop",
                },
            },
            ["usage"] = new Dictionary<string, int> { ["prompt_tokens"] = 0, ["completion_tokens"] = 0, ["total_tokens"] = 0 },
        };
        await WriteJsonAsync(ctx, 200, response);
    }

    private static async Task WriteAnthropicProbeResponseAsync(HttpContext ctx)
    {
        var events = new[]
        {
            "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_probe\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"probe\",\"stop_reason\":null,\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}}\n\n",
            "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
            "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n",
            "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
            "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n",
            "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
        };
        var body = string.Join("", events);
        ctx.Response.StatusCode = 200;
        ctx.Response.ContentType = "text/event-stream";
        ctx.Response.Headers.CacheControl = "no-cache";
        await ctx.Response.WriteAsync(body);
        await ctx.Response.Body.FlushAsync();
    }

    // ═══ Non-Streaming Response Builder ═══

    private static Dictionary<string, object> BuildNonStreamingResponse(CollectedResult result)
    {
        var responseDict = new Dictionary<string, object>
        {
            ["id"] = result.ID,
            ["object"] = "chat.completion",
            ["created"] = (int)DateTimeOffset.UtcNow.ToUnixTimeSeconds(),
            ["model"] = result.Model,
            ["choices"] = new[]
            {
                new Dictionary<string, object>
                {
                    ["index"] = 0,
                    ["message"] = new Dictionary<string, object>
                    {
                        ["role"] = "assistant",
                        ["content"] = result.Content,
                    },
                    ["finish_reason"] = result.FinishReason,
                },
            },
            ["usage"] = new Dictionary<string, int>
            {
                ["prompt_tokens"] = result.PromptTokens,
                ["completion_tokens"] = result.CompletionTokens,
                ["total_tokens"] = result.TotalTokens > 0 ? result.TotalTokens : result.PromptTokens + result.CompletionTokens,
            },
        };

        if (result.ReasoningTokens > 0)
            responseDict["completion_tokens_details"] = new Dictionary<string, int> { ["reasoning_tokens"] = result.ReasoningTokens };

        if (result.CacheReadInputTokens > 0 && responseDict["usage"] is Dictionary<string, int> usage)
            usage["prompt_tokens_details"] = new Dictionary<string, int> { ["cached_tokens"] = result.CacheReadInputTokens };

        if (result.ToolCalls.Count > 0)
        {
            var choices = (Dictionary<string, object>[])responseDict["choices"];
            var msg = (Dictionary<string, object>)choices[0]["message"];
            msg["tool_calls"] = result.ToolCalls;
        }

        return responseDict;
    }

    // ═══ Format Conversion: Anthropic -> OpenAI ═══

    private static Dictionary<string, object> ConvertAnthropicToOpenAI(Dictionary<string, object> payload)
    {
        var result = new Dictionary<string, object>
        {
            ["model"] = payload.GetValueOrDefault("model", "deepseek-v3") ?? "deepseek-v3",
            ["stream"] = true,
            ["stream_options"] = new Dictionary<string, object> { ["include_usage"] = true },
        };

        result["max_tokens"] = payload.TryGetValue("max_tokens", out var mt) && mt is int maxT ? maxT : 4096;
        if (payload.TryGetValue("temperature", out var temp) && temp is double d)
            result["temperature"] = d;

        if (payload.TryGetValue("messages", out var messages))
            result["messages"] = ConvertAnthropicMessagesToOpenAI(messages);

        if (payload.TryGetValue("tools", out var tools))
            result["tools"] = ConvertAnthropicToolsToOpenAI(tools);

        if (payload.TryGetValue("tool_choice", out var tc))
            result["tool_choice"] = ConvertAnthropicToolChoiceToOpenAI(tc);

        if (payload.TryGetValue("stop_sequences", out var stop) && stop is List<string> stopList)
            result["stop"] = stopList;

        EnsureMinMessages(result);
        return result;
    }

    private static List<Dictionary<string, object>> ConvertAnthropicMessagesToOpenAI(object? messagesObj)
    {
        var result = new List<Dictionary<string, object>>();
        if (messagesObj is not JsonElement messagesEl || messagesEl.ValueKind != JsonValueKind.Array)
            return result;

        foreach (var msg in messagesEl.EnumerateArray())
        {
            var role = msg.TryGetProperty("role", out var r) ? r.GetString() ?? "user" : "user";
            if (!msg.TryGetProperty("content", out var content)) continue;

            if (content.ValueKind == JsonValueKind.String)
            {
                result.Add(new Dictionary<string, object> { ["role"] = role, ["content"] = content.GetString() ?? "" });
            }
            else if (content.ValueKind == JsonValueKind.Array)
            {
                var parts = new List<Dictionary<string, object>>();
                var textContent = new StringBuilder();

                foreach (var block in content.EnumerateArray())
                {
                    var type = block.TryGetProperty("type", out var t) ? t.GetString() ?? "" : "";
                    switch (type)
                    {
                        case "text":
                            var text = block.TryGetProperty("text", out var te) ? te.GetString() ?? "" : "";
                            parts.Add(new Dictionary<string, object> { ["type"] = "text", ["text"] = text });
                            textContent.Append(text);
                            break;

                        case "image":
                            if (block.TryGetProperty("source", out var source))
                            {
                                var srcType = source.TryGetProperty("type", out var st) ? st.GetString() ?? "" : "";
                                if (srcType == "base64" &&
                                    source.TryGetProperty("data", out var dataEl) &&
                                    source.TryGetProperty("media_type", out var mtEl))
                                {
                                    var url = $"data:{mtEl.GetString() ?? "image/png"};base64,{dataEl.GetString() ?? ""}";
                                    parts.Add(new Dictionary<string, object>
                                    {
                                        ["type"] = "image_url",
                                        ["image_url"] = new Dictionary<string, string> { ["url"] = url },
                                    });
                                }
                                else if (srcType == "url" && source.TryGetProperty("url", out var urlEl))
                                {
                                    parts.Add(new Dictionary<string, object>
                                    {
                                        ["type"] = "image_url",
                                        ["image_url"] = new Dictionary<string, string> { ["url"] = urlEl.GetString() ?? "" },
                                    });
                                }
                            }
                            break;

                        case "tool_use":
                            if (block.TryGetProperty("id", out var idEl) &&
                                block.TryGetProperty("name", out var nameEl) &&
                                block.TryGetProperty("input", out var inputEl))
                            {
                                result.Add(new Dictionary<string, object>
                                {
                                    ["role"] = "assistant",
                                    ["tool_calls"] = new[]
                                    {
                                        new Dictionary<string, object>
                                        {
                                            ["id"] = idEl.GetString() ?? "",
                                            ["type"] = "function",
                                            ["function"] = new Dictionary<string, string>
                                            {
                                                ["name"] = nameEl.GetString() ?? "",
                                                ["arguments"] = inputEl.GetRawText(),
                                            },
                                        },
                                    },
                                });
                            }
                            break;

                        case "tool_result":
                            if (block.TryGetProperty("tool_use_id", out var tuiEl))
                            {
                                object toolContent = "";
                                if (block.TryGetProperty("content", out var tcEl))
                                {
                                    if (tcEl.ValueKind == JsonValueKind.String)
                                        toolContent = tcEl.GetString() ?? "";
                                    else if (tcEl.ValueKind == JsonValueKind.Array)
                                        toolContent = string.Join("", tcEl.EnumerateArray()
                                            .Where(b => b.TryGetProperty("text", out _))
                                            .Select(b => b.GetProperty("text").GetString() ?? ""));
                                }
                                result.Add(new Dictionary<string, object>
                                {
                                    ["role"] = "tool",
                                    ["tool_call_id"] = tuiEl.GetString() ?? "",
                                    ["content"] = toolContent,
                                });
                            }
                            break;
                    }
                }

                if (parts.Count > 0)
                {
                    if (parts.Count == 1 && parts[0].GetValueOrDefault("type", "")?.ToString() == "text")
                        result.Add(new Dictionary<string, object> { ["role"] = role, ["content"] = textContent.ToString() });
                    else
                        result.Add(new Dictionary<string, object> { ["role"] = role, ["content"] = parts });
                }
            }
        }

        return result;
    }

    private static List<Dictionary<string, object>> ConvertAnthropicToolsToOpenAI(object? toolsObj)
    {
        var result = new List<Dictionary<string, object>>();
        if (toolsObj is not JsonElement toolsEl || toolsEl.ValueKind != JsonValueKind.Array)
            return result;

        foreach (var tool in toolsEl.EnumerateArray())
        {
            var name = tool.TryGetProperty("name", out var n) ? n.GetString() : null;
            if (name == null) continue;

            var desc = tool.TryGetProperty("description", out var d) ? d.GetString() ?? "" : "";
            var parameters = tool.TryGetProperty("input_schema", out var p) ? p.GetRawText() : "{}";

            result.Add(new Dictionary<string, object>
            {
                ["type"] = "function",
                ["function"] = new Dictionary<string, string>
                {
                    ["name"] = name,
                    ["description"] = desc,
                    ["parameters"] = parameters,
                },
            });
        }
        return result;
    }

    private static object ConvertAnthropicToolChoiceToOpenAI(object tc)
    {
        if (tc is string s)
            return s switch { "auto" => "auto", "any" => "required", "none" => "none", _ => "auto" };
        if (tc is JsonElement el && el.ValueKind == JsonValueKind.Object &&
            el.TryGetProperty("type", out var typeEl) && typeEl.GetString() == "tool")
            return "required";
        return "auto";
    }

    // ═══ Format Conversion: OpenAI -> Anthropic ═══

    private static Dictionary<string, object> ConvertOpenAIToAnthropic(CollectedResult result, Dictionary<string, object> originalPayload)
    {
        var content = new List<Dictionary<string, object>>();
        if (!string.IsNullOrEmpty(result.Content))
            content.Add(new Dictionary<string, object> { ["type"] = "text", ["text"] = result.Content });

        foreach (var tc in result.ToolCalls)
        {
            if (tc.TryGetValue("id", out var id) && tc.TryGetValue("function", out var fn) &&
                fn is Dictionary<string, object> fnDict && fnDict.TryGetValue("name", out var name))
            {
                content.Add(new Dictionary<string, object>
                {
                    ["type"] = "tool_use",
                    ["id"] = id?.ToString() ?? "",
                    ["name"] = name?.ToString() ?? "",
                    ["input"] = fnDict.GetValueOrDefault("arguments", "{}")?.ToString() ?? "{}",
                });
            }
        }

        var stopReason = result.FinishReason switch
        {
            "stop" => "end_turn",
            "tool_calls" => "tool_use",
            "length" => "max_tokens",
            _ => "end_turn",
        };

        return new Dictionary<string, object>
        {
            ["id"] = $"msg_{Guid.NewGuid():N}"[..28],
            ["type"] = "message",
            ["role"] = "assistant",
            ["content"] = content,
            ["model"] = result.Model,
            ["stop_reason"] = stopReason,
            ["stop_sequence"] = (string?)null,
            ["usage"] = new Dictionary<string, int>
            {
                ["input_tokens"] = result.PromptTokens,
                ["output_tokens"] = result.CompletionTokens,
                ["cache_creation_input_tokens"] = result.CacheCreationInputTokens,
                ["cache_read_input_tokens"] = result.CacheReadInputTokens,
            },
        };
    }

    // ═══ Format Conversion: Responses -> OpenAI ═══

    private static Dictionary<string, object> ConvertResponsesToOpenAI(Dictionary<string, object> payload)
    {
        var result = new Dictionary<string, object>
        {
            ["model"] = payload.GetValueOrDefault("model", "auto-chat") ?? "auto-chat",
            ["stream"] = true,
            ["stream_options"] = new Dictionary<string, object> { ["include_usage"] = true },
        };

        result["max_tokens"] = payload.TryGetValue("max_output_tokens", out var mt) && mt is int maxT ? maxT : 4096;
        if (payload.TryGetValue("temperature", out var temp) && temp is double d)
            result["temperature"] = d;

        if (payload.TryGetValue("input", out var input))
            result["messages"] = ConvertResponsesInputToMessages(input);

        if (payload.TryGetValue("instructions", out var instr) && instr is string instructions && !string.IsNullOrEmpty(instructions))
        {
            if (result.TryGetValue("messages", out var msgsObj) && msgsObj is List<Dictionary<string, object>> messages)
            {
                messages.Insert(0, new Dictionary<string, object> { ["role"] = "system", ["content"] = instructions });
                result["messages"] = messages;
            }
        }

        if (payload.TryGetValue("tools", out var tools))
            result["tools"] = ConvertResponsesToolsToOpenAI(tools);

        if (payload.TryGetValue("reasoning", out var reasoning) && reasoning is Dictionary<string, object> reasoningDict)
        {
            if (reasoningDict.TryGetValue("effort", out var effort) && effort is string effortStr)
            {
                result["temperature"] = effortStr switch
                {
                    "high" => 0.7,
                    "low" => 0.3,
                    "medium" => 0.5,
                    _ => result.GetValueOrDefault("temperature", 0.5),
                };
            }
        }

        EnsureMinMessages(result);
        return result;
    }

    private static List<Dictionary<string, object>> ConvertResponsesInputToMessages(object input)
    {
        var messages = new List<Dictionary<string, object>>();
        if (input is string s)
        {
            messages.Add(new Dictionary<string, object> { ["role"] = "user", ["content"] = s });
        }
        else if (input is JsonElement el && el.ValueKind == JsonValueKind.Array)
        {
            foreach (var item in el.EnumerateArray())
            {
                if (item.ValueKind == JsonValueKind.String)
                    messages.Add(new Dictionary<string, object> { ["role"] = "user", ["content"] = item.GetString() ?? "" });
                else if (item.ValueKind == JsonValueKind.Object)
                {
                    var role = item.TryGetProperty("role", out var r) ? r.GetString() ?? "user" : "user";
                    if (item.TryGetProperty("content", out var content))
                        messages.Add(new Dictionary<string, object> { ["role"] = role, ["content"] = JsonSerializer.Deserialize<object>(content.GetRawText()) ?? "" });
                }
            }
        }
        return messages;
    }

    private static List<Dictionary<string, object>> ConvertResponsesToolsToOpenAI(object tools)
    {
        var result = new List<Dictionary<string, object>>();
        if (tools is not JsonElement toolsEl || toolsEl.ValueKind != JsonValueKind.Array)
            return result;

        foreach (var tool in toolsEl.EnumerateArray())
        {
            var type = tool.TryGetProperty("type", out var t) ? t.GetString() ?? "" : "";
            if (type is "function" or "function_call")
            {
                var name = tool.TryGetProperty("name", out var n) ? n.GetString() : null;
                if (name == null) continue;
                var desc = tool.TryGetProperty("description", out var d) ? d.GetString() ?? "" : "";
                var parameters = tool.TryGetProperty("parameters", out var p) ? p.GetRawText()
                    : tool.TryGetProperty("input_schema", out var isEl) ? isEl.GetRawText() : "{}";

                result.Add(new Dictionary<string, object>
                {
                    ["type"] = "function",
                    ["function"] = new Dictionary<string, string>
                    {
                        ["name"] = name,
                        ["description"] = desc,
                        ["parameters"] = parameters,
                    },
                });
            }
        }
        return result;
    }

    // ═══ Format Conversion: OpenAI -> Responses ═══

    private static Dictionary<string, object> ConvertOpenAIToResponses(CollectedResult result, Dictionary<string, object> originalPayload)
    {
        var output = new List<Dictionary<string, object>>();

        if (!string.IsNullOrEmpty(result.Content))
        {
            output.Add(new Dictionary<string, object>
            {
                ["type"] = "message",
                ["id"] = $"msg_{Guid.NewGuid():N}"[..28],
                ["role"] = "assistant",
                ["content"] = new[]
                {
                    new Dictionary<string, object> { ["type"] = "output_text", ["text"] = result.Content },
                },
            });
        }

        foreach (var tc in result.ToolCalls)
        {
            if (tc.TryGetValue("id", out var id) && tc.TryGetValue("function", out var fn) &&
                fn is Dictionary<string, object> fnDict && fnDict.TryGetValue("name", out var name))
            {
                output.Add(new Dictionary<string, object>
                {
                    ["type"] = "function_call",
                    ["id"] = id?.ToString() ?? "",
                    ["call_id"] = id?.ToString() ?? "",
                    ["name"] = name?.ToString() ?? "",
                    ["arguments"] = fnDict.GetValueOrDefault("arguments", "{}")?.ToString() ?? "{}",
                });
            }
        }

        var stopReason = result.FinishReason switch
        {
            "stop" => "completed",
            "tool_calls" => "completed",
            "length" => "incomplete",
            _ => "completed",
        };

        return new Dictionary<string, object>
        {
            ["id"] = $"resp_{Guid.NewGuid():N}"[..28],
            ["object"] = "response",
            ["created_at"] = (int)DateTimeOffset.UtcNow.ToUnixTimeSeconds(),
            ["status"] = stopReason,
            ["model"] = result.Model,
            ["output"] = output,
            ["usage"] = new Dictionary<string, int>
            {
                ["input_tokens"] = result.PromptTokens,
                ["output_tokens"] = result.CompletionTokens,
                ["total_tokens"] = result.TotalTokens > 0 ? result.TotalTokens : result.PromptTokens + result.CompletionTokens,
            },
        };
    }

    // ═══ Utility Methods ═══

    private static async Task<Dictionary<string, object>?> ParseBodyAsync(HttpContext ctx)
    {
        try
        {
            using var reader = new StreamReader(ctx.Request.Body);
            var body = await reader.ReadToEndAsync();
            if (string.IsNullOrEmpty(body)) return null;

            var result = JsonSerializer.Deserialize<Dictionary<string, object>>(body, new JsonSerializerOptions
            {
                PropertyNameCaseInsensitive = true,
            });
            return result;
        }
        catch
        {
            return null;
        }
    }

    private static async Task WriteJsonAsync(HttpContext ctx, int statusCode, object data)
    {
        ctx.Response.StatusCode = statusCode;
        ctx.Response.ContentType = "application/json";
        var json = JsonSerializer.Serialize(data);
        await ctx.Response.WriteAsync(json);
    }

    private static async Task WriteErrorAsync(HttpContext ctx, int statusCode, string message)
    {
        var error = new Dictionary<string, object>
        {
            ["error"] = new Dictionary<string, string>
            {
                ["message"] = message,
                ["type"] = "invalid_request_error",
                ["code"] = "proxy_error",
            },
        };
        await WriteJsonAsync(ctx, statusCode, error);
    }

    private static async Task WriteAnthropicErrorAsync(HttpContext ctx, string type, string message)
    {
        var error = new Dictionary<string, object>
        {
            ["type"] = "error",
            ["error"] = new Dictionary<string, string> { ["type"] = type, ["message"] = message },
        };
        await WriteJsonAsync(ctx, 400, error);
    }

    private static async Task<string> ReadErrorBodyAsync(Stream stream)
    {
        try
        {
            using var reader = new StreamReader(stream);
            var body = await reader.ReadToEndAsync();
            if (body.Length > 500) body = body[..500];
            // Strip HTML tags
            var result = new StringBuilder();
            var inTag = false;
            foreach (var c in body)
            {
                if (c == '<') inTag = true;
                else if (c == '>') { inTag = false; continue; }
                if (!inTag) result.Append(c);
            }
            return result.ToString().Trim();
        }
        catch
        {
            return "";
        }
    }

    /// <summary>
    /// Check if an error message matches context limit patterns.
    /// </summary>
    public static bool IsContextLimitError(string message)
    {
        if (string.IsNullOrEmpty(message)) return false;
        var lower = message.ToLowerInvariant();
        return Constants.ContextLimitPatterns.Any(p => lower.Contains(p));
    }
}
