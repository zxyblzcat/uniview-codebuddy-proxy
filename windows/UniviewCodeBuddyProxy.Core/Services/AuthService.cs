using System;
using System.Collections.Generic;
using System.Net.Http;
using System.Net.Http.Json;
using System.Text;
using System.Text.Json;
using System.Threading.Tasks;

namespace UniviewCodeBuddyProxy.Services;

/// <summary>OAuth2 Device Flow authentication result.</summary>
public enum PollResultType { Waiting, Success, Failed }

public sealed class PollResult
{
    public PollResultType Type { get; }
    public TokenData? Token { get; }
    public string? ErrorMessage { get; }

    private PollResult(PollResultType type, TokenData? token = null, string? errorMessage = null)
    {
        Type = type;
        Token = token;
        ErrorMessage = errorMessage;
    }

    public static PollResult Waiting() => new(PollResultType.Waiting);
    public static PollResult Success(TokenData token) => new(PollResultType.Success, token);
    public static PollResult Failed(string message) => new(PollResultType.Failed, errorMessage: message);
}

/// <summary>OAuth2 Device Flow authentication service.</summary>
public sealed class AuthService
{
    private readonly HttpClient _http;

    public AuthService()
    {
        _http = new HttpClient
        {
            Timeout = TimeSpan.FromSeconds(30),
        };
    }

    // ═══ Device Flow ═══

    /// <summary>
    /// Start the Device Flow by requesting an auth URL and state from upstream.
    /// </summary>
    public async Task<(string AuthURL, string AuthState)> StartDeviceFlowAsync()
    {
        var nonce = GenerateNonce();
        var url = $"{Constants.Upstream.BaseURL}{Constants.Upstream.AuthStateURL}?platform=CLI&nonce={nonce}";

        using var request = new HttpRequestMessage(HttpMethod.Post, url);
        foreach (var (key, value) in AuthStartHeaders())
            request.Headers.TryAddWithoutValidation(key, value);

        var body = JsonSerializer.Serialize(new { nonce });
        request.Content = new StringContent(body, Encoding.UTF8, "application/json");

        using var response = await _http.SendAsync(request);
        var responseBody = await response.Content.ReadAsStringAsync();

        using var doc = JsonDocument.Parse(responseBody);
        var root = doc.RootElement;

        var code = root.TryGetProperty("code", out var codeEl) ? codeEl.GetInt32() : -1;
        if (code != 0)
            throw new InvalidOperationException($"Auth start failed: {responseBody[..Math.Min(200, responseBody.Length)]}");

        if (!root.TryGetProperty("data", out var data))
            throw new InvalidOperationException("Missing data in auth start response");

        var state = data.TryGetProperty("state", out var stateEl) ? stateEl.GetString() : null;
        var authUrl = data.TryGetProperty("authUrl", out var urlEl) ? urlEl.GetString() : null;

        if (string.IsNullOrEmpty(state) || string.IsNullOrEmpty(authUrl))
            throw new InvalidOperationException("Missing state or authUrl in auth start response");

        return (authUrl, state);
    }

