using System;
using System.Collections.ObjectModel;
using System.ComponentModel;
using System.Runtime.CompilerServices;
using UniviewCodeBuddyProxy.Helpers;
using UniviewCodeBuddyProxy.Services;

namespace UniviewCodeBuddyProxy.ViewModels;

/// <summary>
/// Settings view model — all config properties, theme presets, save/reset.
/// </summary>
public sealed class SettingsViewModel : INotifyPropertyChanged
{
    private readonly ConfigManager _config;
    private readonly ThemeManager _theme;

    public SettingsViewModel(ConfigManager config, ThemeManager theme)
    {
        _config = config;
        _theme = theme;

        // Forward ConfigManager property changes
        _config.PropertyChanged += (_, e) => OnPropertyChanged(e.PropertyName);
    }

    // ── Network ──

    public double Port
    {
        get => _config.Port;
        set => _config.Port = (int)value;
    }

    public string ApiPassword
    {
        get => _config.ApiPassword;
        set => _config.ApiPassword = value;
    }

    public double MaxConcurrentRequests
    {
        get => _config.MaxConcurrentRequests;
        set => _config.MaxConcurrentRequests = (int)value;
    }

    // ── Cache ──

    public bool CacheEnabled
    {
        get => _config.CacheEnabled;
        set => _config.CacheEnabled = value;
    }

    public double CacheTTL
    {
        get => _config.CacheTTL;
        set => _config.CacheTTL = (int)value;
    }

    // ── AI Features ──

    public bool ImageAutoSwitchModel
    {
        get => _config.ImageAutoSwitchModel;
        set => _config.ImageAutoSwitchModel = value;
    }

    public string VisionModel
    {
        get => _config.VisionModel;
        set => _config.VisionModel = value;
    }

    public ObservableCollection<string> VisionModelOptions { get; } = ["glm-4.6v", "glm-5.1", "glm-4.7"];

    public bool ClaudeInject
    {
        get => _config.ClaudeInject;
        set => _config.ClaudeInject = value;
    }

    // ── Resilience ──

    public double MaxRetries
    {
        get => _config.MaxRetries;
        set => _config.MaxRetries = (int)value;
    }

    public double CbMaxFailures
    {
        get => _config.CbMaxFailures;
        set => _config.CbMaxFailures = (int)value;
    }

    public double CbResetTimeoutSecs
    {
        get => _config.CbResetTimeoutSecs;
        set => _config.CbResetTimeoutSecs = (int)value;
    }

    public double CooldownDurationSecs
    {
        get => _config.CooldownDurationSecs;
        set => _config.CooldownDurationSecs = (int)value;
    }

    // ── Telemetry ──

    public bool TelemetryEnabled
    {
        get => _config.TelemetryEnabled;
        set => _config.TelemetryEnabled = value;
    }

    // ── Logging ──

    public double LogMaxSizeMB
    {
        get => _config.LogMaxSizeMB;
        set => _config.LogMaxSizeMB = (int)value;
    }

    public double LogCleanupInterval
    {
        get => _config.LogCleanupInterval;
        set => _config.LogCleanupInterval = (int)value;
    }

    // ── Appearance ──

    public AppearanceMode SelectedAppearance
    {
        get => _theme.AppearanceMode;
        set
        {
            if (_theme.AppearanceMode != value)
            {
                _theme.AppearanceMode = value;
                OnPropertyChanged();
            }
        }
    }

    public ThemeColors Colors => _theme.Colors;

    public string Locale
    {
        get => _config.Locale;
        set => _config.Locale = value;
    }

    public ObservableCollection<string> LocaleOptions { get; } = ["zh-CN", "en"];

    // ── Computed display text for slider labels ──

    public string CacheTTLText => $"{(int)CacheTTL}s";
    public string CbResetTimeoutSecsText => $"{(int)CbResetTimeoutSecs}s";
    public string CooldownDurationSecsText => $"{(int)CooldownDurationSecs}s";
    public string LogMaxSizeMBText => $"{(int)LogMaxSizeMB}MB";

    // ── About ──

    public string AppName => Constants.AppMeta.Name;
    public string BundleId => Constants.AppMeta.BundleId;
    public string Version => "1.0.0";
    public string Build => "1";

    // ── Commands ──

    public void SaveSettings()
    {
        _config.Save();
    }

    public void ResetToDefaults()
    {
        _config.ResetToDefaults();
    }

    // ── Helper ──

    public static string FormatInterval(int seconds)
    {
        if (seconds < 60) return $"{seconds}s";
        if (seconds < 3600)
        {
            var mins = seconds / 60;
            return mins == 1 ? "1min" : $"{mins}min";
        }
        var hours = seconds / 3600;
        var rem = (seconds % 3600) / 60;
        return rem > 0 ? $"{hours}h{rem}m" : $"{hours}h";
    }

    // ── INotifyPropertyChanged ──

    public event PropertyChangedEventHandler? PropertyChanged;
    private void OnPropertyChanged([CallerMemberName] string? name = null)
        => PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(name));
}
