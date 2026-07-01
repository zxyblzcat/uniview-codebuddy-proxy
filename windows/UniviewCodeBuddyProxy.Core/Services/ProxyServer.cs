using System;
using System.Collections.Generic;
using System.IO;
using System.Text;
using System.Text.Json;
using System.Threading;
using System.Threading.Channels;
using System.Threading.Tasks;
using Microsoft.AspNetCore.Builder;
using Microsoft.AspNetCore.Hosting;
using Microsoft.AspNetCore.Http;

namespace UniviewCodeBuddyProxy.Services;

/// <summary>
/// ASP.NET Core Minimal API proxy server with all route registrations,
/// middleware (logging, API password auth, concurrency, body size limit),
/// SSE streaming responses, CORS headers.
/// Mirrors the macOS ProxyServer.swift route structure.
/// </summary>
public sealed class ProxyServer
{
    // ═══ Dependencies ═══

    private readonly ConfigManager _configManager;
    private readonly TokenManager _tokenManager;
    private readonly AuthService _authService;
    private readonly LogBuffer _logBuffer;
    private readonly TelemetryReporter _telemetryReporter;
    private readonly UsageStats _usageStats;

    // ═══ Internal Services ═══

    private readonly UpstreamClient _upstreamClient;
    private readonly CircuitBreaker _circuitBreaker;
    private readonly RetryHandler _retryHandler;
    private readonly CacheManager _cacheManager;
    private readonly ProxyController _proxyController;

    // ═══ State ═══

    private WebApplication? _app;
    private readonly SemaphoreSlim _concurrencyLimiter;

    public ProxyServer(
        ConfigManager configManager,
        TokenManager tokenManager,
        AuthService authService,
        LogBuffer logBuffer,
        TelemetryReporter telemetryReporter,
        UsageStats usageStats)
    {
        _configManager = configManager;
        _tokenManager = tokenManager;
        _authService = authService;
        _logBuffer = logBuffer;
        _telemetryReporter = telemetryReporter;
        _usageStats = usageStats;

        _upstreamClient = new UpstreamClient(configManager.UpstreamMaxConnsPerHost);
        _circuitBreaker = new CircuitBreaker();
        _retryHandler = new RetryHandler(_upstreamClient, _circuitBreaker, _tokenManager, _telemetryReporter, configManager.MaxRetries);
        _cacheManager = new CacheManager();
        _proxyController = new ProxyController(
            _configManager, _tokenManager, _upstreamClient, _retryHandler,
            _cacheManager, _circuitBreaker, _telemetryReporter, _logBuffer, _usageStats);

        _concurrencyLimiter = new SemaphoreSlim(configManager.MaxConcurrentRequests, configManager.MaxConcurrentRequests);
    }

    // ═══ Start / Stop ═══

