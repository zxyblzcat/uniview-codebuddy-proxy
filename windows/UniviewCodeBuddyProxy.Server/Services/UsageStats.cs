using System;
using System.Collections.Generic;
using System.ComponentModel;
using System.IO;
using System.Runtime.CompilerServices;
using System.Text.Json;
using System.Text.Json.Serialization;
using System.Timers;

namespace UniviewCodeBuddyProxy.Services;

/// <summary>
/// Per-model usage statistics.
/// </summary>
public sealed class ModelStats
{
    public long Requests { get; set; }
    public long PromptTokens { get; set; }
    public long CompletionTokens { get; set; }
    public double Credit { get; set; }
}

/// <summary>
/// Hourly bucket for the 48-hour rolling window.
/// </summary>
public sealed class HourlyBucket
{
    public int Hour { get; set; }
    public long Requests { get; set; }
    public long PromptTokens { get; set; }
    public long CompletionTokens { get; set; }
    public double Credit { get; set; }
}

/// <summary>
/// JSON-serializable snapshot of a day's usage data.
/// </summary>
public sealed class DailySnapshot
{
    public string Date { get; set; } = string.Empty;

    public long TotalRequests { get; set; }
    public long TotalPromptTokens { get; set; }
    public long TotalCompletionTokens { get; set; }
    public double TotalCredit { get; set; }
    public long CacheHitTokens { get; set; }
    public long CacheMissTokens { get; set; }
    public long SuccessCount { get; set; }
    public long FailureCount { get; set; }
    public double TotalLatency { get; set; }

    public List<HourlyBucket> HourlyBuckets { get; set; } = [];
    public Dictionary<string, ModelStats> ModelDistribution { get; set; } = [];
}

/// <summary>
/// Thread-safe usage statistics tracker with INotifyPropertyChanged for data binding.
/// Persists daily JSON snapshots to %APPDATA%/UniviewCodeBuddyProxy/stats/.
/// </summary>
public sealed class UsageStats : INotifyPropertyChanged, IDisposable
{
    // ═══ Constants ═══

    private const int BucketCount = 48;
    private const double AutoSaveIntervalSecs = 300; // 5 minutes
    private const int RetentionDays = 7;

    // ═══ State ═══

    private readonly object _lock = new();

    private long _totalRequests;
    private long _totalPromptTokens;
    private long _totalCompletionTokens;
    private double _totalCredit;
    private long _cacheHitTokens;
    private long _cacheMissTokens;
    private long _successCount;
    private long _failureCount;
    private double _totalLatency;

    private readonly HourlyBucket[] _hourlyBuckets = new HourlyBucket[BucketCount];
    private readonly Dictionary<string, ModelStats> _modelDistribution = new(StringComparer.OrdinalIgnoreCase);

    private readonly System.Timers.Timer _autoSaveTimer;
    private readonly string _statsDirectory;
    private bool _isDisposed;

    // ═══ Public Properties ═══

    public long TotalRequests { get { lock (_lock) return _totalRequests; } }
    public long TotalPromptTokens { get { lock (_lock) return _totalPromptTokens; } }
    public long TotalCompletionTokens { get { lock (_lock) return _totalCompletionTokens; } }
    public double TotalCredit { get { lock (_lock) return _totalCredit; } }
    public long CacheHitTokens { get { lock (_lock) return _cacheHitTokens; } }
    public long CacheMissTokens { get { lock (_lock) return _cacheMissTokens; } }
    public long SuccessCount { get { lock (_lock) return _successCount; } }
    public long FailureCount { get { lock (_lock) return _failureCount; } }
    public double TotalLatency { get { lock (_lock) return _totalLatency; } }

    /// <summary>Success rate as a value between 0 and 1.</summary>
    public double SuccessRate
    {
        get
        {
            lock (_lock)
            {
                return _totalRequests > 0 ? (double)_successCount / _totalRequests : 0;
            }
        }
    }

    /// <summary>Average latency in milliseconds.</summary>
    public double AvgLatency
    {
        get
        {
            lock (_lock)
            {
                return _totalRequests > 0 ? _totalLatency / _totalRequests * 1000 : 0;
            }
        }
    }

    /// <summary>Cache hit rate as a value between 0 and 1.</summary>
    public double CacheHitRate
    {
        get
        {
            lock (_lock)
            {
                var total = _cacheHitTokens + _cacheMissTokens;
                return total > 0 ? (double)_cacheHitTokens / total : 0;
            }
        }
    }

    /// <summary>Total tokens (prompt + completion).</summary>
    public long TotalTokens
    {
        get
        {
            lock (_lock)
            {
                return _totalPromptTokens + _totalCompletionTokens;
            }
        }
    }

