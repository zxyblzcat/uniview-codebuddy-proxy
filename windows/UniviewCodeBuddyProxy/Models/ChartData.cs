using System;
using Microsoft.UI.Xaml.Media;
using UniviewCodeBuddyProxy.Helpers;

namespace UniviewCodeBuddyProxy.Models;

/// <summary>
/// Hourly request data point for the bar chart.
/// </summary>
public sealed class HourlyRequest
{
    public string Hour { get; init; } = string.Empty;
    public int Count { get; init; }
}

/// <summary>
/// Model usage data point for the donut chart.
/// </summary>
public sealed class ModelUsageItem
{
    public string Name { get; init; } = string.Empty;
    public double Ratio { get; init; }
    public string Color { get; init; } = "#5B9CF6";

    /// <summary>
    /// Returns a Brush for the model color, for XAML data binding.
    /// </summary>
    public Brush ColorBrush => ColorHelper.FromHex(Color).ToBrush();

    /// <summary>
    /// Percentage display string (e.g. "35%").
    /// </summary>
    public string PercentDisplay => $"{Ratio * 100:F0}%";
}
