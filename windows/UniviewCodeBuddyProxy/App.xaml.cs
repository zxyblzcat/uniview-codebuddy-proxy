using System;
using System.IO;
using System.Threading.Tasks;
using Microsoft.UI.Xaml;
using UniviewCodeBuddyProxy.Helpers;
using UniviewCodeBuddyProxy.Services;

namespace UniviewCodeBuddyProxy;

/// <summary>
/// Application entry point — starts ASP.NET Core Minimal API server, initializes services,
/// and creates main window.
/// </summary>
public partial class App : Application
{
    private Window? _window;
    private ProxyServer? _proxyServer;

    // Core services
    public ThemeManager ThemeManager { get; } = new();
    public ConfigManager ConfigManager { get; private set; } = null!;
    public LogBuffer LogBuffer { get; private set; } = null!;
    public TokenManager TokenManager { get; private set; } = null!;
    public AuthService AuthService { get; private set; } = null!;
    public TelemetryReporter TelemetryReporter { get; private set; } = null!;
    public UsageStats UsageStats { get; private set; } = null!;

    /// <summary>
    /// Path to the crash log file, written when an unhandled exception occurs.
    /// Located next to the exe for easy discovery.
    /// </summary>
    private static readonly string CrashLogPath = Path.Combine(
        AppContext.BaseDirectory, "crash.log");

    public App()
    {
        // Global unhandled exception handlers — write crash log before the process dies
        AppDomain.CurrentDomain.UnhandledException += OnUnhandledException;
        TaskScheduler.UnobservedTaskException += OnUnobservedTaskException;

        InitializeComponent();
    }

    protected override void OnLaunched(LaunchActivatedEventArgs args)
    {
        try
        {
            // Initialize core services
            ConfigManager = new ConfigManager();
            LogBuffer = new LogBuffer();
            TokenManager = new TokenManager();
            AuthService = new AuthService();
            TelemetryReporter = new TelemetryReporter(ConfigManager, TokenManager);
            UsageStats = new UsageStats();

            // Create main window
            _window = new MainWindow
            {
                ThemeManager = ThemeManager
            };

            // Start the proxy server
            StartProxyServer();
        }
        catch (Exception ex)
        {
            WriteCrashLog("OnLaunched", ex);
            // Re-throw so the process exits with a clear error instead of hanging invisibly
            throw;
        }
    }

    /// <summary>
    /// Shows the main window and brings it to the foreground.
    /// </summary>
    public void ShowWindow()
    {
        if (_window != null)
        {
            _window.Activate();
        }
    }

    /// <summary>
    /// Shuts down the proxy server and exits the application.
    /// </summary>
    public void QuitApp()
    {
        _proxyServer?.Stop();
        UsageStats?.Dispose();
        TelemetryReporter?.Shutdown();
        TokenManager?.Dispose();
        LogBuffer?.Dispose();
        ConfigManager?.Dispose();
        _window?.Close();
        Environment.Exit(0);
    }

    private void StartProxyServer()
    {
        _proxyServer = new ProxyServer(
            ConfigManager,
            TokenManager,
            AuthService,
            LogBuffer,
            TelemetryReporter,
            UsageStats);
        _proxyServer.Start();
    }

    // ── Unhandled exception handlers ──

    private static void OnUnhandledException(object sender, UnhandledExceptionEventArgs e)
    {
        var exception = e.ExceptionObject as Exception;
        WriteCrashLog("UnhandledException", exception);
    }

    private static void OnUnobservedTaskException(object? sender, UnobservedTaskExceptionEventArgs e)
    {
        WriteCrashLog("UnobservedTaskException", e.Exception);
        e.SetObserved(); // Prevent process crash from unobserved task
    }

    /// <summary>
    /// Writes a crash log to the file next to the exe.
    /// Includes timestamp, exception type, message, and full stack trace.
    /// </summary>
    private static void WriteCrashLog(string source, Exception? exception)
    {
        try
        {
            var timestamp = DateTime.Now.ToString("yyyy-MM-dd HH:mm:ss.fff");
            var entry = $"[{timestamp}] [{source}] {exception?.GetType().FullName}: {exception?.Message}\n" +
                        $"Stack Trace:\n{exception?.StackTrace}\n" +
                        (exception?.InnerException != null
                            ? $"\nInner Exception:\n  {exception.InnerException.GetType().FullName}: {exception.InnerException.Message}\n  {exception.InnerException.StackTrace}\n"
                            : "") +
                        "\n---\n";

            File.AppendAllText(CrashLogPath, entry);
        }
        catch
        {
            // If we can't write the crash log, there's nothing more we can do
        }
    }
}
