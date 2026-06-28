using Microsoft.UI.Xaml.Media;
using UniviewCodeBuddyProxy.Helpers;

namespace UniviewCodeBuddyProxy.Models;

/// <summary>
/// UI-facing log entry model for display in the Logs page.
/// Uses Services.LogEntry as the authoritative source; this model
/// provides display-oriented formatting for XAML data binding.
/// </summary>
public class LogEntryDisplay
{
    private static int _nextId = 1;

    public int Id { get; init; }
    public DateTimeOffset Timestamp { get; init; }
    public Services.LogLevel Level { get; init; }
    public string Message { get; init; } = string.Empty;

    public LogEntryDisplay()
    {
        Id = Interlocked.Increment(ref _nextId);
        Timestamp = DateTimeOffset.UtcNow;
    }

    public LogEntryDisplay(Services.LogLevel level, string message) : this()
    {
        Level = level;
        Message = message;
    }

    /// <summary>
    /// Creates a display model from a Services.LogEntry.
    /// </summary>
    public static LogEntryDisplay FromLogEntry(Services.LogEntry entry) => new()
    {
        Timestamp = entry.Timestamp,
        Level = entry.Level,
        Message = entry.Message
    };

    /// <summary>
    /// Timestamp formatted as "HH:mm:ss.fff" for SSE push.
    /// </summary>
    public string SseTimestamp => Timestamp.ToString("HH:mm:ss.fff");

    /// <summary>
    /// Timestamp formatted as "HH:mm:ss" for compact display.
    /// </summary>
    public string ShortTime => Timestamp.ToString("HH:mm:ss");

    /// <summary>
    /// Human-readable level name for display.
    /// </summary>
    public string LevelLabel => Level.ToString().ToUpperInvariant();

    /// <summary>
    /// Emoji icon for the log level.
    /// </summary>
    public string LevelEmoji => Level switch
    {
        Services.LogLevel.Debug => "🔍",
        Services.LogLevel.Info => "ℹ️",
        Services.LogLevel.Warn => "⚠️",
        Services.LogLevel.Error => "❌",
        _ => "📋"
    };

    /// <summary>
    /// Short tag for the log level (DBG/INFO/WARN/ERR).
    /// </summary>
    public string LevelTag => Level switch
    {
        Services.LogLevel.Debug => "DBG",
        Services.LogLevel.Info => "INFO",
        Services.LogLevel.Warn => "WARN",
        Services.LogLevel.Error => "ERR",
        _ => Level.ToString().ToUpperInvariant()
    };

    /// <summary>
    /// Background brush for the log row, based on level.
    /// </summary>
    public Brush LevelBgBrush => Level switch
    {
        Services.LogLevel.Error => ThemeColors.DangerSubtle.ToBrush(),
        Services.LogLevel.Warn => ThemeColors.WarningSubtle.ToBrush(),
        Services.LogLevel.Debug => ThemeColors.InfoSubtle.ToBrush(),
        _ => new SolidColorBrush(Microsoft.UI.Colors.Transparent)
    };

    /// <summary>
    /// Badge background brush for the level tag.
    /// </summary>
    public Brush LevelBadgeBrush => Level switch
    {
        Services.LogLevel.Error => ThemeColors.Danger.ToBrush(),
        Services.LogLevel.Warn => ThemeColors.Warning.ToBrush(),
        Services.LogLevel.Info => ThemeColors.Info.ToBrush(),
        Services.LogLevel.Debug => ThemeColors.Purple.ToBrush(),
        _ => ColorHelper.FromHex("#888888").ToBrush()
    };

    /// <summary>
    /// Foreground brush for the level badge text.
    /// All level badge backgrounds are saturated colors, so white text is readable
    /// in both light and dark modes.
    /// </summary>
    public Brush LevelBadgeFg => new SolidColorBrush(Microsoft.UI.Colors.White);
}
