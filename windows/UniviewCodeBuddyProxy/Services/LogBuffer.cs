using System;
using System.Collections.Generic;
using System.Linq;
using System.Text;
using System.Threading;
using System.Threading.Channels;

namespace UniviewCodeBuddyProxy.Services;

/// <summary>Log level enum.</summary>
public enum LogLevel
{
    Debug,
    Info,
    Warn,
    Error,
}

/// <summary>Log entry record.</summary>
public sealed class LogEntry
{
    public Guid Id { get; } = Guid.NewGuid();
    public DateTime Timestamp { get; }
    public LogLevel Level { get; }
    public string Message { get; }

    public LogEntry(LogLevel level, string message, DateTime? timestamp = null)
    {
        Level = level;
        Message = message;
        Timestamp = timestamp ?? DateTime.UtcNow;
    }

    public string SseTimestamp => Timestamp.ToString("o");
    public string ShortTime => Timestamp.ToLocalTime().ToString("HH:mm:ss.fff");
}

/// <summary>
/// Thread-safe ring buffer log storage with SSE streaming and file logging.
/// </summary>
public sealed class LogBuffer : IDisposable
{
    // ═══ Constants ═══

    private const int Capacity = 10_000;
    private static readonly TimeSpan HeartbeatInterval = TimeSpan.FromSeconds(15);

    // ═══ Ring Buffer State ═══

    private readonly LogEntry?[] _ring = new LogEntry[Capacity];
    private int _writeIndex;
    private bool _hasWrapped;
    private readonly object _ringLock = new();

    // ═══ SSE Subscribers ═══

    private readonly Dictionary<Guid, Channel<LogEvent>> _subscribers = new();
    private readonly object _subLock = new();

    // ═══ File Logging ═══

    private readonly object _fileLock = new();
    private string? _logFilePath;
    private long _currentLogSize;
    private int _maxLogSizeMB = Constants.Defaults.LogMaxSizeMB;

    // ═══ Log Event for SSE ═══

    public enum LogEventType { Entry, Heartbeat }

    public readonly struct LogEvent
    {
        public LogEventType Type { get; }
        public LogEntry? Entry { get; }

        private LogEvent(LogEventType type, LogEntry? entry = null)
        {
            Type = type;
            Entry = entry;
        }

        public static LogEvent FromEntry(LogEntry entry) => new(LogEventType.Entry, entry);
        public static LogEvent Heartbeat() => new(LogEventType.Heartbeat);
    }

    // ═══ Constructor ═══

    public LogBuffer()
    {
        SetupLogFile();
        StartHeartbeatLoop();
    }

    // ═══ Append Methods ═══

    public void Append(LogLevel level, string message)
    {
        var entry = new LogEntry(level, message);
        AppendEntry(entry);
    }

    public void Info(string message) => Append(LogLevel.Info, message);
    public void Warn(string message) => Append(LogLevel.Warn, message);
    public void Error(string message) => Append(LogLevel.Error, message);
    public void Debug(string message) => Append(LogLevel.Debug, message);

    private void AppendEntry(LogEntry entry)
    {
        lock (_ringLock)
        {
            var idx = _writeIndex % Capacity;
            _ring[idx] = entry;
            _writeIndex++;
            if (_writeIndex >= Capacity && !_hasWrapped)
                _hasWrapped = true;
        }

        WriteToFile(entry);
        EmitToSubscribers(LogEvent.FromEntry(entry));
        EntryAppended?.Invoke(entry);
    }

    // ═══ Query Methods ═══

    public int Count
    {
        get { lock (_ringLock) { return _hasWrapped ? Capacity : _writeIndex; } }
    }

    public List<LogEntry> Recent(int count)
    {
        lock (_ringLock)
        {
            var total = _hasWrapped ? Capacity : _writeIndex;
            var start = Math.Max(0, total - count);
            var result = new List<LogEntry>(Math.Min(count, total));
            for (var i = start; i < total; i++)
            {
                var entry = _ring[i % Capacity];
                if (entry != null) result.Add(entry);
            }
            return result;
        }
    }

    public List<LogEntry> All() => Recent(Count);

    public List<LogEntry> Filter(LogLevel level)
    {
        lock (_ringLock)
        {
            var total = _hasWrapped ? Capacity : _writeIndex;
            var result = new List<LogEntry>();
            for (var i = 0; i < total; i++)
            {
                var entry = _ring[i % Capacity];
                if (entry != null && entry.Level == level)
                    result.Add(entry);
            }
            return result;
        }
    }

    public void Clear()
    {
        lock (_ringLock)
        {
            Array.Clear(_ring);
            _writeIndex = 0;
            _hasWrapped = false;
        }
    }

    /// <summary>Event fired when a new entry is appended.</summary>
    public event Action<LogEntry>? EntryAppended;

