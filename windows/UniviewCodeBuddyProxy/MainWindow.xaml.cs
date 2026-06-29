using Microsoft.UI;
using Microsoft.UI.Windowing;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media;
using Microsoft.UI.Xaml.Navigation;
using UniviewCodeBuddyProxy.Helpers;
using UniviewCodeBuddyProxy.Views;

namespace UniviewCodeBuddyProxy;

/// <summary>
/// Main application window with NavigationView, glass-morphism background,
/// and top status bar. Default size 1200x800.
/// </summary>
public sealed partial class MainWindow : Window
{
    private ThemeManager _themeManager = new();
    private readonly Helpers.ToastManager _toastManager = new();
    private readonly Windows.UI.ViewManagement.UISettings _uiSettings = new();

    public ThemeManager ThemeManager
    {
        get => _themeManager;
        set
        {
            _themeManager = value;
            ApplyTheme();
        }
    }

    public MainWindow()
    {
        InitializeComponent();

        // Set default size
        var appWindow = this.AppWindow;
        appWindow.Resize(new Windows.Graphics.SizeInt32(1200, 800));

        // Center the window on screen
        var displayArea = DisplayArea.GetFromWindowId(appWindow.Id, DisplayAreaFallback.Primary);
        if (displayArea != null)
        {
            var centerX = (displayArea.WorkArea.Width - 1200) / 2;
            var centerY = (displayArea.WorkArea.Height - 800) / 2;
            appWindow.Move(new Windows.Graphics.PointInt32(centerX, centerY));
        }

        // Set title
        Title = "CodeBuddy 代理";

        // Detect initial system theme
        var backgroundColor = _uiSettings.GetColorValue(Windows.UI.ViewManagement.UIColorType.Background);
        _themeManager.UpdateSystemTheme(backgroundColor == Windows.UI.Colors.Black || backgroundColor.R < 128);

        // Apply initial theme
        ApplyTheme();

        // Wire theme changes
        _themeManager.PropertyChanged += OnThemeChanged;

        // Wire system theme changes
        _uiSettings.ColorValuesChanged += OnSystemThemeChanged;

        // Wire toast manager
        _toastManager.PropertyChanged += OnToastChanged;

        // Set default page
        NavigateToPage("Dashboard");
    }

    private void ApplyTheme()
    {
        var colors = _themeManager.Colors;

        // Apply ElementTheme to root so {ThemeResource} brushes resolve correctly
        if (RootGrid != null)
        {
            RootGrid.RequestedTheme = _themeManager.EffectiveElementTheme;
        }

        // Update gradient background layers
        // colors.Bg etc. return Microsoft.UI.Color; implicit conversion to Windows.UI.Color
        // handles GradientStop.Color and AcrylicBrush properties.
        BgBaseBrush.Color = colors.Bg;
        PrimaryGlowStop.Color = colors.Primary.WithOpacity(0.08);
        AccentGlowStop.Color = colors.Accent.WithOpacity(0.05);
        DepthGlowStop.Color = colors.Primary.WithOpacity(0.04);

        // Apply AcrylicBrush to the content area for glass-morphism
        var acrylicBrush = new AcrylicBrush
        {
            BackgroundSource = Microsoft.UI.Xaml.Media.AcrylicBackgroundSource.HostBackdrop,
            TintColor = colors.Bg,
            TintOpacity = 0.78,
            FallbackColor = colors.Bg,
        };
        ContentArea.Background = acrylicBrush;

        UpdateStatusBar();
    }

    private void OnThemeChanged(object? sender, System.ComponentModel.PropertyChangedEventArgs e)
    {
        if (e.PropertyName == nameof(ThemeManager.Colors) ||
            e.PropertyName == nameof(ThemeManager.EffectiveElementTheme))
        {
            ApplyTheme();
        }
    }

    private void OnSystemThemeChanged(Windows.UI.ViewManagement.UISettings sender, object args)
    {
        // UISettings.ColorValuesChanged fires on a background thread
        DispatcherQueue.TryEnqueue(() =>
        {
            var backgroundColor = sender.GetColorValue(Windows.UI.ViewManagement.UIColorType.Background);
            var systemIsDark = backgroundColor == Windows.UI.Colors.Black || backgroundColor.R < 128;
            _themeManager.UpdateSystemTheme(systemIsDark);
        });
    }

    private void OnToastChanged(object? sender, System.ComponentModel.PropertyChangedEventArgs e)
    {
        if (e.PropertyName == nameof(Helpers.ToastManager.IsVisible))
        {
            AppToast.Visibility = _toastManager.IsVisible ? Visibility.Visible : Visibility.Collapsed;
        }
        if (e.PropertyName == nameof(Helpers.ToastManager.Message))
        {
            AppToast.Message = _toastManager.Message;
        }
        if (e.PropertyName == nameof(Helpers.ToastManager.Kind))
        {
            AppToast.Kind = _toastManager.Kind;
        }
    }

    private void UpdateStatusBar()
    {
        var colors = _themeManager.Colors;
        ConnectionIndicator.Fill = new SolidColorBrush(colors.Success);
        ProxyStatusText.Foreground = new SolidColorBrush(colors.Text);
        ModelCountText.Foreground = new SolidColorBrush(colors.TextSecondary);
        StatusText.Foreground = new SolidColorBrush(colors.TextSecondary);
    }

    private void NavView_SelectionChanged(NavigationView sender, NavigationViewSelectionChangedEventArgs args)
    {
        if (args.SelectedItem is NavigationViewItem item)
        {
            var tag = item.Tag?.ToString() ?? "Dashboard";
            NavigateToPage(tag);
        }
    }

    private void NavigateToPage(string tag)
    {
        StatusText.Text = tag switch
        {
            "Dashboard" => "仪表盘",
            "Models" => "模型管理",
            "Tokens" => "令牌管理",
            "Logs" => "日志",
            "Settings" => "设置",
            _ => tag
        };

        Type pageType = tag switch
        {
            "Dashboard" => typeof(DashboardPage),
            "Models" => typeof(ModelsPage),
            "Tokens" => typeof(TokensPage),
            "Logs" => typeof(LogsPage),
            "Settings" => typeof(SettingsPage),
            _ => typeof(DashboardPage)
        };

        ContentFrame.Navigate(pageType);
    }

    private void OnNavigationFailed(object sender, NavigationFailedEventArgs e)
    {
        throw new Exception($"Failed to load page: {e.SourcePageType.FullName}");
    }
}
