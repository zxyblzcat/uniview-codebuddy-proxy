using System.Text.Json.Serialization;

namespace UniviewCodeBuddyProxy.Models;

/// <summary>
/// UI display model for token data, wrapping Services.TokenData with
/// JSON serialization attributes for API responses and display formatting.
/// </summary>
public class TokenDataDisplay
{
    [JsonPropertyName("bearer_token")]
    public string BearerToken { get; set; } = string.Empty;

    [JsonPropertyName("access_token")]
    public string AccessToken { get; set; } = string.Empty;

    [JsonPropertyName("refresh_token")]
    public string RefreshToken { get; set; } = string.Empty;

    [JsonPropertyName("token_type")]
    public string TokenType { get; set; } = "Bearer";

    [JsonPropertyName("expires_in")]
    public int ExpiresIn { get; set; }

    [JsonPropertyName("domain")]
    public string Domain { get; set; } = string.Empty;

    [JsonPropertyName("session_state")]
    public string SessionState { get; set; } = string.Empty;

    [JsonPropertyName("created_at")]
    public long CreatedAt { get; set; }

    [JsonPropertyName("expires_at")]
    public long ExpiresAt { get; set; }

    [JsonPropertyName("user_id")]
    public string UserID { get; set; } = string.Empty;

    /// <summary>
    /// Unique identifier for this token (same as UserID).
    /// </summary>
    [JsonIgnore]
    public string Id => UserID;

    /// <summary>
    /// Whether the token is expired (with 5-second clock-drift tolerance).
    /// </summary>
    [JsonIgnore]
    public bool IsExpired =>
        ExpiresAt > 0 && DateTimeOffset.UtcNow.ToUnixTimeSeconds() > ExpiresAt + 5;

    /// <summary>
    /// The effective bearer string (prefers BearerToken over AccessToken).
    /// </summary>
    [JsonIgnore]
    public string Bearer => string.IsNullOrEmpty(BearerToken) ? AccessToken : BearerToken;

    /// <summary>
    /// Creates a display model from a Services.TokenData.
    /// </summary>
    public static TokenDataDisplay FromServiceToken(Services.TokenData token) => new()
    {
        BearerToken = token.BearerToken,
        AccessToken = token.AccessToken,
        RefreshToken = token.RefreshToken,
        TokenType = token.TokenType,
        ExpiresIn = token.ExpiresIn,
        Domain = token.Domain,
        SessionState = token.SessionState,
        CreatedAt = token.CreatedAt,
        ExpiresAt = token.ExpiresAt,
        UserID = token.UserID,
    };
}