    public void Start(Action<Exception>? onError = null)
    {
        var builder = WebApplication.CreateBuilder();

        // Configure Kestrel to listen on localhost
        builder.WebHost.ConfigureKestrel(options =>
        {
            options.ListenLocalhost(_configManager.Port);
        });

        var app = builder.Build();

        // ═══ Middleware ═══

        // CORS headers
        app.Use(async (ctx, next) =>
        {
            ctx.Response.Headers.AccessControlAllowOrigin = "*";
            ctx.Response.Headers.AccessControlAllowMethods = "GET, POST, PUT, DELETE, OPTIONS, HEAD";
            ctx.Response.Headers.AccessControlAllowHeaders = "Authorization, Content-Type, X-API-Key, anthropic-version, anthropic-beta";
            ctx.Response.Headers.AccessControlMaxAge = "86400";

            if (ctx.Request.Method == "OPTIONS")
            {
                ctx.Response.StatusCode = 204;
                return;
            }

            await next();
        });

        // Logging middleware
        app.Use(async (ctx, next) =>
        {
            var start = DateTime.UtcNow;
            await next();
            var elapsed = (DateTime.UtcNow - start).TotalMilliseconds;
            _logBuffer.Info($"[proxy] {ctx.Request.Method} {ctx.Request.Path} -> {ctx.Response.StatusCode} ({elapsed:F0}ms)");
        });

        // API password auth middleware
        app.Use(async (ctx, next) =>
        {
            var password = _configManager.ApiPassword;
            if (string.IsNullOrEmpty(password))
            {
                await next();
                return;
            }

            var path = ctx.Request.Path.Value ?? "";
            if (path.StartsWith("/auth/") || path == "/health" || path == "/" || path.StartsWith("/api/locale"))
            {
                await next();
                return;
            }

            var authHeader = ctx.Request.Headers.Authorization.FirstOrDefault();
            var apiKeyHeader = ctx.Request.Headers["X-API-Key"].FirstOrDefault();
            var apiKeyQuery = ctx.Request.Query["api_key"].FirstOrDefault();

            var provided = authHeader?
                .Replace("Bearer ", "", StringComparison.OrdinalIgnoreCase)
                ?? apiKeyHeader
                ?? apiKeyQuery;

            if (provided == password)
            {
                await next();
                return;
            }

            ctx.Response.StatusCode = 401;
            ctx.Response.ContentType = "application/json";
            await ctx.Response.WriteAsync("{\"error\":{\"message\":\"Invalid API key\",\"type\":\"invalid_request_error\",\"code\":\"invalid_api_key\"}}");
        });

        // Concurrency limit middleware
        app.Use(async (ctx, next) =>
        {
            var path = ctx.Request.Path.Value ?? "";
            // Only limit proxy routes
            if (!path.StartsWith("/v1/") && !path.StartsWith("/v1/v1/"))
            {
                await next();
                return;
            }

            if (!await _concurrencyLimiter.WaitAsync(0))
            {
                ctx.Response.StatusCode = 429;
                ctx.Response.ContentType = "application/json";
                await ctx.Response.WriteAsync("{\"error\":{\"message\":\"Too many concurrent requests\",\"type\":\"rate_limit_error\"}}");
                return;
            }

            try
            {
                await next();
            }
            finally
            {
                _concurrencyLimiter.Release();
            }
        });

        // Body size limit middleware
        app.Use(async (ctx, next) =>
        {
            if (ctx.Request.ContentLength.HasValue && ctx.Request.ContentLength.Value > Constants.Defaults.MaxBodySizeMB * 1024 * 1024)
            {
                ctx.Response.StatusCode = 413;
                ctx.Response.ContentType = "application/json";
                await ctx.Response.WriteAsync("{\"error\":\"request body too large\"}");
                return;
            }
            await next();
        });

        // ═══ Register Routes ═══
        RegisterRoutes(app);

        _app = app;
        _logBuffer.Info($"proxy server starting on 127.0.0.1:{_configManager.Port}");

        // Run in background -- observe the task to catch port-bind failures
        _ = app.RunAsync().ContinueWith(t =>
        {
            if (t.IsFaulted && t.Exception != null)
            {
                onError?.Invoke(t.Exception);
            }
        }, TaskContinuationOptions.OnlyOnFaulted);
    }

    public void Stop()
    {
        _app?.Lifetime.StopApplication();
        _logBuffer.Info("proxy server stopped");
    }

    // ═══ Route Registration ═══

    private void RegisterRoutes(WebApplication app)
    {
        // ═══ Health Check ═══
        app.MapGet("/health", (HttpContext ctx) =>
        {
            ctx.Response.ContentType = "text/plain";
            return ctx.Response.WriteAsync("ok");
        });

        // ═══ Auth Routes ═══
        app.MapGet("/auth/start", HandleAuthStart);
        app.MapGet("/auth/poll", HandleAuthPoll);
        app.MapPost("/auth/manual", HandleAuthManual);
        app.MapGet("/auth/status", HandleAuthStatus);
        app.MapGet("/auth/tokens", HandleAuthListTokens);
        app.MapDelete("/auth/tokens/{user_id}", HandleAuthDeleteToken);
        app.MapPost("/auth/tokens/{user_id}/refresh", HandleAuthRefreshToken);

        // ═══ Proxy Routes -- OpenAI Format ═══
        app.MapPost("/v1/chat/completions", _proxyController.HandleChatCompletionsAsync);
        app.MapGet("/v1/models", _proxyController.HandleModelsAsync);
        app.MapGet("/v1/models/{id}", (HttpContext ctx, string id) => _proxyController.HandleModelByIDAsync(ctx, id));
        app.MapPost("/v1/completions", _proxyController.HandleCompletionsAsync);
        app.MapPost("/v1/embeddings", _proxyController.HandleEmbeddingsAsync);

        // ═══ Proxy Routes -- Anthropic Format ═══
        app.MapPost("/v1/messages", _proxyController.HandleAnthropicMessagesAsync);
        app.MapPost("/v1/messages/count_tokens", _proxyController.HandleCountTokensAsync);

        // ═══ Proxy Routes -- Responses API ═══
        app.MapPost("/v1/responses", _proxyController.HandleResponsesAsync);
        app.MapPost("/v1/responses/compact", _proxyController.HandleResponsesCompactAsync);

        // ═══ Utility Routes ═══
        app.MapMethods("/v1", new[] { "HEAD" }, () => Results.Ok());
        app.MapMethods("/", new[] { "HEAD" }, () => Results.Ok());
        app.MapGet("/", _proxyController.HandleServiceInfoAsync);

        // ═══ Double-path Registration (/v1/v1/* = mirror of /v1/* for clients that double-prepend) ═══
        app.MapPost("/v1/v1/chat/completions", _proxyController.HandleChatCompletionsAsync);
        app.MapGet("/v1/v1/models", _proxyController.HandleModelsAsync);
        app.MapPost("/v1/v1/completions", _proxyController.HandleCompletionsAsync);
        app.MapPost("/v1/v1/embeddings", _proxyController.HandleEmbeddingsAsync);
        app.MapPost("/v1/v1/messages", _proxyController.HandleAnthropicMessagesAsync);
        app.MapPost("/v1/v1/responses", _proxyController.HandleResponsesAsync);

        // ═══ Management API ═══
        app.MapGet("/api/config", HandleGetConfig);
        app.MapPut("/api/config", HandlePutConfig);
        app.MapGet("/api/logs/stream", HandleLogStream);
        app.MapDelete("/api/logs", HandleClearLogs);
        app.MapGet("/api/locale", HandleGetLocale);
        app.MapPut("/api/locale", HandlePutLocale);
    }

