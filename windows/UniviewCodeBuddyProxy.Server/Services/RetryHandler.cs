using System;
using System.Collections.Generic;
using System.IO;
using System.Net;
using System.Net.Http;
using System.Text.Json;
using System.Threading.Tasks;

namespace UniviewCodeBuddyProxy.Services;

/// <summary>Retry error types.</summary>
public sealed class RetryError : Exception
{
    public RetryErrorType ErrorType { get; }

    private RetryError(RetryErrorType type, string message) : base(message)
    {
        ErrorType = type;
    }

    public static RetryError CircuitOpen() => new(RetryErrorType.CircuitOpen, "circuit breaker is open: upstream service unavailable");
    public static RetryError RateLimited(double retryAfter) => new(RetryErrorType.RateLimited, $"rate limited, retry after {retryAfter}s");
    public static RetryError Unauthorized() => new(RetryErrorType.Unauthorized, "authentication failed");
    public static RetryError ServerError(int code) => new(RetryErrorType.ServerError, $"server error {code}");
    public static RetryError MaxRetriesExceeded() => new(RetryErrorType.MaxRetriesExceeded, "max retries exceeded");
    public static RetryError NoBearerToken() => new(RetryErrorType.NoBearerToken, "no available bearer token after retry");
}

public enum RetryErrorType
{
    CircuitOpen,
    RateLimited,
    Unauthorized,
    ServerError,
    MaxRetriesExceeded,
    NoBearerToken,
}

/// <summary>
/// Upstream request retry handler with exponential backoff and circuit breaker awareness.
/// Only retries on 429/502/503/504. Max 3 retries.
/// </summary>
public sealed class RetryHandler
{
    // ═══ Dependencies ═══

    private readonly UpstreamClient _upstreamClient;
    private readonly CircuitBreaker _circuitBreaker;
    private readonly TokenManager _tokenManager;
    private readonly TelemetryReporter _telemetryReporter;

    // ═══ Configuration ═══

    private readonly int _maxRetries;

    // ═══ Constants ═══

    private const double BaseBackoff = 0.1;    // 100ms
    private const double MaxBackoff = 30.0;     // 30s
    private const double MaxJitter = 0.5;       // 0-500ms
    private const double MaxRetryAfter = 60.0;   // 60s cap

    private static readonly HashSet<int> RetryableStatusCodes = new() { 429, 502, 503, 504 };

    // ═══ Constructor ═══

    public RetryHandler(
        UpstreamClient upstreamClient,
        CircuitBreaker circuitBreaker,
        TokenManager tokenManager,
        TelemetryReporter telemetryReporter,
        int maxRetries = Constants.Defaults.MaxRetries)
    {
        _upstreamClient = upstreamClient;
        _circuitBreaker = circuitBreaker;
        _tokenManager = tokenManager;
        _telemetryReporter = telemetryReporter;
        _maxRetries = Math.Max(0, maxRetries);
    }

    // ═══ Execute ═══

