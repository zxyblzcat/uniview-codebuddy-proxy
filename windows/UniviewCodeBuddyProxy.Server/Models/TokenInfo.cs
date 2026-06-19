using System;

namespace UniviewCodeBuddyProxy.Models;

/// <summary>
/// Token display model for the tokens page.
/// </summary>
public sealed class TokenInfo
{
    public string Id { get; init; } = string.Empty;
    public string UserID { get; init; } = string.Empty;
    public TokenStatus Status { get; init; }
    public DateTimeOffset CreatedAt { get; init; }
    public DateTimeOffset? ExpiresAt { get; init; }
    public int RequestCount { get; init; }
    public double ErrorRate { get; init; }
    public DateTimeOffset? LastUsed { get; init; }

    public string StatusLabel => Status switch
    {
        TokenStatus.Active => "Active",
        TokenStatus.Cooldown => "Cooldown",
        TokenStatus.Unavailable => "Unavailable",
        TokenStatus.Expired => "Expired",
        _ => "Unknown"
    };

    public string StatusEmoji => Status switch
    {
        TokenStatus.Active => "🟢",
        TokenStatus.Cooldown => "🟡",
        TokenStatus.Unavailable => "🔴",
        TokenStatus.Expired => "⚪",
        _ => "❓"
    };
}

/// <summary>
/// Token health status.
/// </summary>
public enum TokenStatus
{
    Active,
    Cooldown,
    Unavailable,
    Expired
}
