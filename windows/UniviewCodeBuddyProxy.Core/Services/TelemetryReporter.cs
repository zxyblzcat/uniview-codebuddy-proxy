using System;
using System.Collections.Generic;
using System.Linq;
using System.Net.Http;
using System.Text;
using System.Text.Json;
using System.Threading;

namespace UniviewCodeBuddyProxy.Services;

/// <summary>Event code constants.</summary>
public static class EventCode
{
    public const string ChatRequestSend = "chat_request_send";
    public const string ChatMessageResponse = "chat_message_response";
    public const string CompletionTrigger = "completion_trigger";
    public const string CompletionResponse = "completion_response";
    public const string CompletionAction = "completion_action";
    public const string ResponsesRequestSend = "responses_request_send";
    public const string ResponsesMessageResponse = "responses_message_response";
    public const string UpstreamRetry = "upstream_retry";
    public const string UpstreamFailure = "upstream_failure";
}

/// <summary>Pending telemetry event.</summary>
internal sealed class TelemetryEvent
{
    public string EventCode { get; }
    public DateTime Timestamp { get; }
    public string? Model { get; }
    public Dictionary<string, object> Properties { get; }

    public TelemetryEvent(string eventCode, string? model, Dictionary<string, object> properties)
    {
        EventCode = eventCode;
        Timestamp = DateTime.UtcNow;
        Model = model;
        Properties = properties;
    }

    /// <summary>Serialize for the /v2/report endpoint.</summary>
    public Dictionary<string, object> ToJSON(long reportDelay)
    {
        var json = new Dictionary<string, object>
        {
            ["eventCode"] = EventCode,
            ["timestamp"] = new DateTimeOffset(Timestamp).ToUnixTimeMilliseconds(),
            ["reportDelay"] = reportDelay,
        };
        if (Model != null)
            json["model"] = Model;
        if (Properties.Count > 0)
            json["data"] = Properties;
        return json;
    }
}

/// <summary>
/// Batch event reporting service with timer-based and threshold-based flushing.
/// Posts events to /v2/report endpoint.
/// </summary>
public sealed class TelemetryReporter : IDisposable
{
    // ═══ Constants ═══

    private const double FireDelaySecs = 2.0;
    private const int MaxBatchSize = 50;
    private const int RequestTimeoutSecs = 10;

    // ═══ Dependencies ═══

    private readonly ConfigManager _configManager;
    private readonly TokenManager _tokenManager;
    private readonly HttpClient _http;

    // ═══ State ═══

    private readonly List<TelemetryEvent> _events = new();
    private readonly object _lock = new();
    private Timer? _fireTimer;
    private bool _isStopped;

    public bool IsEnabled => _configManager.TelemetryEnabled;

    // ═══ Constructor ═══

    public TelemetryReporter(ConfigManager configManager, TokenManager tokenManager)
    {
        _configManager = configManager;
        _tokenManager = tokenManager;
        _http = new HttpClient { Timeout = TimeSpan.FromSeconds(RequestTimeoutSecs) };

        StartFireLoop();
    }

    // ═══ Fire Loop ═══

    private void StartFireLoop()
    {
        _fireTimer = new Timer(_ => FireBatch(), null,
            TimeSpan.FromSeconds(FireDelaySecs),
            TimeSpan.FromSeconds(FireDelaySecs));
    }

    private void StopFireLoop()
    {
        _fireTimer?.Dispose();
        _fireTimer = null;
    }

    // ═══ Event Adding ═══

    /// <summary>Generic event add entry point.</summary>
    public void Report(string eventCode, string? model = null, Dictionary<string, object>? properties = null)
    {
        if (!_configManager.TelemetryEnabled) return;

        lock (_lock)
        {
            if (_isStopped) return;
            var evt = new TelemetryEvent(eventCode, model, properties ?? new());
            _events.Add(evt);

            if (_events.Count >= MaxBatchSize)
                FireBatch();
        }
    }

    // ═══ Batch Sending ═══

