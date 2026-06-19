using Cb = UniviewCodeBuddyProxy.Services.Constants;

namespace UniviewCodeBuddyProxy.Helpers;

/// <summary>
/// Convenience aliases that redirect to Services.Constants so Views and ViewModels
/// can write <c>Upstream.BaseURL</c> instead of <c>Constants.Upstream.BaseURL</c>.
/// The authoritative definitions live in Services/ConfigManager.cs.
/// </summary>
public static class Upstream
{
    public const string BaseURL = Cb.Upstream.BaseURL;
    public const string Domain = Cb.Upstream.Domain;
    public const string ProductVersion = Cb.Upstream.ProductVersion;
    public static readonly string UserAgent = Cb.Upstream.UserAgent;

    public const string AuthStateURL = Cb.Upstream.AuthStateURL;
    public const string AuthTokenURL = Cb.Upstream.AuthTokenURL;
    public const string TokenRefreshURL = Cb.Upstream.TokenRefreshURL;
    public const string ChatURL = Cb.Upstream.ChatURL;
    public const string CompletionURL = Cb.Upstream.CompletionURL;
    public const string EmbeddingURL = Cb.Upstream.EmbeddingURL;
    public const string ConfigURL = Cb.Upstream.ConfigURL;
    public const string ReportURL = Cb.Upstream.ReportURL;
}

public static class Defaults
{
    public const int Port = Cb.Defaults.Port;
    public const int MaxConcurrentRequests = Cb.Defaults.MaxConcurrentRequests;
    public const int UpstreamMaxConnsPerHost = Cb.Defaults.UpstreamMaxConnsPerHost;
    public const int MaxRetries = Cb.Defaults.MaxRetries;
    public const int CacheTTL = Cb.Defaults.CacheTTL;
    public const bool CacheEnabled = Cb.Defaults.CacheEnabled;
    public const bool DebugMode = Cb.Defaults.DebugMode;
    public const bool ClaudeInject = Cb.Defaults.ClaudeInject;
    public const bool TelemetryEnabled = Cb.Defaults.TelemetryEnabled;
    public const bool ImageAutoSwitchModel = Cb.Defaults.ImageAutoSwitchModel;
    public const string VisionModel = Cb.Defaults.VisionModel;
    public const int CbMaxFailures = Cb.Defaults.CbMaxFailures;
    public const int CbResetTimeoutSecs = Cb.Defaults.CbResetTimeoutSecs;
    public const int CooldownDurationSecs = Cb.Defaults.CooldownDurationSecs;
    public const int LogMaxSizeMB = Cb.Defaults.LogMaxSizeMB;
    public const int LogCleanupInterval = Cb.Defaults.LogCleanupInterval;
    public const int MaxBodySizeMB = Cb.Defaults.MaxBodySizeMB;
    public const int IdleTimeoutSecs = Cb.Defaults.IdleTimeoutSecs;
    public const int ModelsCacheTTL = Cb.Defaults.ModelsCacheTTL;
    public const int ResponseHeaderTimeoutSecs = Cb.Defaults.ResponseHeaderTimeoutSecs;
}

public static class AppMeta
{
    public const string Name = Cb.AppMeta.Name;
    public const string BundleId = Cb.AppMeta.BundleId;
}

/// <summary>
/// Model name to provider inference — delegates to Services.Constants.InferOwnedBy.
/// </summary>
public static class ModelOwnership
{
    public static string InferOwnedBy(string model) => Cb.InferOwnedBy(model);
}
