using System;
using System.Collections.Generic;
using System.ComponentModel;
using System.IO;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace UniviewCodeBuddyProxy.Services;

/// <summary>
/// JSON file-based configuration manager with INotifyPropertyChanged and hot reload.
/// </summary>
public sealed class ConfigManager : INotifyPropertyChanged, IDisposable
{
    // ═══ Constants ═══

    private static readonly string ConfigDir = Path.Combine(
        Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData),
        "UniviewCodeBuddyProxy");
    private static readonly string ConfigPath = Path.Combine(ConfigDir, "config.json");

    private static readonly JsonSerializerOptions JsonOpts = new()
    {
        WriteIndented = true,
        PropertyNamingPolicy = JsonNamingPolicy.CamelCase,
        DefaultIgnoreCondition = JsonIgnoreCondition.WhenWritingNull,
    };

    // ═══ Fields ═══

    private FileSystemWatcher? _watcher;
    private bool _disposed;

    // ═══ Properties with INotifyPropertyChanged ═══

    private int _port = Constants.Defaults.Port;
    public int Port
    {
        get => _port;
        set { if (_port != value) { _port = value; OnPropertyChanged(); Save(); } }
    }

    private string _apiPassword = "";
    public string ApiPassword
    {
        get => _apiPassword;
        set { if (_apiPassword != value) { _apiPassword = value; OnPropertyChanged(); Save(); } }
    }

    private bool _cacheEnabled = Constants.Defaults.CacheEnabled;
    public bool CacheEnabled
    {
        get => _cacheEnabled;
        set { if (_cacheEnabled != value) { _cacheEnabled = value; OnPropertyChanged(); Save(); } }
    }

    private int _cacheTTL = Constants.Defaults.CacheTTL;
    public int CacheTTL
    {
        get => _cacheTTL;
        set { if (_cacheTTL != value) { _cacheTTL = value; OnPropertyChanged(); Save(); } }
    }

    private bool _debugMode = Constants.Defaults.DebugMode;
    public bool DebugMode
    {
        get => _debugMode;
        set { if (_debugMode != value) { _debugMode = value; OnPropertyChanged(); Save(); } }
    }

    private bool _claudeInject = Constants.Defaults.ClaudeInject;
    public bool ClaudeInject
    {
        get => _claudeInject;
        set { if (_claudeInject != value) { _claudeInject = value; OnPropertyChanged(); Save(); } }
    }

    private int _maxRetries = Constants.Defaults.MaxRetries;
    public int MaxRetries
    {
        get => _maxRetries;
        set { if (_maxRetries != value) { _maxRetries = value; OnPropertyChanged(); Save(); } }
    }

    private int _cbMaxFailures = Constants.Defaults.CbMaxFailures;
    public int CbMaxFailures
    {
        get => _cbMaxFailures;
        set { if (_cbMaxFailures != value) { _cbMaxFailures = value; OnPropertyChanged(); Save(); } }
    }

    private int _cbResetTimeoutSecs = Constants.Defaults.CbResetTimeoutSecs;
    public int CbResetTimeoutSecs
    {
        get => _cbResetTimeoutSecs;
        set { if (_cbResetTimeoutSecs != value) { _cbResetTimeoutSecs = value; OnPropertyChanged(); Save(); } }
    }

    private int _cooldownDurationSecs = Constants.Defaults.CooldownDurationSecs;
    public int CooldownDurationSecs
    {
        get => _cooldownDurationSecs;
        set { if (_cooldownDurationSecs != value) { _cooldownDurationSecs = value; OnPropertyChanged(); Save(); } }
    }

    private bool _telemetryEnabled = Constants.Defaults.TelemetryEnabled;
    public bool TelemetryEnabled
    {
        get => _telemetryEnabled;
        set { if (_telemetryEnabled != value) { _telemetryEnabled = value; OnPropertyChanged(); Save(); } }
    }

    private bool _imageAutoSwitchModel = Constants.Defaults.ImageAutoSwitchModel;
    public bool ImageAutoSwitchModel
    {
        get => _imageAutoSwitchModel;
        set { if (_imageAutoSwitchModel != value) { _imageAutoSwitchModel = value; OnPropertyChanged(); Save(); } }
    }

    private string _visionModel = Constants.Defaults.VisionModel;
    public string VisionModel
    {
        get => _visionModel;
        set { if (_visionModel != value) { _visionModel = value; OnPropertyChanged(); Save(); } }
    }

    private int _maxConcurrentRequests = Constants.Defaults.MaxConcurrentRequests;
    public int MaxConcurrentRequests
    {
        get => _maxConcurrentRequests;
        set { if (_maxConcurrentRequests != value) { _maxConcurrentRequests = value; OnPropertyChanged(); Save(); } }
    }

    private int _upstreamMaxConnsPerHost = Constants.Defaults.UpstreamMaxConnsPerHost;
    public int UpstreamMaxConnsPerHost
    {
        get => _upstreamMaxConnsPerHost;
        set { if (_upstreamMaxConnsPerHost != value) { _upstreamMaxConnsPerHost = value; OnPropertyChanged(); Save(); } }
    }

    private string _locale = "zh-CN";
    public string Locale
    {
        get => _locale;
        set { if (_locale != value) { _locale = value; OnPropertyChanged(); Save(); } }
    }

    private int _logMaxSizeMB = Constants.Defaults.LogMaxSizeMB;
    public int LogMaxSizeMB
    {
        get => _logMaxSizeMB;
        set { if (_logMaxSizeMB != value) { _logMaxSizeMB = value; OnPropertyChanged(); Save(); } }
    }

    private int _logCleanupInterval = Constants.Defaults.LogCleanupInterval;
    public int LogCleanupInterval
    {
        get => _logCleanupInterval;
        set { if (_logCleanupInterval != value) { _logCleanupInterval = value; OnPropertyChanged(); Save(); } }
    }

    // ═══ Derived Properties ═══

    public string LogFilePath => Path.Combine(ConfigDir, "proxy.log");
    public string TokenFilePath => Path.Combine(ConfigDir, "token.json");

    // ═══ Event ═══

    public event PropertyChangedEventHandler? PropertyChanged;

    // ═══ Constructor ═══

    public ConfigManager()
    {
        Load();
        SetupWatcher();
    }

    // ═══ Methods ═══

    private void OnPropertyChanged([System.Runtime.CompilerServices.CallerMemberName] string? name = null)
        => PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(name));

    /// <summary>
    /// Load config from JSON file, falling back to defaults.
    /// </summary>
    public void Load()
    {
        try
        {
            if (!File.Exists(ConfigPath)) return;

            var json = File.ReadAllText(ConfigPath);
            var data = JsonSerializer.Deserialize<ConfigData>(json, JsonOpts);
            if (data == null) return;

            _port = data.Port ?? Constants.Defaults.Port;
            _apiPassword = data.ApiPassword ?? "";
            _cacheEnabled = data.CacheEnabled ?? Constants.Defaults.CacheEnabled;
            _cacheTTL = data.CacheTTL ?? Constants.Defaults.CacheTTL;
            _debugMode = data.DebugMode ?? Constants.Defaults.DebugMode;
            _claudeInject = data.ClaudeInject ?? Constants.Defaults.ClaudeInject;
            _maxRetries = data.MaxRetries ?? Constants.Defaults.MaxRetries;
            _cbMaxFailures = data.CbMaxFailures ?? Constants.Defaults.CbMaxFailures;
            _cbResetTimeoutSecs = data.CbResetTimeoutSecs ?? Constants.Defaults.CbResetTimeoutSecs;
            _cooldownDurationSecs = data.CooldownDurationSecs ?? Constants.Defaults.CooldownDurationSecs;
            _telemetryEnabled = data.TelemetryEnabled ?? Constants.Defaults.TelemetryEnabled;
            _imageAutoSwitchModel = data.ImageAutoSwitchModel ?? Constants.Defaults.ImageAutoSwitchModel;
            _visionModel = data.VisionModel ?? Constants.Defaults.VisionModel;
            _maxConcurrentRequests = data.MaxConcurrentRequests ?? Constants.Defaults.MaxConcurrentRequests;
            _upstreamMaxConnsPerHost = data.UpstreamMaxConnsPerHost ?? Constants.Defaults.UpstreamMaxConnsPerHost;
            _locale = data.Locale ?? "zh-CN";
            _logMaxSizeMB = data.LogMaxSizeMB ?? Constants.Defaults.LogMaxSizeMB;
            _logCleanupInterval = data.LogCleanupInterval ?? Constants.Defaults.LogCleanupInterval;
        }
        catch
        {
            // Ignore load errors, use defaults
        }
    }

    /// <summary>
    /// Save current config to JSON file.
    /// </summary>
    public void Save()
    {
        try
        {
            Directory.CreateDirectory(ConfigDir);
            var data = new ConfigData
            {
                Port = _port,
                ApiPassword = _apiPassword,
                CacheEnabled = _cacheEnabled,
                CacheTTL = _cacheTTL,
                DebugMode = _debugMode,
                ClaudeInject = _claudeInject,
                MaxRetries = _maxRetries,
                CbMaxFailures = _cbMaxFailures,
                CbResetTimeoutSecs = _cbResetTimeoutSecs,
                CooldownDurationSecs = _cooldownDurationSecs,
                TelemetryEnabled = _telemetryEnabled,
                ImageAutoSwitchModel = _imageAutoSwitchModel,
                VisionModel = _visionModel,
                MaxConcurrentRequests = _maxConcurrentRequests,
                UpstreamMaxConnsPerHost = _upstreamMaxConnsPerHost,
                Locale = _locale,
                LogMaxSizeMB = _logMaxSizeMB,
                LogCleanupInterval = _logCleanupInterval,
            };
            var json = JsonSerializer.Serialize(data, JsonOpts);
            File.WriteAllText(ConfigPath, json);
        }
        catch
        {
            // Ignore save errors
        }
    }

    /// <summary>
    /// Reset all config to defaults.
    /// </summary>
    public void ResetToDefaults()
    {
        Port = Constants.Defaults.Port;
        ApiPassword = "";
        CacheEnabled = Constants.Defaults.CacheEnabled;
        CacheTTL = Constants.Defaults.CacheTTL;
        DebugMode = Constants.Defaults.DebugMode;
        ClaudeInject = Constants.Defaults.ClaudeInject;
        MaxRetries = Constants.Defaults.MaxRetries;
        CbMaxFailures = Constants.Defaults.CbMaxFailures;
        CbResetTimeoutSecs = Constants.Defaults.CbResetTimeoutSecs;
        CooldownDurationSecs = Constants.Defaults.CooldownDurationSecs;
        TelemetryEnabled = Constants.Defaults.TelemetryEnabled;
        ImageAutoSwitchModel = Constants.Defaults.ImageAutoSwitchModel;
        VisionModel = Constants.Defaults.VisionModel;
        MaxConcurrentRequests = Constants.Defaults.MaxConcurrentRequests;
        UpstreamMaxConnsPerHost = Constants.Defaults.UpstreamMaxConnsPerHost;
        Locale = "zh-CN";
        LogMaxSizeMB = Constants.Defaults.LogMaxSizeMB;
        LogCleanupInterval = Constants.Defaults.LogCleanupInterval;
    }

    /// <summary>
    /// Export config as environment variable dictionary.
    /// </summary>
    public Dictionary<string, string> AsEnvironmentVariables()
    {
        var env = new Dictionary<string, string>
        {
            ["PORT"] = Port.ToString(),
            ["CACHE_ENABLED"] = CacheEnabled ? "true" : "false",
            ["CACHE_TTL"] = CacheTTL.ToString(),
            ["DEBUG_MODE"] = DebugMode ? "true" : "false",
            ["CLAUDE_INJECT"] = ClaudeInject ? "true" : "false",
            ["MAX_RETRIES"] = MaxRetries.ToString(),
            ["CB_MAX_FAILURES"] = CbMaxFailures.ToString(),
            ["CB_RESET_TIMEOUT_SECS"] = CbResetTimeoutSecs.ToString(),
            ["COOLDOWN_DURATION_SECS"] = CooldownDurationSecs.ToString(),
            ["TELEMETRY_ENABLED"] = TelemetryEnabled ? "true" : "false",
            ["IMAGE_AUTO_SWITCH_MODEL"] = ImageAutoSwitchModel ? "true" : "false",
            ["VISION_MODEL"] = VisionModel,
            ["MAX_CONCURRENT_REQUESTS"] = MaxConcurrentRequests.ToString(),
            ["UPSTREAM_MAX_CONNS_PER_HOST"] = UpstreamMaxConnsPerHost.ToString(),
            ["LOCALE"] = Locale,
            ["LOG_MAX_SIZE_MB"] = LogMaxSizeMB.ToString(),
            ["LOG_CLEANUP_INTERVAL"] = LogCleanupInterval.ToString(),
        };
        if (!string.IsNullOrEmpty(ApiPassword))
            env["API_PASSWORD"] = ApiPassword;
        return env;
    }

    // ═══ Hot Reload via FileSystemWatcher ═══

    private void SetupWatcher()
    {
        try
        {
            Directory.CreateDirectory(ConfigDir);
            _watcher = new FileSystemWatcher(ConfigDir)
            {
                Filter = "config.json",
                NotifyFilter = NotifyFilters.LastWrite | NotifyFilters.Size,
                EnableRaisingEvents = true,
            };
            _watcher.Changed += (_, _) =>
            {
                try { Load(); }
                catch { /* ignore */ }
            };
        }
        catch
        {
            // Ignore watcher setup errors
        }
    }

    public void Dispose()
    {
        if (_disposed) return;
        _disposed = true;
        _watcher?.Dispose();
    }

    // ═══ Serialization DTO ═══

    private sealed class ConfigData
    {
        public int? Port { get; set; }
        public string? ApiPassword { get; set; }
        public bool? CacheEnabled { get; set; }
        public int? CacheTTL { get; set; }
        public bool? DebugMode { get; set; }
        public bool? ClaudeInject { get; set; }
        public int? MaxRetries { get; set; }
        public int? CbMaxFailures { get; set; }
        public int? CbResetTimeoutSecs { get; set; }
        public int? CooldownDurationSecs { get; set; }
        public bool? TelemetryEnabled { get; set; }
        public bool? ImageAutoSwitchModel { get; set; }
        public string? VisionModel { get; set; }
        public int? MaxConcurrentRequests { get; set; }
        public int? UpstreamMaxConnsPerHost { get; set; }
        public string? Locale { get; set; }
        public int? LogMaxSizeMB { get; set; }
        public int? LogCleanupInterval { get; set; }
    }
}

