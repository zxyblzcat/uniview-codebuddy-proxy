using System;
using System.Collections.Generic;
using System.IO;
using System.Net;
using System.Net.Http;
using System.Net.Http.Headers;
using System.Text;
using System.Text.Json;
using System.Threading.Tasks;

namespace UniviewCodeBuddyProxy.Services;

/// <summary>Upstream request intent type.</summary>
public enum UpstreamIntent
{
    Craft,
    CodeCompletion,
    Embedding,
}

/// <summary>Upstream request error.</summary>
public sealed class UpstreamError : Exception
{
    public int StatusCode { get; }
    public double RetryAfter { get; }

    public UpstreamError(int statusCode, string message, double retryAfter = 0)
        : base($"upstream {statusCode}: {message}")
    {
        StatusCode = statusCode;
        RetryAfter = retryAfter;
    }
}

/// <summary>Result of collecting upstream chunks for non-streaming requests.</summary>
public sealed class UpstreamCollectResult
{
    public int StatusCode { get; set; }
    public string ErrorText { get; set; } = "";
    public List<string> ContentParts { get; set; } = [];
    public int PromptTokens { get; set; }
    public int CompletionTokens { get; set; }
    public int TotalTokens { get; set; }
    public int CacheReadInputTokens { get; set; }
    public int CacheCreationInputTokens { get; set; }
    public double Credit { get; set; }
}

/// <summary>
/// HttpClient-based upstream request client.
/// No total timeout, 30min response header timeout.
/// </summary>
public sealed class UpstreamClient : IDisposable
{
    // ═══ Constants ═══

    private static readonly TimeSpan IdleTimeout = TimeSpan.FromSeconds(120);
    private const int MaxErrorBodySize = 1 << 20; // 1MB

    private static readonly HashSet<string> ProtectedHeaders = new(StringComparer.OrdinalIgnoreCase)
    {
        "Authorization", "X-Machine-Id", "X-User-Id", "Content-Type", "Host",
    };

    // ═══ State ═══

    private readonly HttpClient _http;
    public string MachineId { get; }

    // ═══ Constructor ═══

    public UpstreamClient(int maxConnsPerHost = Constants.Defaults.UpstreamMaxConnsPerHost)
    {
        MachineId = ComputeFNV128();

        var handler = new SocketsHttpHandler
        {
            MaxConnectionsPerServer = maxConnsPerHost,
            PooledConnectionIdleTimeout = TimeSpan.FromMinutes(5),
            ConnectTimeout = TimeSpan.FromSeconds(30),
        };

        _http = new HttpClient(handler)
        {
            Timeout = Timeout.InfiniteTimeSpan, // No total timeout
        };

        // Set response header timeout via HttpClient.DefaultRequestHeaders is not available,
        // so we use per-request timeout via CancellationTokenSource
    }

    // ═══ Upstream Header Construction ═══

    /// <summary>
    /// Build complete upstream request headers.
    /// </summary>
    public Dictionary<string, string> BuildUpstreamHeaders(
        string model,
        UpstreamIntent intent,
        string userId,
        string machineId,
        string bearerToken)
    {
        var intentStr = intent switch
        {
            UpstreamIntent.Craft => "craft",
            UpstreamIntent.CodeCompletion => "CodeCompletion",
            UpstreamIntent.Embedding => "embedding",
            _ => "craft",
        };
        var rid = GenerateRequestID();
        var span = GenerateSpanID();

        var headers = new Dictionary<string, string>
        {
            ["Accept"] = "text/event-stream",
            ["Content-Type"] = "application/json",
            ["b3"] = $"{rid}-{span}-1-",
            ["X-B3-TraceId"] = rid,
            ["X-B3-ParentSpanId"] = "",
            ["X-B3-SpanId"] = span,
            ["X-B3-Sampled"] = "1",
            ["X-Agent-Intent"] = intentStr,
            ["X-Env-ID"] = "production",
            ["X-Domain"] = Constants.Upstream.Domain,
            ["X-Product"] = "SaaS",
            ["X-User-Id"] = userId,
            ["X-Machine-Id"] = machineId,
            ["X-Request-ID"] = rid,
            ["X-Conversation-ID"] = GenerateRequestID(),
            ["X-Session-ID"] = GenerateRequestID(),
            ["X-IDE-Type"] = "CLI",
            ["X-Product-Version"] = Constants.Upstream.ProductVersion,
            ["User-Agent"] = Constants.Upstream.UserAgent,
        };

        if (!string.IsNullOrEmpty(bearerToken))
            headers["Authorization"] = $"Bearer {bearerToken}";

        return headers;
    }