    /// <summary>
    /// Poll the upstream token endpoint until the user completes login.
    /// </summary>
    public async Task<PollResult> PollTokenAsync(string authState)
    {
        var url = $"{Constants.Upstream.BaseURL}{Constants.Upstream.AuthTokenURL}?state={authState}";

        using var request = new HttpRequestMessage(HttpMethod.Get, url);
        foreach (var (key, value) in AuthPollHeaders())
            request.Headers.TryAddWithoutValidation(key, value);

        try
        {
            using var response = await _http.SendAsync(request);
            var responseBody = await response.Content.ReadAsStringAsync();

            using var doc = JsonDocument.Parse(responseBody);
            var root = doc.RootElement;

            var code = root.TryGetProperty("code", out var codeEl) ? codeEl.GetInt32() : 0;

            // 11217 = waiting for user login
            if (code == 11217)
                return PollResult.Waiting();

            if (code == 0 && root.TryGetProperty("data", out var data))
            {
                var accessToken = data.TryGetProperty("accessToken", out var atEl) ? atEl.GetString() : null;
                if (string.IsNullOrEmpty(accessToken))
                    return PollResult.Failed("Missing accessToken");

                var expiresIn = data.TryGetProperty("expiresIn", out var expEl) ? expEl.GetInt32() : 0;
                if (expiresIn == 0) expiresIn = 3600;

                var now = DateTimeOffset.UtcNow.ToUnixTimeSeconds();
                var userId = ExtractUserIDFromJWT(accessToken!);
                if (string.IsNullOrEmpty(userId))
                    userId = data.TryGetProperty("domain", out var dEl) ? dEl.GetString() ?? "" : "";

                var token = new TokenData
                {
                    BearerToken = accessToken!,
                    AccessToken = accessToken!,
                    RefreshToken = data.TryGetProperty("refreshToken", out var rtEl) ? rtEl.GetString() ?? "" : "",
                    TokenType = data.TryGetProperty("tokenType", out var ttEl) ? ttEl.GetString() ?? "" : "",
                    ExpiresIn = expiresIn,
                    Domain = data.TryGetProperty("domain", out var dmEl) ? dmEl.GetString() ?? "" : "",
                    SessionState = data.TryGetProperty("sessionState", out var ssEl) ? ssEl.GetString() ?? "" : "",
                    CreatedAt = now,
                    ExpiresAt = now + expiresIn,
                    UserID = userId,
                };

                return PollResult.Success(token);
            }

            return PollResult.Failed("auth_poll_failed");
        }
        catch (Exception ex)
        {
            return PollResult.Failed(ex.Message);
        }
    }

    // ═══ Manual Token Entry ═══

    /// <summary>
    /// Parse a manually-provided bearer token into a TokenData struct.
    /// </summary>
    public TokenData ParseManualToken(string bearerToken)
    {
        var now = DateTimeOffset.UtcNow.ToUnixTimeSeconds();
        var userId = ExtractUserIDFromJWT(bearerToken);

        var expiresAt = now + 86400; // default 24h
        var jwtExp = ExtractJWTExp(bearerToken);
        if (jwtExp > 0)
            expiresAt = jwtExp;

        return new TokenData
        {
            BearerToken = bearerToken,
            AccessToken = bearerToken,
            RefreshToken = "",
            TokenType = "",
            ExpiresIn = (int)(expiresAt - now),
            Domain = "",
            SessionState = "",
            CreatedAt = now,
            ExpiresAt = expiresAt,
            UserID = userId,
        };
    }

    // ═══ Token Refresh ═══

