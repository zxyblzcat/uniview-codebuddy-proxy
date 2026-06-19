using System;
using System.Collections;
using System.Linq;
using Microsoft.UI;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media;
using Microsoft.UI.Xaml.Shapes;
using UniviewCodeBuddyProxy.Helpers;
using UniviewCodeBuddyProxy.Models;

namespace UniviewCodeBuddyProxy.Controls;

/// <summary>
/// Reusable bar chart control with gradient bars and labels.
/// Extracted from DashboardPage inline Canvas drawing.
/// Mirrors macOS BarChart.swift.
/// </summary>
public sealed partial class BarChart : UserControl
{
    public BarChart()
    {
        this.InitializeComponent();
        Loaded += OnLoaded;
        ChartCanvas.SizeChanged += OnSizeChanged;
    }

    // ── Dependency Properties ──

    public static readonly DependencyProperty ItemsSourceProperty =
        DependencyProperty.Register(nameof(ItemsSource), typeof(IList), typeof(BarChart),
            new PropertyMetadata(null, OnItemsSourceChanged));

    public IList ItemsSource
    {
        get => (IList)GetValue(ItemsSourceProperty);
        set => SetValue(ItemsSourceProperty, value);
    }

    public static readonly DependencyProperty BarColorProperty =
        DependencyProperty.Register(nameof(BarColor), typeof(Color), typeof(BarChart),
            new PropertyMetadata(ColorHelper.FromHex("#5B9CF6"), OnVisualPropertyChanged));

    public Color BarColor
    {
        get => (Color)GetValue(BarColorProperty);
        set => SetValue(BarColorProperty, value);
    }

    public static readonly DependencyProperty HighlightColorProperty =
        DependencyProperty.Register(nameof(HighlightColor), typeof(Color), typeof(BarChart),
            new PropertyMetadata(ColorHelper.FromHex("#5B9CF6"), OnVisualPropertyChanged));

    public Color HighlightColor
    {
        get => (Color)GetValue(HighlightColorProperty);
        set => SetValue(HighlightColorProperty, value);
    }

    private static void OnItemsSourceChanged(DependencyObject d, DependencyPropertyChangedEventArgs e)
    {
        ((BarChart)d).DrawBars();
    }

    private static void OnVisualPropertyChanged(DependencyObject d, DependencyPropertyChangedEventArgs e)
    {
        ((BarChart)d).DrawBars();
    }

    private void OnLoaded(object sender, RoutedEventArgs e) => DrawBars();
    private void OnSizeChanged(object sender, SizeChangedEventArgs e) => DrawBars();

    private void DrawBars()
    {
        var canvas = ChartCanvas;
        canvas.Children.Clear();

        if (ItemsSource == null || ItemsSource.Count == 0) return;

        // Extract values from HourlyRequest items
        var items = new (string Label, int Count)[ItemsSource.Count];
        for (int i = 0; i < ItemsSource.Count; i++)
        {
            var item = ItemsSource[i] as HourlyRequest;
            items[i] = (item?.Hour ?? "", item?.Count ?? 0);
        }

        var maxCount = items.Max(h => h.Count);
        if (maxCount == 0) maxCount = 1;

        double canvasHeight = 160;
        double canvasWidth = canvas.ActualWidth > 0 ? canvas.ActualWidth : 400;
        int barCount = items.Length;
        double barSpacing = 6;
        double barWidth = Math.Max(8, (canvasWidth - barSpacing * (barCount + 1)) / barCount);

        var primaryBrush = new SolidColorBrush(BarColor);
        var primarySubtleBrush = new SolidColorBrush(BarColor.WithOpacity(0.4));
        var textBrush = new SolidColorBrush(((App)Application.Current).ThemeManager.Colors.TextMuted);

        double x = barSpacing;
        for (int i = 0; i < barCount; i++)
        {
            var item = items[i];
            double barHeight = (double)item.Count / maxCount * canvasHeight;

            var bar = new Rectangle
            {
                Width = barWidth,
                Height = Math.Max(2, barHeight),
                RadiusX = 3,
                RadiusY = 3,
                Fill = i == barCount - 1 ? primaryBrush : primarySubtleBrush
            };

            Canvas.SetLeft(bar, x);
            Canvas.SetTop(bar, canvasHeight - barHeight);
            canvas.Children.Add(bar);

            // Hour label below
            var label = new TextBlock
            {
                Text = item.Label.Length > 5 ? item.Label[^5..] : item.Label,
                FontSize = 8,
                Foreground = textBrush
            };
            Canvas.SetLeft(label, x - 2);
            Canvas.SetTop(label, canvasHeight + 4);
            canvas.Children.Add(label);

            x += barWidth + barSpacing;
        }
    }
}

/// <summary>
/// Extension method for SolidColorBrush opacity.
/// </summary>
internal static class BrushExtensions
{
    public static SolidColorBrush WithOpacity(this SolidColorBrush brush, double opacity)
    {
        return new SolidColorBrush(brush.Color.WithOpacity(opacity));
    }
}
