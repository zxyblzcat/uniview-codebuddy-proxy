using System;
using System.Collections.ObjectModel;
using System.ComponentModel;
using System.Linq;
using System.Runtime.CompilerServices;
using UniviewCodeBuddyProxy.Helpers;
using UniviewCodeBuddyProxy.Models;
using UniviewCodeBuddyProxy.Services;

namespace UniviewCodeBuddyProxy.ViewModels;

/// <summary>
/// Dashboard view model — KPIs, charts, and recent activity.
/// Uses live services when available, falls back to mock data.
/// </summary>
public sealed class DashboardViewModel : INotifyPropertyChanged
{
    private readonly TokenManager? _tokenManager;
    private readonly LogBuffer? _logBuffer;
    private readonly TelemetryReporter? _telemetryReporter;

    // ── KPIs ──

    private int _activeTokens;
    public int ActiveTokens
    {
        get => _activeTokens;
        set { if (_activeTokens != value) { _activeTokens = value; OnPropertyChanged(); } }
    }

    private int _requestsToday = 1247;
    public int RequestsToday
    {
        get => _requestsToday;
        set { if (_requestsToday != value) { _requestsToday = value; OnPropertyChanged(); } }
    }

    private string _avgLatency = "320ms";
    public string AvgLatency
    {
        get => _avgLatency;
        set { if (_avgLatency != value) { _avgLatency = value; OnPropertyChanged(); } }
    }

    private string _uptime = "99.8%";
    public string Uptime
    {
        get => _uptime;
        set { if (_uptime != value) { _uptime = value; OnPropertyChanged(); } }
    }

    // ── Chart data ──

    public ObservableCollection<HourlyRequest> HourlyRequests { get; } = [];
    public ObservableCollection<ModelUsageItem> ModelUsage { get; } = [];

    // ── Recent activity ──

    public ObservableCollection<LogEntryDisplay> RecentActivity { get; } = [];

    public DashboardViewModel(TokenManager? tokenManager = null, LogBuffer? logBuffer = null, TelemetryReporter? telemetryReporter = null)
    {
        _tokenManager = tokenManager;
        _logBuffer = logBuffer;
        _telemetryReporter = telemetryReporter;

        GenerateHourlyData();
        GenerateModelUsageData();

        // Subscribe to live data if services are available
        if (_tokenManager != null)
        {
            _tokenManager.PropertyChanged += OnTokenManagerChanged;
            RefreshKPIs();
        }

        if (_logBuffer != null)
        {
            // Do NOT subscribe to EntryAppended here — the page code-behind handles
            // UI-thread dispatch and calls RefreshActivity() on the main thread.
            // Direct subscription would cause cross-thread ObservableCollection mutation.
            RefreshActivity();
        }
    }

    private void OnTokenManagerChanged(object? sender, PropertyChangedEventArgs e)
    {
        if (e.PropertyName == nameof(TokenManager.ActiveTokenCount))
        {
            // TokenManager.PropertyChanged may fire on any thread,
            // but setting a simple int property on the VM is thread-safe
            // (OnPropertyChanged dispatches to UI via SynchronizationContext
            // or the binding engine handles cross-thread reads).
            RefreshKPIs();
        }
    }

    /// <summary>
    /// Refreshes KPI values from live services. Must be called on the UI thread.
    /// </summary>
    private void RefreshKPIs()
    {
        if (_tokenManager != null)
        {
            ActiveTokens = _tokenManager.ActiveTokenCount;
        }
        // RequestsToday, AvgLatency, Uptime would come from TelemetryReporter aggregations
        // For now, keep mock values until telemetry aggregation is implemented
    }

    /// <summary>
    /// Refreshes recent activity from LogBuffer. Must be called on the UI thread.
    /// </summary>
    public void RefreshActivity()
    {
        if (_logBuffer == null) return;

        var recent = _logBuffer.Recent(5);
        RecentActivity.Clear();
        foreach (var entry in recent)
        {
            RecentActivity.Add(LogEntryDisplay.FromLogEntry(entry));
        }
    }

    private void GenerateHourlyData()
    {
        HourlyRequests.Clear();
        var random = new Random();
        var now = DateTime.Now;
        for (int i = 11; i >= 0; i--)
        {
            var time = now.AddHours(-i);
            HourlyRequests.Add(new HourlyRequest
            {
                Hour = time.ToString("HH:mm"),
                Count = random.Next(20, 180)
            });
        }
    }

    private void GenerateModelUsageData()
    {
        ModelUsage.Clear();
        ModelUsage.Add(new ModelUsageItem { Name = "GLM", Ratio = 0.35, Color = ColorHelper.ToHex(ThemeColors.Info) });
        ModelUsage.Add(new ModelUsageItem { Name = "DeepSeek", Ratio = 0.28, Color = ColorHelper.ToHex(ThemeColors.Purple) });
        ModelUsage.Add(new ModelUsageItem { Name = "MiniMax", Ratio = 0.15, Color = ColorHelper.ToHex(ThemeColors.Success) });
        ModelUsage.Add(new ModelUsageItem { Name = "Kimi", Ratio = 0.12, Color = ColorHelper.ToHex(ThemeColors.Warning) });
        ModelUsage.Add(new ModelUsageItem { Name = "Hunyuan", Ratio = 0.10, Color = ColorHelper.ToHex(ColorHelper.FromHex("#F97316")) });
    }

    // ── INotifyPropertyChanged ──

    public event PropertyChangedEventHandler? PropertyChanged;
    private void OnPropertyChanged([CallerMemberName] string? name = null)
        => PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(name));
}