    /// <summary>
    /// Refresh an access token using a refresh token.
    /// </summary>
    public async Task<TokenData> RefreshTokenAsync(string refreshToken)
    {
        if (string.IsNullOrEmpty(refreshToken))
            throw new InvalidOperationException("No refresh token available");

        var url = $"{Constants.Upstream.BaseURL}{Constants.Upstream.TokenRefreshURL}";

        using var request = new HttpRequestMessage(HttpMethod.Post, url);
        request.Headers.TryAddWithoutValidation("Authorization", $"Bearer {refreshToken}");
        request.Headers.TryAddWithoutValidation("X-Refresh-Token", refreshToken);
        request.Headers.TryAddWithoutValidation("Content-Type", "application/json");

        using var response = await _http.SendAsync(request);
        var responseBody = await response.Content.ReadAsStringAsync();

        if (!response.IsSuccessStatusCode)
            throw new InvalidOperationException($"Token refresh failed: HTTP {(int)response.StatusCode}: {responseBody[..Math.Min(300, responseBody.Length)]}");

        using var doc = JsonDocument.Parse(responseBody);
        var root = doc.RootElement;

        var code = root.TryGetProperty("code", out var codeEl) ? codeEl.GetInt32() : -1;
        if (code != 0)
            throw new InvalidOperationException($"Token refresh failed: upstream code {code}");

        if (!root.TryGetProperty("data", out var data))
            throw new InvalidOperationException("Missing data in refresh response");

        var accessToken = data.TryGetProperty("accessToken", out var atEl) ? atEl.GetString() : null;
        if (string.IsNullOrEmpty(accessToken))
            throw new InvalidOperationException("Missing accessToken in refresh response");

        var expiresIn = data.TryGetProperty("expiresIn", out var expEl) ? expEl.GetInt32() : 0;
        if (expiresIn == 0) expiresIn = 3600;

        var now = DateTimeOffset.UtcNow.ToUnixTimeSeconds();
        var userId = ExtractUserIDFromJWT(accessToken!);
        if (string.IsNullOrEmpty(userId))
            userId = data.TryGetProperty("domain", out var dEl) ? dEl.GetString() ?? "" : "";

        var newRefreshToken = data.TryGetProperty("refreshToken", out var rtEl) ? rtEl.GetString() : null;
        if (string.IsNullOrEmpty(newRefreshToken))
            newRefreshToken = refreshToken;

        return new TokenData
        {
            BearerToken = accessToken!,
            AccessToken = accessToken!,
            RefreshToken = newRefreshToken!,
            TokenType = data.TryGetProperty("tokenType", out var ttEl) ? ttEl.GetString() ?? "" : "",
            ExpiresIn = expiresIn,
            Domain = data.TryGetProperty("domain", out var dmEl) ? dmEl.GetString() ?? "" : "",
            SessionState = data.TryGetProperty("sessionState", out var ssEl) ? ssEl.GetString() ?? "" : "",
            CreatedAt = now,
            ExpiresAt = now + expiresIn,
            UserID = userId,
        };
    }

    // ═══ Auto-Relogin ═══

    /// <summary>
    /// Attempts auto-relogin: first tries refresh, then falls back to Device Flow.
    /// </summary>
    public async Task<TokenData?> AutoReloginAsync(TokenData? expiredToken, Action<string>? onAuthURL = null)
    {
        // Step 1: Try refresh
        if (expiredToken != null && !string.IsNullOrEmpty(expiredToken.RefreshToken))
        {
            try
            {
                var newToken = await RefreshTokenAsync(expiredToken.RefreshToken);
                return newToken;
            }
            catch
            {
                // Fall through to Device Flow
            }
        }

        // Step 2: Device Flow fallback
        try
        {
            var (authURL, authState) = await StartDeviceFlowAsync();
            onAuthURL?.Invoke(authURL);

            // Poll for up to 60 iterations, 3 seconds apart
            for (var i = 0; i < 60; i++)
            {
                var result = await PollTokenAsync(authState);
                switch (result.Type)
                {
                    case PollResultType.Success:
                        return result.Token;
                    case PollResultType.Failed:
                        return null;
                    case PollResultType.Waiting:
                        await Task.Delay(3000);
                        break;
                }
            }
        }
        catch
        {
            // Auto-relogin failed
        }

        return null;
    }

    // ═══ Header Builders ═══

    /// <summary>
    /// Headers for the auth/state (start) request.
    /// </summary>
    public static Dictionary<string, string> AuthStartHeaders()
    {
        return new Dictionary<string, string>
        {
            ["Host"] = Constants.Upstream.Domain,
            ["Accept"] = "application/json, text/plain, */*",
            ["Content-Type"] = "application/json",
            ["Cache-Control"] = "no-cache",
            ["Pragma"] = "no-cache",
            ["Connection"] = "close",
            ["X-Requested-With"] = "XMLHttpRequest",
            ["X-Domain"] = Constants.Upstream.Domain,
            ["X-No-Authorization"] = "true",
            ["X-No-User-Id"] = "true",
            ["X-No-Enterprise-Id"] = "true",
            ["X-No-Department-Info"] = "true",
            ["User-Agent"] = Constants.Upstream.UserAgent,
            ["X-Product"] = "SaaS",
            ["X-Request-ID"] = GenerateRequestID(),
        };
    }

