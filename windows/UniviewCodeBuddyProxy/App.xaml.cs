using System;
using Microsoft.UI.Xaml;
using UniviewCodeBuddyProxy.Helpers;
using UniviewCodeBuddyProxy.Services;
using H.NotifyIcon;

namespace UniviewCodeBuddyProxy;

/// <summary>
/// Application entry point — starts ASP.NET Core Minimal API server, initializes services,
/// creates main window, and manages system tray icon.
/// </summary>
public partial class App : Application
{
    private Window? _window;
    private TaskbarIcon? _taskbarIcon;
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
        _window.Closed += OnWindowClosed;

        // Setup system tray
        SetupTaskbarIcon();

        // Start the proxy server
        StartProxyServer();
    }

    /// <summary>
    /// Hides the main window instead of closing it when the user clicks the X button.
    /// </summary>
    private void OnWindowClosed(object sender, WindowEventArgs e)
    {
        if (_taskbarIcon != null)
        {
            e.Handled = true;
            _window!.AppWindow.Hide();
        }
    }

    /// <summary>
    /// Shows the main window and brings it to the foreground.
    /// </summary>
    public void ShowWindow()
    {
        if (_window != null)
        {
            _window.AppWindow.Show();
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
        _taskbarIcon?.Dispose();
        _window?.Close();
        Environment.Exit(0);
    }

    private void SetupTaskbarIcon()
    {
        _taskbarIcon = new TaskbarIcon();
        _taskbarIcon.ToolTipText = "CodeBuddy 代理";

        _taskbarIcon.ContextMenu = new Microsoft.UI.Xaml.Controls.MenuFlyout();

        var openItem = new Microsoft.UI.Xaml.Controls.MenuFlyoutItem { Text = "打开管理面板" };
        openItem.Click += (s, e) => ShowWindow();
        _taskbarIcon.ContextMenu.Items.Add(openItem);

        _taskbarIcon.ContextMenu.Items.Add(new Microsoft.UI.Xaml.Controls.MenuFlyoutSeparator());

        var quitItem = new Microsoft.UI.Xaml.Controls.MenuFlyoutItem { Text = "退出 CodeBuddy 代理" };
        quitItem.Click += (s, e) => QuitApp();
        _taskbarIcon.ContextMenu.Items.Add(quitItem);

        _taskbarIcon.LeftClick += (s, e) => ShowWindow();
        _taskbarIcon.ForceCreate();
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
