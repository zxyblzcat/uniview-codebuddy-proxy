using System;

using Microsoft.UI.Xaml.Media;
using UniviewCodeBuddyProxy.Helpers;

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
        TokenStatus.Active => "活跃",
        TokenStatus.Cooldown => "冷却",
        TokenStatus.Unavailable => "不可用",
        TokenStatus.Expired => "已过期",
        _ => "未知"
    };

    public string StatusEmoji => Status switch
    {
        TokenStatus.Active => "🟢",
        TokenStatus.Cooldown => "🟡",
        TokenStatus.Unavailable => "🔴",
        TokenStatus.Expired => "⚪",
        _ => "❓"
    };

    public Brush StatusBadgeColor => Status switch
    {
        TokenStatus.Active => ThemeColors.SuccessSubtle.ToBrush(),
        TokenStatus.Cooldown => ThemeColors.WarningSubtle.ToBrush(),
        TokenStatus.Unavailable => ThemeColors.DangerSubtle.ToBrush(),
        TokenStatus.Expired => new SolidColorBrush(Microsoft.UI.Colors.Gray),
        _ => new SolidColorBrush(Microsoft.UI.Colors.Transparent)
    };

    /// <summary>
    /// Foreground brush for status badge text — matches macOS statusBadge()
    /// where fg color varies per status (e.g. green text on green-tinted bg).
    /// </summary>
    public Brush StatusBadgeFg => Status switch
    {
        TokenStatus.Active => ThemeColors.Success.ToBrush(),
        TokenStatus.Cooldown => ThemeColors.Warning.ToBrush(),
        TokenStatus.Unavailable => ThemeColors.Danger.ToBrush(),
        TokenStatus.Expired => new SolidColorBrush(Microsoft.UI.Colors.Gray),
        _ => new SolidColorBrush(Microsoft.UI.Colors.Transparent)
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