    // ═══ SSE Log Streaming ═══

    /// <summary>
    /// Subscribe to live log events. Returns a channel that receives backlog + live events + heartbeat.
    /// </summary>
    public Channel<LogEvent> Subscribe(int backlog = 100)
    {
        var channel = Channel.CreateUnbounded<LogEvent>();
        var subId = Guid.NewGuid();

        // Send backlog
        var history = Recent(backlog);
        foreach (var entry in history)
        {
            channel.Writer.TryWrite(LogEvent.FromEntry(entry));
        }

        lock (_subLock)
        {
            _subscribers[subId] = channel;
        }

        // Auto-remove on completion
        _ = Task.Run(async () =>
        {
            await channel.Reader.Completion;
            lock (_subLock)
            {
                _subscribers.Remove(subId);
            }
        });

        return channel;
    }

    /// <summary>
    /// Generate SSE format event text from a LogEvent.
    /// </summary>
    public string SseEventData(LogEvent logEvent)
    {
        switch (logEvent.Type)
        {
            case LogEventType.Entry when logEvent.Entry is { } entry:
                {
                    var escapedMsg = EscapeJson(entry.Message);
                    var data = $"{{\"timestamp\":\"{entry.SseTimestamp}\",\"level\":\"{entry.Level.ToString().ToLowerInvariant()}\",\"message\":{escapedMsg}}}";
                    return $"event: log\ndata: {data}\n\n";
                }
            case LogEventType.Heartbeat:
                {
                    var ts = DateTime.UtcNow.ToString("o");
                    return $"event: heartbeat\ndata: {{\"timestamp\":\"{ts}\"}}\n\n";
                }
            default:
                return "";
        }
    }

    private void EmitToSubscribers(LogEvent logEvent)
    {
        List<Channel<LogEvent>> channels;
        lock (_subLock)
        {
            channels = _subscribers.Values.ToList();
        }
        foreach (var ch in channels)
        {
            ch.Writer.TryWrite(logEvent);
        }
    }

    // ═══ Heartbeat ═══

    private Timer? _heartbeatTimer;

    private void StartHeartbeatLoop()
    {
        _heartbeatTimer = new Timer(_ =>
        {
            EmitToSubscribers(LogEvent.Heartbeat());
        }, null, HeartbeatInterval, HeartbeatInterval);
    }

    // ═══ File Logging ═══

    public int MaxLogSizeMB
    {
        get => _maxLogSizeMB;
        set => _maxLogSizeMB = value;
    }

    private void SetupLogFile()
    {
        try
        {
            var dir = Path.Combine(
                Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData),
                "UniviewCodeBuddyProxy");
            Directory.CreateDirectory(dir);

            _logFilePath = Path.Combine(dir, "proxy.log");

            lock (_fileLock)
            {
                if (!File.Exists(_logFilePath))
                    File.Create(_logFilePath).Dispose();

                var fi = new FileInfo(_logFilePath);
                _currentLogSize = fi.Length;
            }
        }
        catch
        {
            // Ignore file setup errors
        }
    }

    private void WriteToFile(LogEntry entry)
    {
        if (_logFilePath == null) return;

        try
        {
            lock (_fileLock)
            {
                var line = $"{entry.ShortTime} [{entry.Level.ToString().ToUpperInvariant()}] {entry.Message}\n";
                var data = Encoding.UTF8.GetBytes(line);
                using var fs = new FileStream(_logFilePath, FileMode.Append, FileAccess.Write, FileShare.Read);
                fs.Write(data, 0, data.Length);
                _currentLogSize += data.Length;
                CheckAndTruncateIfNeeded();
            }
        }
        catch
        {
            // Ignore write errors
        }
    }

    private void CheckAndTruncateIfNeeded()
    {
        var maxBytes = (long)_maxLogSizeMB * 1024 * 1024;
        if (_currentLogSize <= maxBytes) return;

        try
        {
            // Truncate: clear file and reset
            using var fs = new FileStream(_logFilePath, FileMode.Create, FileAccess.Write, FileShare.Read);
            fs.Flush();
            _currentLogSize = 0;
        }
        catch
        {
            // Ignore truncation errors
        }
    }

    // ═══ JSON Escaping ═══

    private static string EscapeJson(string s)
    {
        var sb = new StringBuilder(s.Length + 8);
        sb.Append('"');
        foreach (var c in s)
        {
            switch (c)
            {
                case '\\': sb.Append("\\\\"); break;
                case '"': sb.Append("\\\""); break;
                case '\n': sb.Append("\\n"); break;
                case '\r': sb.Append("\\r"); break;
                case '\t': sb.Append("\\t"); break;
                default: sb.Append(c); break;
            }
        }
        sb.Append('"');
        return sb.ToString();
    }

    public void Dispose()
    {
        _heartbeatTimer?.Dispose();
    }
}