    /// <summary>
    /// Headers for the auth/token (poll) request -- includes B3 tracing.
    /// </summary>
    public static Dictionary<string, string> AuthPollHeaders()
    {
        var rid = GenerateRequestID();
        var span = GenerateSpanID();
        return new Dictionary<string, string>
        {
            ["Host"] = Constants.Upstream.Domain,
            ["Accept"] = "application/json, text/plain, */*",
            ["Cache-Control"] = "no-cache",
            ["Pragma"] = "no-cache",
            ["Connection"] = "close",
            ["X-Requested-With"] = "XMLHttpRequest",
            ["X-Request-ID"] = rid,
            ["b3"] = $"{rid}-{span}-1-",
            ["X-B3-TraceId"] = rid,
            ["X-B3-ParentSpanId"] = "",
            ["X-B3-SpanId"] = span,
            ["X-B3-Sampled"] = "1",
            ["X-No-Authorization"] = "true",
            ["X-No-User-Id"] = "true",
            ["X-No-Enterprise-Id"] = "true",
            ["X-No-Department-Info"] = "true",
            ["X-Domain"] = Constants.Upstream.Domain,
            ["User-Agent"] = Constants.Upstream.UserAgent,
            ["X-Product"] = "SaaS",
        };
    }

    // ═══ JWT Helpers ═══

    /// <summary>
    /// Extract user ID from a JWT by decoding the payload.
    /// </summary>
    public static string ExtractUserIDFromJWT(string token)
    {
        var parts = SplitJWT(token);
        if (parts.Length < 2) return "";

        try
        {
            var payload = Base64UrlDecode(parts[1]);
            using var doc = JsonDocument.Parse(payload);
            var root = doc.RootElement;

            foreach (var key in new[] { "email", "preferred_username", "sub" })
            {
                if (root.TryGetProperty(key, out var el))
                {
                    var val = el.GetString();
                    if (!string.IsNullOrEmpty(val)) return val;
                }
            }
        }
        catch
        {
            // Ignore JWT parse errors
        }

        return "";
    }

    /// <summary>
    /// Extract the exp claim from a JWT.
    /// </summary>
    public static long ExtractJWTExp(string token)
    {
        var parts = SplitJWT(token);
        if (parts.Length < 2) return 0;

        try
        {
            var payload = Base64UrlDecode(parts[1]);
            using var doc = JsonDocument.Parse(payload);
            var root = doc.RootElement;

            if (root.TryGetProperty("exp", out var el))
            {
                if (el.ValueKind == JsonValueKind.Number)
                    return (long)el.GetDouble();
            }
        }
        catch
        {
            // Ignore JWT parse errors
        }

        return 0;
    }

    private static string[] SplitJWT(string token)
    {
        var result = new List<string>();
        var start = 0;
        for (var i = 0; i < token.Length && result.Count < 2; i++)
        {
            if (token[i] == '.')
            {
                result.Add(token[start..i]);
                start = i + 1;
            }
        }
        result.Add(token[start..]);
        return result.ToArray();
    }

    private static string Base64UrlDecode(string s)
    {
        var sb = new StringBuilder(s);
        sb.Replace('-', '+').Replace('_', '/');
        var remainder = sb.Length % 4;
        if (remainder == 2) sb.Append("==");
        else if (remainder == 3) sb.Append('=');
        var bytes = Convert.FromBase64String(sb.ToString());
        return Encoding.UTF8.GetString(bytes);
    }

    // ═══ ID Generation ═══

    private static string GenerateNonce()
    {
        var bytes = new byte[8];
        Random.Shared.NextBytes(bytes);
        return Convert.ToHexString(bytes).ToLowerInvariant();
    }

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
}