    /// <summary>
    /// Merge extra headers from client into base headers.
    /// </summary>
    public static void MergeExtraHeaders(Dictionary<string, string> baseHeaders, Dictionary<string, string>? extra)
    {
        if (extra == null) return;

        foreach (var (key, value) in extra)
        {
            if (ProtectedHeaders.Contains(key))
                continue;

            if (key.Equals("anthropic-beta", StringComparison.OrdinalIgnoreCase))
            {
                if (baseHeaders.TryGetValue(key, out var existing) && !string.IsNullOrEmpty(existing))
                    baseHeaders[key] = existing + "," + value;
                else
                    baseHeaders[key] = value;
            }
            else
            {
                baseHeaders[key] = value;
            }
        }
    }

    // ═══ Upstream Request ═══

    /// <summary>
    /// Send a POST request to upstream, returning the response stream.
    /// </summary>
    public async Task<(Stream Stream, HttpResponseMessage Response)> DoUpstreamRequestAsync(
        Dictionary<string, object> payload,
        Dictionary<string, string> headers)
    {
        var url = Constants.Upstream.BaseURL + Constants.Upstream.ChatURL;
        using var request = new HttpRequestMessage(HttpMethod.Post, url);

        var json = JsonSerializer.Serialize(payload);
        request.Content = new StringContent(json, Encoding.UTF8, "application/json");

        foreach (var (key, value) in headers)
            request.Headers.TryAddWithoutValidation(key, value);

        // 30 minute response header timeout
        using var cts = new CancellationTokenSource(TimeSpan.FromMinutes(30));
        var response = await _http.SendAsync(request, HttpCompletionOption.ResponseHeadersRead, cts.Token);

        if (response.StatusCode != HttpStatusCode.OK)
        {
            var body = await ReadErrorBodyAsync(response);
            var errText = StripHTML(body);

            double retryAfter = 0;
            if (response.StatusCode == HttpStatusCode.TooManyRequests)
                retryAfter = ParseRetryAfterHeader(response);

            throw new UpstreamError((int)response.StatusCode, errText[..Math.Min(300, errText.Length)], retryAfter);
        }

        var stream = await response.Content.ReadAsStreamAsync();
        return (stream, response);
    }

