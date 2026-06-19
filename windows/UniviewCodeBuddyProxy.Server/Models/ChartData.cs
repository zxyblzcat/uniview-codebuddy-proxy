using System;

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
}