    /// <summary>
    /// Execute an upstream request with retries and circuit breaker awareness.
    /// </summary>
    public async Task<T> ExecuteAsync<T>(
        Dictionary<string, object> payload,
        Dictionary<string, string> headers,
        string model,
        Func<Stream, HttpResponseMessage, Task<T>> transform)
    {
        Exception? lastError = null;
        var currentHeaders = new Dictionary<string, string>(headers);

        for (var attempt = 0; attempt <= _maxRetries; attempt++)
        {
            // Circuit breaker check
            if (!_circuitBreaker.AllowRequest())
            {
                _telemetryReporter.ReportUpstreamFailure(model, 503, attempt + 1, _maxRetries, "circuit breaker open");
                throw RetryError.CircuitOpen();
            }

            try
            {
                var (stream, response) = await _upstreamClient.DoUpstreamRequestAsync(payload, currentHeaders);
                var statusCode = (int)response.StatusCode;

                if (statusCode >= 200 && statusCode <= 299)
                {
                    _circuitBreaker.RecordSuccess();
                    return await transform(stream, response);
                }

                // Read error body
                using var reader = new StreamReader(stream);
                var errorBody = await reader.ReadToEndAsync();
                errorBody = errorBody.Length > 300 ? errorBody[..300] : errorBody;

                _circuitBreaker.RecordFailure();

                // Check if retryable
                if (RetryableStatusCodes.Contains(statusCode))
                {
                    double retryAfter = 0;
                    if (statusCode == 429)
                    {
                        retryAfter = ParseRetryAfter(response);
                        var userId = currentHeaders.GetValueOrDefault("X-User-Id", "");
                        _tokenManager.MarkCooldown(userId, Math.Max(retryAfter, 30));
                    }

                    lastError = new UpstreamError(statusCode, errorBody, retryAfter);

                    if (attempt >= _maxRetries)
                    {
                        _telemetryReporter.ReportUpstreamFailure(model, statusCode, attempt + 1, _maxRetries, errorBody[..Math.Min(100, errorBody.Length)]);
                        throw lastError;
                    }

                    var delay = ComputeBackoff(attempt, retryAfter);
                    _telemetryReporter.ReportUpstreamRetry(model, statusCode, attempt + 1, _maxRetries, (long)(delay * 1000));

                    await Task.Delay(TimeSpan.FromSeconds(delay));

                    // Refresh Bearer Token
                    var newBearer = _tokenManager.NextToken()?.Bearer;
                    if (!string.IsNullOrEmpty(newBearer))
                        currentHeaders["Authorization"] = $"Bearer {newBearer}";
                    else
                        throw RetryError.NoBearerToken();

                    continue;
                }

                // Non-retryable errors (401 etc.)
                if (statusCode == 401)
                {
                    var userId = currentHeaders.GetValueOrDefault("X-User-Id", "");
                    _tokenManager.MarkUnavailable(userId);
                }

                _telemetryReporter.ReportUpstreamFailure(model, statusCode, attempt + 1, _maxRetries, errorBody[..Math.Min(100, errorBody.Length)]);
                throw new UpstreamError(statusCode, errorBody);
            }
            catch (UpstreamError ue)
            {
                lastError = ue;
                _circuitBreaker.RecordFailure();

                if (!RetryableStatusCodes.Contains(ue.StatusCode))
                    throw;

                if (attempt >= _maxRetries)
                    throw;

                var delay = ComputeBackoff(attempt, ue.RetryAfter);
                _telemetryReporter.ReportUpstreamRetry(model, ue.StatusCode, attempt + 1, _maxRetries, (long)(delay * 1000));
                await Task.Delay(TimeSpan.FromSeconds(delay));

                var newBearer = _tokenManager.NextToken()?.Bearer;
                if (!string.IsNullOrEmpty(newBearer))
                    currentHeaders["Authorization"] = $"Bearer {newBearer}";
                else
                    throw RetryError.NoBearerToken();
            }
            catch (Exception ex) when (ex is not UpstreamError and not RetryError)
            {
                lastError = ex;
                _circuitBreaker.RecordFailure();
                _telemetryReporter.ReportUpstreamFailure(model, 0, attempt + 1, _maxRetries, ex.Message);

                if (attempt >= _maxRetries)
                    throw;

                var delay = ComputeBackoff(attempt, 0);
                _telemetryReporter.ReportUpstreamRetry(model, 0, attempt + 1, _maxRetries, (long)(delay * 1000));
                await Task.Delay(TimeSpan.FromSeconds(delay));
            }
        }

        throw lastError ?? RetryError.MaxRetriesExceeded();
    }

    // ═══ Backoff Computation ═══

    /// <summary>
    /// Compute exponential backoff + random jitter.
    /// Formula: 100ms * 2^attempt + random(0, 500ms), cap 30s.
    /// If Retry-After > computed (and <= 60s), use Retry-After.
    /// </summary>
    private static double ComputeBackoff(int attempt, double retryAfter)
    {
        var shift = Math.Min(attempt, 30);
        var @base = BaseBackoff * (1L << shift);
        var jitter = Random.Shared.NextDouble() * MaxJitter;
        var computed = @base + jitter;

        if (computed > MaxBackoff)
            computed = MaxBackoff;

        if (retryAfter > 0)
        {
            var cappedRetryAfter = Math.Min(retryAfter, MaxRetryAfter);
            if (cappedRetryAfter > computed)
                return cappedRetryAfter;
        }

        return computed;
    }

    private static double ParseRetryAfter(HttpResponseMessage response)
    {
        if (response.Headers.RetryAfter?.Delta is { } delta)
        {
            var seconds = delta.TotalSeconds;
            return seconds > 0 ? Math.Min(seconds, MaxRetryAfter) : 0;
        }

        if (response.Headers.TryGetValues("Retry-After", out var values))
        {
            foreach (var v in values)
            {
                if (double.TryParse(v, out var seconds) && seconds > 0)
                    return Math.Min(seconds, MaxRetryAfter);
            }
        }

        return 0;
    }
}