    /// <summary>
    /// Collect all upstream chunks for non-streaming requests.
    /// </summary>
    public UpstreamCollectResult CollectUpstreamChunks(
        Dictionary<string, object> payload,
        string bearer,
        double timeoutSecs)
    {
        var result = new UpstreamCollectResult();

        try
        {
            var model = payload.TryGetValue("model", out var m) ? m?.ToString() ?? "" : "";
            var headers = BuildUpstreamHeaders(model, UpstreamIntent.Craft, "", MachineId, bearer);

            var task = Task.Run(async () =>
            {
                try
                {
                    var (stream, response) = await DoUpstreamRequestAsync(payload, headers);
                    result.StatusCode = (int)response.StatusCode;

                    if (!response.IsSuccessStatusCode)
                    {
                        using var reader = new StreamReader(stream);
                        var body = await reader.ReadToEndAsync();
                        result.ErrorText = StripHTML(body);
                        return;
                    }

                    using var sr = new StreamReader(stream);
                    string? line;
                    while ((line = await sr.ReadLineAsync()) != null)
                    {
                        var trimmed = line.Trim();
                        if (!trimmed.StartsWith("data: ") || trimmed == "data: [DONE]") continue;

                        var jsonStr = trimmed[6..];
                        try
                        {
                            using var doc = JsonDocument.Parse(jsonStr);
                            var root = doc.RootElement;

                            // Extract content
                            if (root.TryGetProperty("choices", out var choices) && choices.GetArrayLength() > 0)
                            {
                                var firstChoice = choices[0];
                                if (firstChoice.TryGetProperty("delta", out var delta))
                                {
                                    if (delta.TryGetProperty("content", out var contentEl))
                                    {
                                        var content = contentEl.GetString();
                                        if (!string.IsNullOrEmpty(content))
                                            result.ContentParts.Add(content);
                                    }
                                }
                            }

                            // Extract usage
                            if (root.TryGetProperty("usage", out var usage))
                            {
                                if (usage.TryGetProperty("prompt_tokens", out var pt))
                                    result.PromptTokens = pt.GetInt32();
                                if (usage.TryGetProperty("completion_tokens", out var ct))
                                    result.CompletionTokens = ct.GetInt32();
                                if (usage.TryGetProperty("total_tokens", out var tt))
                                    result.TotalTokens = tt.GetInt32();
                                if (usage.TryGetProperty("prompt_tokens_details", out var ptd))
                                {
                                    if (ptd.TryGetProperty("cached_tokens", out var cachedEl))
                                    {
                                        var v = cachedEl.ValueKind == JsonValueKind.Number ? cachedEl.GetInt32() : 0;
                                        if (v > 0) result.CacheReadInputTokens = v;
                                    }
                                    if (ptd.TryGetProperty("cache_creation_tokens", out var cctEl))
                                    {
                                        var v = cctEl.ValueKind == JsonValueKind.Number ? cctEl.GetInt32() : 0;
                                        if (v > 0) result.CacheCreationInputTokens = v;
                                    }
                                }
                                if (usage.TryGetProperty("credit", out var cr) && cr.ValueKind == JsonValueKind.Number)
                                    result.Credit = cr.GetDouble();
                            }
                        }
                        catch
                        {
                            // Skip malformed chunks
                        }
                    }
                }
                catch (Exception ex)
                {
                    result.StatusCode = 500;
                    result.ErrorText = ex.Message;
                }
            });

            task.Wait(TimeSpan.FromSeconds(timeoutSecs));
        }
        catch
        {
            // Timeout
        }

        return result;
    }

    /// <summary>
    /// Fetch models from upstream config endpoint.
    /// </summary>
    public async Task<List<Dictionary<string, string>>> FetchModelsAsync()
    {
        var result = new List<Dictionary<string, string>>();

        // Add hardcoded extra models
        foreach (var (name, ownedBy) in Constants.ExtraModels)
        {
            result.Add(new Dictionary<string, string>
            {
                ["id"] = name,
                ["object"] = "model",
                ["owned_by"] = ownedBy,
            });
        }

        try
        {
            var url = Constants.Upstream.BaseURL + Constants.Upstream.ConfigURL;
            using var request = new HttpRequestMessage(HttpMethod.Get, url);
            request.Headers.TryAddWithoutValidation("User-Agent", Constants.Upstream.UserAgent);
            request.Headers.TryAddWithoutValidation("X-Product", "SaaS");
            request.Headers.TryAddWithoutValidation("X-Domain", Constants.Upstream.Domain);

            using var cts = new CancellationTokenSource(TimeSpan.FromSeconds(15));
            using var response = await _http.SendAsync(request, cts.Token);
            var body = await response.Content.ReadAsStringAsync();

            using var doc = JsonDocument.Parse(body);
            var root = doc.RootElement;

            if (root.TryGetProperty("data", out var data) && data.ValueKind == JsonValueKind.Array)
            {
                foreach (var model in data.EnumerateArray())
                {
                    var id = model.TryGetProperty("id", out var idEl) ? idEl.GetString() : null;
                    if (string.IsNullOrEmpty(id)) continue;

                    // Skip if already in extra models
                    if (result.Exists(m => m["id"] == id)) continue;

                    var ownedBy = model.TryGetProperty("owned_by", out var obEl)
                        ? obEl.GetString() ?? Constants.InferOwnedBy(id)
                        : Constants.InferOwnedBy(id!);

                    result.Add(new Dictionary<string, string>
                    {
                        ["id"] = id!,
                        ["object"] = "model",
                        ["owned_by"] = ownedBy,
                    });
                }
            }
        }
        catch
        {
            // Return just extra models on failure
        }

        return result;
    }

