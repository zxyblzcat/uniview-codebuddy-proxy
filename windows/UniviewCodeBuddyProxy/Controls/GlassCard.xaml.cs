using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media;
using UniviewCodeBuddyProxy.Helpers;

namespace UniviewCodeBuddyProxy.Controls;

/// <summary>
/// Glass-morphism card with optional header (icon + title).
/// Mirrors macOS GlassCard.
/// </summary>
public sealed partial class GlassCard : UserControl
{
    public GlassCard()
    {
        this.InitializeComponent();
        PointerEntered += OnPointerEntered;
        PointerExited += OnPointerExited;

        // Listen for theme changes to update highlight color
        var app = (App)Application.Current;
        app.ThemeManager.PropertyChanged += OnThemePropertyChanged;
        UpdateHighlightColor();
    }

    private void OnThemePropertyChanged(object? sender, System.ComponentModel.PropertyChangedEventArgs e)
    {
        if (e.PropertyName == nameof(ThemeManager.Colors))
        {
            UpdateHighlightColor();
        }
    }

    private void UpdateHighlightColor()
    {
        var app = (App)Application.Current;
        var colors = app.ThemeManager.Colors;
        HighlightStart.Color = colors.HighlightGradientStart;
    }

    private void OnPointerEntered(object sender, Microsoft.UI.Xaml.Input.PointerRoutedEventArgs e)
    {
        VisualStateManager.GoToState(this, "PointerOver", true);
    }

    private void OnPointerExited(object sender, Microsoft.UI.Xaml.Input.PointerRoutedEventArgs e)
    {
        VisualStateManager.GoToState(this, "Normal", true);
    }

    // ── Header dependency property ──

    public static readonly DependencyProperty HeaderProperty =
        DependencyProperty.Register(
            nameof(Header),
            typeof(string),
            typeof(GlassCard),
            new PropertyMetadata(null, OnHeaderChanged));

    public string Header
    {
        get => (string)GetValue(HeaderProperty);
        set => SetValue(HeaderProperty, value);
    }

    private static void OnHeaderChanged(DependencyObject d, DependencyPropertyChangedEventArgs e)
    {
        var card = (GlassCard)d;
        var hasHeader = !string.IsNullOrEmpty(e.NewValue as string);
        card.HeaderPanel.Visibility = hasHeader ? Visibility.Visible : Visibility.Collapsed;
        card.HeaderDivider.Visibility = hasHeader ? Visibility.Visible : Visibility.Collapsed;
        card.HeaderText.Text = e.NewValue as string ?? string.Empty;
    }

    // ── Icon dependency property (Segoe MDL2 glyph) ──

    public static readonly DependencyProperty IconProperty =
        DependencyProperty.Register(
            nameof(Icon),
            typeof(string),
            typeof(GlassCard),
            new PropertyMetadata(string.Empty, OnIconChanged));

    public string Icon
    {
        get => (string)GetValue(IconProperty);
        set => SetValue(IconProperty, value);
    }

    private static void OnIconChanged(DependencyObject d, DependencyPropertyChangedEventArgs e)
    {
        var card = (GlassCard)d;
        var glyph = e.NewValue as string ?? string.Empty;
        card.HeaderIcon.Glyph = glyph;
        card.HeaderIcon.Visibility = string.IsNullOrEmpty(glyph)
            ? Visibility.Collapsed
            : Visibility.Visible;
    }

    // ── CornerRadius override ──

    public new static readonly DependencyProperty CornerRadiusProperty =
        DependencyProperty.Register(
            nameof(CornerRadius),
            typeof(double),
            typeof(GlassCard),
            new PropertyMetadata(20.0, OnCornerRadiusChanged));

    public new double CornerRadius
    {
        get => (double)GetValue(CornerRadiusProperty);
        set => SetValue(CornerRadiusProperty, value);
    }

    private static void OnCornerRadiusChanged(DependencyObject d, DependencyPropertyChangedEventArgs e)
    {
        var card = (GlassCard)d;
        var radius = (double)e.NewValue;
        card.CardBorder.CornerRadius = new CornerRadius(radius);
    }
}