    private void FireBatch()
    {
        List<TelemetryEvent> batch;
        lock (_lock)
        {
            if (_events.Count == 0) return;
            batch = new List<TelemetryEvent>(_events);
            _events.Clear();
        }

        _ = SendBatchAsync(batch);
    }

    private async Task SendBatchAsync(List<TelemetryEvent> events)
    {
        if (events.Count == 0) return;

        var now = DateTimeOffset.UtcNow.ToUnixTimeMilliseconds();
        var payloadArray = events.Select(evt =>
        {
            var eventTimestamp = new DateTimeOffset(evt.Timestamp).ToUnixTimeMilliseconds();
            var reportDelay = now - eventTimestamp;
            return evt.ToJSON(reportDelay);
        }).ToList();

        try
        {
            var url = Constants.Upstream.BaseURL + Constants.Upstream.ReportURL;
            using var request = new HttpRequestMessage(HttpMethod.Post, url);

            var json = JsonSerializer.Serialize(payloadArray);
            request.Content = new StringContent(json, Encoding.UTF8, "application/json");
            request.Headers.TryAddWithoutValidation("Content-Type", "application/json");
            request.Headers.TryAddWithoutValidation("X-Product", "SaaS");
            request.Headers.TryAddWithoutValidation("X-Domain", Constants.Upstream.Domain);
            request.Headers.TryAddWithoutValidation("User-Agent", Constants.Upstream.UserAgent);

            var userId = _tokenManager.CurrentUserID;
            if (!string.IsNullOrEmpty(userId))
                request.Headers.TryAddWithoutValidation("X-User-Id", userId);

            var bearer = _tokenManager.NextToken()?.Bearer;
            if (!string.IsNullOrEmpty(bearer))
                request.Headers.TryAddWithoutValidation("Authorization", $"Bearer {bearer}");

            await _http.SendAsync(request);
        }
        catch
        {
            // Ignore telemetry send errors
        }
    }

    // ═══ Convenience Methods ═══

    /// <summary>Report chat_request_send event.</summary>
    public void ReportChatRequest(string model)
    {
        Report(EventCode.ChatRequestSend, model, new Dictionary<string, object>
        {
            ["mode"] = "craft",
            ["inputType"] = "text",
        });
    }

    /// <summary>Report chat_message_response event.</summary>
    public void ReportChatResponse(string model, double latency, double credit = 0)
    {
        var props = new Dictionary<string, object>
        {
            ["latency"] = latency,
        };
        if (credit > 0)
            props["credit"] = credit;

        Report(EventCode.ChatMessageResponse, model, props);
    }

    /// <summary>Report responses_request_send event.</summary>
    public void ReportResponsesRequest(string model)
    {
        Report(EventCode.ResponsesRequestSend, model, new Dictionary<string, object>
        {
            ["mode"] = "craft",
            ["inputType"] = "text",
        });
    }

    /// <summary>Report upstream_retry event.</summary>
    public void ReportUpstreamRetry(string model, int statusCode, int attempt, int maxRetries, long delayMs)
    {
        Report(EventCode.UpstreamRetry, model, new Dictionary<string, object>
        {
            ["model"] = model,
            ["statusCode"] = statusCode,
            ["attempt"] = attempt,
            ["maxRetries"] = maxRetries,
            ["delayMs"] = delayMs,
        });
    }

    /// <summary>Report upstream_failure event.</summary>
    public void ReportUpstreamFailure(string model, int statusCode, int attempt, int maxRetries, string errMsg)
    {
        Report(EventCode.UpstreamFailure, model, new Dictionary<string, object>
        {
            ["model"] = model,
            ["statusCode"] = statusCode,
            ["attempt"] = attempt,
            ["maxRetries"] = maxRetries,
            ["errMsg"] = errMsg,
        });
    }

    // ═══ Lifecycle ═══

    /// <summary>Stop the reporter and flush remaining events.</summary>
    public void Shutdown()
    {
        lock (_lock)
        {
            if (_isStopped) return;
            _isStopped = true;
        }

        StopFireLoop();
        FireBatch();
    }

    public void Dispose()
    {
        Shutdown();
        _http.Dispose();
    }
}