    // ═══ Auth Route Handlers ═══

    private async Task HandleAuthStart(HttpContext ctx)
    {
        try
        {
            var result = await _authService.StartDeviceFlowAsync();
            var json = JsonSerializer.Serialize(new Dictionary<string, string>
            {
                ["auth_url"] = result.AuthURL,
                ["auth_state"] = result.AuthState,
            });
            ctx.Response.ContentType = "application/json";
            await ctx.Response.WriteAsync(json);
        }
        catch (Exception ex)
        {
            ctx.Response.StatusCode = 500;
            ctx.Response.ContentType = "application/json";
            await ctx.Response.WriteAsync($"{{\"error\":\"{EscapeJson(ex.Message)}\"}}");
        }
    }

    private async Task HandleAuthPoll(HttpContext ctx)
    {
        var authState = ctx.Request.Query["auth_state"].FirstOrDefault();
        if (string.IsNullOrEmpty(authState))
        {
            ctx.Response.StatusCode = 400;
            ctx.Response.ContentType = "application/json";
            await ctx.Response.WriteAsync("{\"error\":\"missing auth_state\"}");
            return;
        }

        var result = await _authService.PollTokenAsync(authState);
        switch (result.Type)
        {
            case PollResultType.Waiting:
                ctx.Response.ContentType = "application/json";
                await ctx.Response.WriteAsync("{\"status\":\"waiting\"}");
                break;

            case PollResultType.Success:
                if (result.Token != null)
                    _tokenManager.AddToken(result.Token);
                ctx.Response.ContentType = "application/json";
                await ctx.Response.WriteAsync($"{{\"status\":\"success\",\"user_id\":\"{EscapeJson(result.Token?.UserID ?? "")}\"}}");
                break;

            case PollResultType.Failed:
                ctx.Response.StatusCode = 400;
                ctx.Response.ContentType = "application/json";
                await ctx.Response.WriteAsync($"{{\"error\":\"{EscapeJson(result.ErrorMessage ?? "unknown error")}\"}}");
                break;
        }
    }

    private async Task HandleAuthManual(HttpContext ctx)
    {
        try
        {
            using var reader = new StreamReader(ctx.Request.Body);
            var body = await reader.ReadToEndAsync();
            var json = JsonSerializer.Deserialize<Dictionary<string, string>>(body);
            if (json == null || !json.TryGetValue("bearer_token", out var bearerToken) || string.IsNullOrEmpty(bearerToken))
            {
                ctx.Response.StatusCode = 400;
                ctx.Response.ContentType = "application/json";
                await ctx.Response.WriteAsync("{\"error\":\"missing bearer_token\"}");
                return;
            }

            var tokenData = _authService.ParseManualToken(bearerToken);
            _tokenManager.AddToken(tokenData);
            ctx.Response.ContentType = "application/json";
            await ctx.Response.WriteAsync("{\"status\":\"success\"}");
        }
        catch (Exception ex)
        {
            ctx.Response.StatusCode = 400;
            ctx.Response.ContentType = "application/json";
            await ctx.Response.WriteAsync($"{{\"error\":\"{EscapeJson(ex.Message)}\"}}");
        }
    }