/// <summary>
/// Application-wide constants matching the macOS Constants.swift.
/// </summary>
public static class Constants
{
    public static class Upstream
    {
        public const string BaseURL = "https://unvcoding.copilot.qq.com";
        public const string Domain = "unvcoding.copilot.qq.com";
        public const string ProductVersion = "2.92.0";
        public static readonly string UserAgent = $"CLI/{ProductVersion} CodeBuddy/{ProductVersion}";

        public const string AuthStateURL = "/v2/plugin/auth/state";
        public const string AuthTokenURL = "/v2/plugin/auth/token";
        public const string TokenRefreshURL = "/v2/plugin/auth/token/refresh";
        public const string ChatURL = "/v2/chat/completions";
        public const string CompletionURL = "/v2/completions";
        public const string EmbeddingURL = "/v2/embeddings";
        public const string ConfigURL = "/v2/config";
        public const string ReportURL = "/v2/report";
    }

    public static class Defaults
    {
        public const int Port = 1026;
        public const int MaxConcurrentRequests = 20;
        public const int UpstreamMaxConnsPerHost = 50;
        public const int MaxRetries = 3;
        public const int CacheTTL = 300;
        public const bool CacheEnabled = false;
        public const bool DebugMode = false;
        public const bool ClaudeInject = false;
        public const bool TelemetryEnabled = true;
        public const bool ImageAutoSwitchModel = true;
        public const string VisionModel = "glm-4.6v";
        public const int CbMaxFailures = 5;
        public const int CbResetTimeoutSecs = 30;
        public const int CooldownDurationSecs = 30;
        public const int LogMaxSizeMB = 50;
        public const int LogCleanupInterval = 1800;
        public const int MaxBodySizeMB = 50;
        public const int IdleTimeoutSecs = 120;
        public const int ModelsCacheTTL = 300;
        public const int ResponseHeaderTimeoutSecs = 1800;
    }