    /// <summary>Get a snapshot of hourly buckets (thread-safe copy).</summary>
    public HourlyBucket[] GetHourlyBuckets()
    {
        lock (_lock)
        {
            var copy = new HourlyBucket[BucketCount];
            for (int i = 0; i < BucketCount; i++)
            {
                copy[i] = new HourlyBucket
                {
                    Hour = _hourlyBuckets[i].Hour,
                    Requests = _hourlyBuckets[i].Requests,
                    PromptTokens = _hourlyBuckets[i].PromptTokens,
                    CompletionTokens = _hourlyBuckets[i].CompletionTokens,
                    Credit = _hourlyBuckets[i].Credit,
                };
            }
            return copy;
        }
    }

    /// <summary>Get a snapshot of model distribution (thread-safe copy).</summary>
    public Dictionary<string, ModelStats> GetModelDistribution()
    {
        lock (_lock)
        {
            var copy = new Dictionary<string, ModelStats>(StringComparer.OrdinalIgnoreCase);
            foreach (var kvp in _modelDistribution)
            {
                copy[kvp.Key] = new ModelStats
                {
                    Requests = kvp.Value.Requests,
                    PromptTokens = kvp.Value.PromptTokens,
                    CompletionTokens = kvp.Value.CompletionTokens,
                    Credit = kvp.Value.Credit,
                };
            }
            return copy;
        }
    }

    // ═══ Constructor ═══

    public UsageStats()
    {
        _statsDirectory = Path.Combine(
            Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData),
            "UniviewCodeBuddyProxy", "stats");

        InitializeBuckets();
        LoadToday();
        CleanupOldFiles();