    private async Task HandleAuthStatus(HttpContext ctx)
    {
        var tokens = _tokenManager.GetAllTokens();
        var json = JsonSerializer.Serialize(tokens);
        ctx.Response.ContentType = "application/json";
        await ctx.Response.WriteAsync(json);
    }

    private async Task HandleAuthListTokens(HttpContext ctx)
    {
        var tokens = _tokenManager.GetAllTokens();
        var json = JsonSerializer.Serialize(tokens);
        ctx.Response.ContentType = "application/json";
        await ctx.Response.WriteAsync(json);
    }

    private async Task HandleAuthDeleteToken(HttpContext ctx, string user_id)
    {
        _tokenManager.RemoveToken(user_id);
        ctx.Response.ContentType = "application/json";
        await ctx.Response.WriteAsync("{\"status\":\"deleted\"}");
    }

    private async Task HandleAuthRefreshToken(HttpContext ctx, string user_id)
    {
        var tokenData = _tokenManager.GetTokenData(user_id);
        if (tokenData == null)
        {
            ctx.Response.StatusCode = 404;
            ctx.Response.ContentType = "application/json";
            await ctx.Response.WriteAsync("{\"error\":\"token not found\"}");
            return;
        }

        try
        {
            var newToken = await _authService.RefreshTokenAsync(tokenData.RefreshToken);
            _tokenManager.RemoveToken(user_id);
            _tokenManager.AddToken(newToken);
            ctx.Response.ContentType = "application/json";
            await ctx.Response.WriteAsync("{\"status\":\"refreshed\"}");
        }
        catch (Exception ex)
        {
            ctx.Response.StatusCode = 500;
            ctx.Response.ContentType = "application/json";
            await ctx.Response.WriteAsync($"{{\"error\":\"{EscapeJson(ex.Message)}\"}}");
        }
    }

    // ═══ Management API Handlers ═══

    private async Task HandleGetConfig(HttpContext ctx)
    {
        var config = _configManager.AsEnvironmentVariables();
        var json = JsonSerializer.Serialize(config);
        ctx.Response.ContentType = "application/json";
        await ctx.Response.WriteAsync(json);
    }

    private async Task HandlePutConfig(HttpContext ctx)
    {
        try
        {
            using var reader = new StreamReader(ctx.Request.Body);
            var body = await reader.ReadToEndAsync();
            var json = JsonSerializer.Deserialize<Dictionary<string, JsonElement>>(body);
            if (json != null)
                ApplyConfigUpdates(json);

            ctx.Response.ContentType = "application/json";
            await ctx.Response.WriteAsync("{\"status\":\"updated\"}");
        }
        catch (Exception ex)
        {
            ctx.Response.StatusCode = 400;
            ctx.Response.ContentType = "application/json";
            await ctx.Response.WriteAsync($"{{\"error\":\"{EscapeJson(ex.Message)}\"}}");
        }
    }

    private async Task HandleLogStream(HttpContext ctx)
    {
        ctx.Response.StatusCode = 200;
        ctx.Response.ContentType = "text/event-stream";
        ctx.Response.Headers.CacheControl = "no-cache";
        ctx.Response.Headers.Connection = "keep-alive";

        var channel = _logBuffer.Subscribe(backlog: 100);
        var cts = CancellationTokenSource.CreateLinkedTokenSource(ctx.RequestAborted);

        try
        {
            await foreach (var logEvent in channel.Reader.ReadAllAsync(cts.Token))
            {
                var sseText = _logBuffer.SseEventData(logEvent);
                await ctx.Response.WriteAsync(sseText, cts.Token);
                await ctx.Response.Body.FlushAsync(cts.Token);
            }
        }
        catch (OperationCanceledException)
        {
            // Client disconnected -- normal
        }
        catch
        {
            // Other errors -- just end the stream
        }
        finally
        {
            cts.Cancel();
            channel.Writer.TryComplete();
        }
    }

    private async Task HandleClearLogs(HttpContext ctx)
    {
        _logBuffer.Clear();
        ctx.Response.ContentType = "application/json";
        await ctx.Response.WriteAsync("{\"status\":\"cleared\"}");
    }

    private async Task HandleGetLocale(HttpContext ctx)
    {
        var json = JsonSerializer.Serialize(new Dictionary<string, string> { ["locale"] = _configManager.Locale });
        ctx.Response.ContentType = "application/json";
        await ctx.Response.WriteAsync(json);
    }