    // ═══ FNV-128a Hash ═══

    /// <summary>
    /// Generate a stable FNV-128a hash from hostname + homeDir as Machine-Id.
    /// Matches Go fnv.New128a() behavior.
    /// </summary>
    private static string ComputeFNV128()
    {
        var hostname = Environment.MachineName ?? "unknown-host";
        var homeDir = Environment.GetFolderPath(Environment.SpecialFolder.UserProfile);
        var seed = hostname + "|" + homeDir;

        // FNV-128a parameters
        // offset basis: 0x6c62272e07bb014262b821756295c58d
        // prime: 0x0000000001000000000000000000013b
        var hash = new UInt128(
            upper: 0x6c62272e07bb0142UL | (0x62b821756295c58dUL << 0),
            lower: 0x62b821756295c58dUL
        );

        // Use 4 x UInt64 limbs, little-endian
        var h = new ulong[] { 0x6295c58d, 0x07bb0142, 0x62b82175, 0x6c62272e };
        var prime = new ulong[] { 0x0000013b, 0x00000000, 0x00000000, 0x00000001 };

        foreach (var b in Encoding.UTF8.GetBytes(seed))
        {
            // hash ^= byte
            h[3] ^= b;

            // hash *= prime (128-bit multiplication using 4 x 64-bit limbs)
            var newHash = new ulong[4];
            for (var i = 0; i < 4; i++)
            {
                ulong carry = 0;
                for (var j = 0; j <= i; j++)
                {
                    var product = h[i - j] * prime[j];
                    var lo = product + carry;
                    carry = (product < lo) ? 1uL : 0uL;
                    carry += (product >> 63) >> 1;
                    var sum = newHash[i] + lo;
                    if (sum < newHash[i]) carry++;
                    newHash[i] = sum;
                }
            }

            h = newHash;
        }

        // Convert to hex string (big-endian output)
        return $"{h[3]:x16}{h[2]:x16}{h[1]:x16}{h[0]:x16}";
    }

    // ═══ ID Generation ═══

    internal static string GenerateRequestID()
    {
        var bytes = new byte[16];
        Random.Shared.NextBytes(bytes);
        return Convert.ToHexString(bytes).ToLowerInvariant();
    }

    internal static string GenerateSpanID()
    {
        var bytes = new byte[8];
        Random.Shared.NextBytes(bytes);
        return Convert.ToHexString(bytes).ToLowerInvariant();
    }

    // ═══ Utility Methods ═══

    private static async Task<string> ReadErrorBodyAsync(HttpResponseMessage response)
    {
        try
        {
            var body = await response.Content.ReadAsStringAsync();
            return body.Length > MaxErrorBodySize ? body[..MaxErrorBodySize] : body;
        }
        catch
        {
            return "";
        }
    }

    private static string StripHTML(string s)
    {
        if (string.IsNullOrEmpty(s)) return s;
        var result = new StringBuilder();
        var inTag = false;
        foreach (var c in s)
        {
            if (c == '<') inTag = true;
            else if (c == '>') { inTag = false; continue; }
            if (!inTag) result.Append(c);
        }
        return result.ToString().Trim();
    }

    private static double ParseRetryAfterHeader(HttpResponseMessage response)
    {
        var value = response.Headers.RetryAfter?.Delta?.TotalSeconds;
        if (value.HasValue && value.Value > 0)
            return Math.Min(value.Value, 60);

        if (response.Headers.TryGetValues("Retry-After", out var values))
        {
            foreach (var v in values)
            {
                if (double.TryParse(v, out var seconds) && seconds > 0)
                    return Math.Min(seconds, 60);
            }
        }

        return 0;
    }

    public void Dispose()
    {
        _http.Dispose();
    }
}
