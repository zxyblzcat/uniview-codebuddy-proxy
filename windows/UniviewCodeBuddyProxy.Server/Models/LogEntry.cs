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
}
