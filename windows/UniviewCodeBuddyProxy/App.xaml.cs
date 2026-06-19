using System;
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

    public App()
    {
        InitializeComponent();
    }

    protected override void OnLaunched(LaunchActivatedEventArgs args)
    {
        // Initialize core services
        ConfigManager = new ConfigManager();
        LogBuffer = new LogBuffer();
        TokenManager = new TokenManager();
        AuthService = new AuthService();
        TelemetryReporter = new TelemetryReporter(ConfigManager, TokenManager);

        // Create main window
        _window = new MainWindow
        {
            ThemeManager = ThemeManager
        };

        // Start the proxy server
        StartProxyServer();
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
            TelemetryReporter);
        _proxyServer.Start();
    }
}