    private async Task HandlePutLocale(HttpContext ctx)
    {
        try
        {
            using var reader = new StreamReader(ctx.Request.Body);
            var body = await reader.ReadToEndAsync();
            var json = JsonSerializer.Deserialize<Dictionary<string, string>>(body);
            if (json != null && json.TryGetValue("locale", out var locale))
                _configManager.Locale = locale;

            ctx.Response.ContentType = "application/json";
            await ctx.Response.WriteAsync("{\"status\":\"updated\"}");
        }
        catch
        {
            ctx.Response.StatusCode = 400;
        }
    }

    // ═══ Config Update Helper ═══

    private void ApplyConfigUpdates(Dictionary<string, JsonElement> json)
    {
        if (json.TryGetValue("port", out var v1) && v1.ValueKind == JsonValueKind.Number)
            _configManager.Port = v1.GetInt32();
        if (json.TryGetValue("apiPassword", out var v2) && v2.ValueKind == JsonValueKind.String)
            _configManager.ApiPassword = v2.GetString() ?? "";
        if (json.TryGetValue("cacheEnabled", out var v3) && (v3.ValueKind == JsonValueKind.True || v3.ValueKind == JsonValueKind.False))
            _configManager.CacheEnabled = v3.GetBoolean();
        if (json.TryGetValue("cacheTTL", out var v4) && v4.ValueKind == JsonValueKind.Number)
            _configManager.CacheTTL = v4.GetInt32();
        if (json.TryGetValue("debugMode", out var v5) && (v5.ValueKind == JsonValueKind.True || v5.ValueKind == JsonValueKind.False))
            _configManager.DebugMode = v5.GetBoolean();
        if (json.TryGetValue("claudeInject", out var v6) && (v6.ValueKind == JsonValueKind.True || v6.ValueKind == JsonValueKind.False))
            _configManager.ClaudeInject = v6.GetBoolean();
        if (json.TryGetValue("maxRetries", out var v7) && v7.ValueKind == JsonValueKind.Number)
            _configManager.MaxRetries = v7.GetInt32();
        if (json.TryGetValue("cbMaxFailures", out var v8) && v8.ValueKind == JsonValueKind.Number)
            _configManager.CbMaxFailures = v8.GetInt32();
        if (json.TryGetValue("cbResetTimeoutSecs", out var v9) && v9.ValueKind == JsonValueKind.Number)
            _configManager.CbResetTimeoutSecs = v9.GetInt32();
        if (json.TryGetValue("cooldownDurationSecs", out var v10) && v10.ValueKind == JsonValueKind.Number)
            _configManager.CooldownDurationSecs = v10.GetInt32();
        if (json.TryGetValue("telemetryEnabled", out var v11) && (v11.ValueKind == JsonValueKind.True || v11.ValueKind == JsonValueKind.False))
            _configManager.TelemetryEnabled = v11.GetBoolean();
        if (json.TryGetValue("imageAutoSwitchModel", out var v12) && (v12.ValueKind == JsonValueKind.True || v12.ValueKind == JsonValueKind.False))
            _configManager.ImageAutoSwitchModel = v12.GetBoolean();
        if (json.TryGetValue("visionModel", out var v13) && v13.ValueKind == JsonValueKind.String)
            _configManager.VisionModel = v13.GetString() ?? "glm-4.6v";
        if (json.TryGetValue("maxConcurrentRequests", out var v14) && v14.ValueKind == JsonValueKind.Number)
            _configManager.MaxConcurrentRequests = v14.GetInt32();
        if (json.TryGetValue("upstreamMaxConnsPerHost", out var v15) && v15.ValueKind == JsonValueKind.Number)
            _configManager.UpstreamMaxConnsPerHost = v15.GetInt32();
        if (json.TryGetValue("locale", out var v16) && v16.ValueKind == JsonValueKind.String)
            _configManager.Locale = v16.GetString() ?? "zh-CN";
        if (json.TryGetValue("logMaxSizeMB", out var v17) && v17.ValueKind == JsonValueKind.Number)
            _configManager.LogMaxSizeMB = v17.GetInt32();
        if (json.TryGetValue("logCleanupInterval", out var v18) && v18.ValueKind == JsonValueKind.Number)
            _configManager.LogCleanupInterval = v18.GetInt32();
    }

    // ═══ Utility ═══

    private static string EscapeJson(string s)
    {
        if (string.IsNullOrEmpty(s)) return "";
        return s.Replace("\\", "\\\\").Replace("\"", "\\\"").Replace("\n", "\\n").Replace("\r", "\\r");
    }
}