        _autoSaveTimer = new System.Timers.Timer(AutoSaveIntervalSecs * 1000);
        _autoSaveTimer.Elapsed += OnAutoSave;
        _autoSaveTimer.AutoReset = true;
        _autoSaveTimer.Start();
    }

    private void InitializeBuckets()
    {
        var now = DateTime.Now;
        for (int i = 0; i < BucketCount; i++)
        {
            var hour = now.AddHours(-(BucketCount - 1 - i));
            _hourlyBuckets[i] = new HourlyBucket
            {
                Hour = hour.Hour,
            };
        }
    }

    // ═══ Record ═══

    /// <summary>
    /// Record a completed request's usage data. Thread-safe.
    /// </summary>
    public void RecordRequest(
        string model,
        int promptTokens,
        int completionTokens,
        int totalTokens,
        double credit,
        int cacheHitTokens,
        int cacheCreationInputTokens,
        double latency,
        bool success)
    {
        lock (_lock)
        {
            _totalRequests++;
            _totalPromptTokens += promptTokens;
            _totalCompletionTokens += completionTokens;
            _totalCredit += credit;
            _cacheHitTokens += cacheHitTokens;

            // Cache miss = prompt tokens - cache hit tokens (approximation)
            var cacheMiss = Math.Max(0, promptTokens - cacheHitTokens - cacheCreationInputTokens);
            _cacheMissTokens += cacheMiss;

            if (success)
                _successCount++;
            else
                _failureCount++;

            _totalLatency += latency;

            // Update current hour bucket
            var currentHour = DateTime.Now.Hour;
            for (int i = 0; i < BucketCount; i++)
            {
                if (_hourlyBuckets[i].Hour == currentHour)
                {
                    _hourlyBuckets[i].Requests++;
                    _hourlyBuckets[i].PromptTokens += promptTokens;
                    _hourlyBuckets[i].CompletionTokens += completionTokens;
                    _hourlyBuckets[i].Credit += credit;
                    break;
                }
            }

            // Update model distribution
            if (!_modelDistribution.TryGetValue(model, out var stats))
            {
                stats = new ModelStats();
                _modelDistribution[model] = stats;
            }
            stats.Requests++;
            stats.PromptTokens += promptTokens;
            stats.CompletionTokens += completionTokens;
            stats.Credit += credit;
        }

        // Notify property changes (outside lock to avoid re-entrancy issues)
        NotifyAllChanged();
    }

    // ═══ Persistence ═══

    private void OnAutoSave(object? sender, ElapsedEventArgs e)
    {
        SaveToday();
    }

    public void SaveToday()
    {
        try
        {
            Directory.CreateDirectory(_statsDirectory);

            DailySnapshot snapshot;
            lock (_lock)
            {
                snapshot = new DailySnapshot
                {
                    Date = DateTime.UtcNow.ToString("yyyy-MM-dd"),
                    TotalRequests = _totalRequests,
                    TotalPromptTokens = _totalPromptTokens,
                    TotalCompletionTokens = _totalCompletionTokens,
                    TotalCredit = _totalCredit,
                    CacheHitTokens = _cacheHitTokens,
                    CacheMissTokens = _cacheMissTokens,
                    SuccessCount = _successCount,
                    FailureCount = _failureCount,
                    TotalLatency = _totalLatency,
                    HourlyBuckets = new List<HourlyBucket>(),
                    ModelDistribution = new Dictionary<string, ModelStats>(),
                };

                for (int i = 0; i < BucketCount; i++)
                {
                    snapshot.HourlyBuckets.Add(new HourlyBucket
                    {
                        Hour = _hourlyBuckets[i].Hour,
                        Requests = _hourlyBuckets[i].Requests,
                        PromptTokens = _hourlyBuckets[i].PromptTokens,
                        CompletionTokens = _hourlyBuckets[i].CompletionTokens,
                        Credit = _hourlyBuckets[i].Credit,
                    });
                }

                foreach (var kvp in _modelDistribution)
                {
                    snapshot.ModelDistribution[kvp.Key] = new ModelStats
                    {
                        Requests = kvp.Value.Requests,
                        PromptTokens = kvp.Value.PromptTokens,
                        CompletionTokens = kvp.Value.CompletionTokens,
                        Credit = kvp.Value.Credit,
                    };
                }
            }

            var filePath = Path.Combine(_statsDirectory, $"{snapshot.Date}.json");
            var json = JsonSerializer.Serialize(snapshot, new JsonSerializerOptions { WriteIndented = true });
            File.WriteAllText(filePath, json);
        }
        catch
        {
            // Ignore persistence errors
        }
    }

    private void LoadToday()
    {
        try
        {
            var today = DateTime.UtcNow.ToString("yyyy-MM-dd");
            var filePath = Path.Combine(_statsDirectory, $"{today}.json");
            if (!File.Exists(filePath)) return;

            var json = File.ReadAllText(filePath);
            var snapshot = JsonSerializer.Deserialize<DailySnapshot>(json);
            if (snapshot == null) return;

            lock (_lock)
            {
                _totalRequests = snapshot.TotalRequests;
                _totalPromptTokens = snapshot.TotalPromptTokens;
                _totalCompletionTokens = snapshot.TotalCompletionTokens;
                _totalCredit = snapshot.TotalCredit;
                _cacheHitTokens = snapshot.CacheHitTokens;
                _cacheMissTokens = snapshot.CacheMissTokens;
                _successCount = snapshot.SuccessCount;
                _failureCount = snapshot.FailureCount;
                _totalLatency = snapshot.TotalLatency;

                // Restore hourly buckets (match by hour)
                if (snapshot.HourlyBuckets != null)
                {
                    foreach (var bucket in snapshot.HourlyBuckets)
                    {
                        for (int i = 0; i < BucketCount; i++)
                        {
                            if (_hourlyBuckets[i].Hour == bucket.Hour)
                            {
                                _hourlyBuckets[i].Requests = bucket.Requests;
                                _hourlyBuckets[i].PromptTokens = bucket.PromptTokens;
                                _hourlyBuckets[i].CompletionTokens = bucket.CompletionTokens;
                                _hourlyBuckets[i].Credit = bucket.Credit;
                                break;
                            }
                        }
                    }
                }

                // Restore model distribution
                if (snapshot.ModelDistribution != null)
                {
                    foreach (var kvp in snapshot.ModelDistribution)
                    {
                        _modelDistribution[kvp.Key] = new ModelStats
                        {
                            Requests = kvp.Value.Requests,
                            PromptTokens = kvp.Value.PromptTokens,
                            CompletionTokens = kvp.Value.CompletionTokens,
                            Credit = kvp.Value.Credit,
                        };
                    }
                }
            }
        }
        catch
        {
            // Ignore load errors — start fresh
        }
    }

    private void CleanupOldFiles()
    {
        try
        {
            if (!Directory.Exists(_statsDirectory)) return;

            var cutoff = DateTime.UtcNow.AddDays(-RetentionDays);
            foreach (var file in Directory.GetFiles(_statsDirectory, "*.json"))
            {
                var name = Path.GetFileNameWithoutExtension(file);
                if (DateTime.TryParse(name, out var date) && date < cutoff)
                {
                    try { File.Delete(file); } catch { /* ignore */ }
                }
            }
        }
        catch
        {
            // Ignore cleanup errors
        }
    }

    // ═══ INotifyPropertyChanged ═══

    public event PropertyChangedEventHandler? PropertyChanged;

    private void NotifyAllChanged()
    {
        OnPropertyChanged(nameof(TotalRequests));
        OnPropertyChanged(nameof(TotalPromptTokens));
        OnPropertyChanged(nameof(TotalCompletionTokens));
        OnPropertyChanged(nameof(TotalCredit));
        OnPropertyChanged(nameof(CacheHitTokens));
        OnPropertyChanged(nameof(CacheMissTokens));
        OnPropertyChanged(nameof(SuccessCount));
        OnPropertyChanged(nameof(FailureCount));
        OnPropertyChanged(nameof(TotalLatency));
        OnPropertyChanged(nameof(SuccessRate));
        OnPropertyChanged(nameof(AvgLatency));
        OnPropertyChanged(nameof(CacheHitRate));
        OnPropertyChanged(nameof(TotalTokens));
    }

    private void OnPropertyChanged([CallerMemberName] string? name = null)
        => PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(name));

    // ═══ IDisposable ═══

    public void Dispose()
    {
        if (_isDisposed) return;
        _isDisposed = true;

        _autoSaveTimer.Stop();
        _autoSaveTimer.Dispose();
        SaveToday();
    }
}
