using System;
using System.Collections.Generic;
using System.Linq;
using Microsoft.UI;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media;
using UniviewCodeBuddyProxy.Helpers;

namespace UniviewCodeBuddyProxy.Controls;

/// <summary>
/// An option in the GlassSegmentedPicker.
/// </summary>
public sealed class GlassSegmentedOption
{
    public string Label { get; init; } = "";
    public string Value { get; init; } = "";
}

/// <summary>
/// Pill-shaped segmented control, matching macOS GlassSegmentedPicker.
/// </summary>
public sealed partial class GlassSegmentedPicker : UserControl
{
    private IList<GlassSegmentedOption>? _options;

    public GlassSegmentedPicker()
    {
        this.InitializeComponent();
    }

    // ── Dependency Properties ──

    public static readonly DependencyProperty OptionsProperty =
        DependencyProperty.Register(nameof(Options), typeof(IList<GlassSegmentedOption>), typeof(GlassSegmentedPicker),
            new PropertyMetadata(null, OnOptionsChanged));

    public IList<GlassSegmentedOption> Options
    {
        get => (IList<GlassSegmentedOption>)GetValue(OptionsProperty);
        set => SetValue(OptionsProperty, value);
    }

    public static readonly DependencyProperty SelectedValueProperty =
        DependencyProperty.Register(nameof(SelectedValue), typeof(string), typeof(GlassSegmentedPicker),
            new PropertyMetadata(string.Empty, OnSelectedValueChanged));

    public string SelectedValue
    {
        get => (string)GetValue(SelectedValueProperty);
        set => SetValue(SelectedValueProperty, value);
    }

    private static void OnOptionsChanged(DependencyObject d, DependencyPropertyChangedEventArgs e)
    {
        var picker = (GlassSegmentedPicker)d;
        picker._options = e.NewValue as IList<GlassSegmentedOption>;
        picker.BuildSegments();
    }

    private static void OnSelectedValueChanged(DependencyObject d, DependencyPropertyChangedEventArgs e)
    {
        var picker = (GlassSegmentedPicker)d;
        picker.UpdateSegmentStyles();
    }

    private void BuildSegments()
    {
        var app = (App)Application.Current;
        var colors = app.ThemeManager.Colors;

        SegmentPanel.Children.Clear();
        if (_options == null) return;

        for (int i = 0; i < _options.Count; i++)
        {
            var option = _options[i];
            var btn = new Button
            {
                Content = option.Label,
                Tag = option.Value,
                CornerRadius = new CornerRadius(999),
                Padding = new Thickness(14, 6, 14, 6),
                FontSize = 12,
                FontWeight = Microsoft.UI.Text.FontWeights.Medium,
                BorderThickness = new Thickness(0),
                Background = new SolidColorBrush(Colors.Transparent),
                Foreground = new SolidColorBrush(colors.TextSecondary),
            };
            btn.Click += OnSegmentClick;

            SegmentPanel.Children.Add(btn);
        }

        UpdateSegmentStyles();
    }

    private void OnSegmentClick(object sender, RoutedEventArgs e)
    {
        if (sender is Button btn && btn.Tag is string value)
        {
            SelectedValue = value;
        }
    }

    private void UpdateSegmentStyles()
    {
        var app = (App)Application.Current;
        var colors = app.ThemeManager.Colors;

        foreach (var child in SegmentPanel.Children)
        {
            if (child is not Button btn || btn.Tag is not string value) continue;

            var isSelected = value == SelectedValue;
            btn.Background = isSelected
                ? new SolidColorBrush(colors.Primary)
                : new SolidColorBrush(Colors.Transparent);
            btn.Foreground = isSelected
                ? new SolidColorBrush(colors.Seed.Bg)
                : new SolidColorBrush(colors.TextSecondary);
        }
    }
}