    public static class AppMeta
    {
        public const string Name = "CodeBuddy Proxy";
        public const string BundleId = "com.uniview.codebuddy-proxy";
    }

    /// <summary>
    /// Extra models to expose that the upstream may not return.
    /// </summary>
    public static readonly (string Name, string OwnedBy)[] ExtraModels =
    [
        ("glm-5.1", "Zhipu"),
        ("glm-5.0", "Zhipu"),
        ("glm-4.7", "Zhipu"),
        ("glm-4.6", "Zhipu"),
        ("minimax-m2.7", "MiniMax"),
        ("minimax-m2.5", "MiniMax"),
        ("kimi-k2.5", "Moonshot"),
        ("deepseek-r1", "DeepSeek"),
        ("deepseek-v3-1-lkeap", "DeepSeek"),
        ("hunyuan-2.0-instruct", "Tencent"),
    ];

    /// <summary>
    /// Infer the provider name from a model name prefix.
    /// </summary>
    public static string InferOwnedBy(string model)
    {
        if (model.StartsWith("glm")) return "Zhipu";
        if (model.StartsWith("minimax")) return "MiniMax";
        if (model.StartsWith("kimi")) return "Moonshot";
        if (model.StartsWith("deepseek")) return "DeepSeek";
        if (model.StartsWith("hunyuan")) return "Tencent";
        if (model.StartsWith("claude")) return "Anthropic";
        if (model.StartsWith("gpt")) return "OpenAI";
        if (model.StartsWith("gemini")) return "Google";
        if (model.StartsWith("codebuddy")) return "Tencent";
        return "Unknown";
    }

    /// <summary>
    /// Get the context window size for a model.
    /// </summary>
    public static int GetModelContextWindow(string model)
    {
        var contextWindows = new Dictionary<string, int>
        {
            ["glm-5.1"] = 128000, ["glm-5.0"] = 128000,
            ["glm-4.7"] = 128000, ["glm-4.6"] = 128000,
            ["deepseek-r1"] = 65536, ["deepseek-v3"] = 65536,
            ["deepseek-v3-1-lkeap"] = 65536,
            ["minimax-m2.7"] = 256000, ["minimax-m2.5"] = 256000,
            ["kimi-k2.5"] = 128000,
            ["hunyuan-2.0-instruct"] = 32768,
        };

        if (contextWindows.TryGetValue(model, out var exact))
            return exact;

        foreach (var kv in contextWindows)
        {
            if (model.StartsWith(kv.Key))
                return kv.Value;
        }

        return 200000;
    }

    /// <summary>
    /// Context length limit patterns for detection.
    /// </summary>
    public static readonly string[] ContextLimitPatterns =
    [
        "context length", "prompt is too long", "maximum context length",
        "exceeds the maximum", "too many tokens", "reduce the length",
        "token limit", "context window", "max_tokens", "input is too long",
    ];
}
