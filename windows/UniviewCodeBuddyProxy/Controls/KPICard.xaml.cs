using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media;

namespace UniviewCodeBuddyProxy.Controls;

/// <summary>
/// KPI display card with title, large value, subtitle, trend arrow, and accent line.
/// Mirrors macOS KPICard.
/// </summary>
public sealed partial class KPICard : UserControl
{
    public KPICard()
    {
        this.InitializeComponent();
    }

    // ── Title ──

    public static readonly DependencyProperty TitleProperty =
        DependencyProperty.Register(nameof(Title), typeof(string), typeof(KPICard),
            new PropertyMetadata(string.Empty, (d, e) => ((KPICard)d).TitleText.Text = (string)e.NewValue));

    public string Title
    {
        get => (string)GetValue(TitleProperty);
        set => SetValue(TitleProperty, value);
    }

    // ── Value ──

    public static readonly DependencyProperty ValueProperty =
        DependencyProperty.Register(nameof(Value), typeof(object), typeof(KPICard),
            new PropertyMetadata(null, (d, e) => ((KPICard)d).ValueText.Text = e.NewValue?.ToString() ?? string.Empty));

    public object Value
    {
        get => GetValue(ValueProperty);
        set => SetValue(ValueProperty, value);
    }

    // ── Subtitle ──

    public static readonly DependencyProperty SubtitleProperty =
        DependencyProperty.Register(nameof(Subtitle), typeof(string), typeof(KPICard),
            new PropertyMetadata(null, OnSubtitleChanged));

    public string Subtitle
    {
        get => (string)GetValue(SubtitleProperty);
        set => SetValue(SubtitleProperty, value);
    }

    private static void OnSubtitleChanged(DependencyObject d, DependencyPropertyChangedEventArgs e)
    {
        var card = (KPICard)d;
        var hasSubtitle = !string.IsNullOrEmpty(e.NewValue as string);
        card.SubtitlePanel.Visibility = hasSubtitle ? Visibility.Visible : Visibility.Collapsed;
        card.SubtitleText.Text = e.NewValue as string ?? string.Empty;
    }

    // ── Trend ──

    public static readonly DependencyProperty TrendProperty =
        DependencyProperty.Register(nameof(Trend), typeof(KPITrend), typeof(KPICard),
            new PropertyMetadata(KPITrend.Neutral, OnTrendChanged));

    public KPITrend Trend
    {
        get => (KPITrend)GetValue(TrendProperty);
        set => SetValue(TrendProperty, value);
    }

    private static void OnTrendChanged(DependencyObject d, DependencyPropertyChangedEventArgs e)
    {
        var card = (KPICard)d;
        var trend = (KPITrend)e.NewValue;

        card.TrendIcon.Glyph = trend switch
        {
            KPITrend.Up => "",     // ArrowUpRight
            KPITrend.Down => "",   // ArrowDownRight
            _ => ""               // ArrowRight
        };

        var color = trend switch
        {
            KPITrend.Up => ColorHelper.ToHex(ThemeColors.Success),
            KPITrend.Down => ColorHelper.ToHex(ThemeColors.Danger),
            _ => null
        };

        if (color != null)
        {
            card.TrendIcon.Foreground = new SolidColorBrush(Helpers.ColorHelper.FromHex(color));
            card.SubtitleText.Foreground = new SolidColorBrush(Helpers.ColorHelper.FromHex(color));
        }
        else
        {
            card.TrendIcon.Foreground = (Brush)card.Resources["TextFillColorSecondaryBrush"]
                ?? new SolidColorBrush(Microsoft.UI.Colors.Gray);
            card.SubtitleText.Foreground = card.TrendIcon.Foreground;
        }
    }

    // ── AccentColor ──

    public static readonly DependencyProperty AccentColorProperty =
        DependencyProperty.Register(nameof(AccentColor), typeof(Brush), typeof(KPICard),
            new PropertyMetadata(null, OnAccentColorChanged));

    public Brush AccentColor
    {
        get => (Brush)GetValue(AccentColorProperty);
        set => SetValue(AccentColorProperty, value);
    }

    private static void OnAccentColorChanged(DependencyObject d, DependencyPropertyChangedEventArgs e)
    {
        var card = (KPICard)d;
        var brush = e.NewValue as SolidColorBrush;
        if (brush == null)
        {
            card.AccentLine.Background = new SolidColorBrush(Microsoft.UI.Colors.Transparent);
            return;
        }
        var color = brush.Color;
        var gradient = new LinearGradientBrush
        {
            StartPoint = new Point(0, 0.5),
            EndPoint = new Point(1, 0.5),
            GradientStops = new GradientStopCollection
            {
                new() { Color = color, Offset = 0 },
                new() { Color = color.WithOpacity(0.2), Offset = 1 }
            }
        };
        card.AccentLine.Background = gradient;
    }
}

/// <summary>
/// KPI trend direction.
/// </summary>
public enum KPITrend
{
    Up,
    Down,
    Neutral
}
