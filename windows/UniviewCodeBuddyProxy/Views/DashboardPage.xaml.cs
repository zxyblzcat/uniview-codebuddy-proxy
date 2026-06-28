using System;
using System.Collections.Generic;
using System.Linq;
using UniviewCodeBuddyProxy.Controls;
using UniviewCodeBuddyProxy.Helpers;
using UniviewCodeBuddyProxy.Models;
using UniviewCodeBuddyProxy.Services;
using UniviewCodeBuddyProxy.ViewModels;
using Microsoft.UI.Dispatching;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace UniviewCodeBuddyProxy.Views;

/// <summary>
/// Dashboard page — KPI cards, bar chart, donut chart, recent activity.
/// Mirrors macOS DashboardView.
/// </summary>
public sealed partial class DashboardPage : Page
{
    public DashboardViewModel ViewModel { get; }
    private DispatcherQueue? _dispatcherQueue;

    public DashboardPage()
    {
        var app = (App)Application.Current;
        ViewModel = new DashboardViewModel(app.TokenManager, app.LogBuffer, app.TelemetryReporter);
        this.InitializeComponent();

        // Capture dispatcher queue in constructor since Page is created on UI thread
        _dispatcherQueue = DispatcherQueue.GetForCurrentThread();

        Loaded += OnLoaded;

        // Subscribe to LogBuffer for live activity updates
        if (app.LogBuffer != null)
        {
            app.LogBuffer.EntryAppended += OnLogEntryAppended;
        }

        // Re-apply theme-dependent UI when appearance changes
        app.ThemeManager.PropertyChanged += OnThemeChanged;
    }

    private void OnThemeChanged(object? sender, System.ComponentModel.PropertyChangedEventArgs e)
    {
        if (e.PropertyName == nameof(ThemeManager.Colors))
        {
            // DonutChart segments use static model colors (don't change with theme),
            // but if future theme-dependent chart colors are added, refresh here.
            // BarChart already subscribes to ThemeManager.PropertyChanged internally.
        }
    }

    private void OnLoaded(object sender, RoutedEventArgs e)
    {
        _dispatcherQueue = DispatcherQueue.GetForCurrentThread();

        // Wire chart controls
        if (BarChartControl != null)
        {
            BarChartControl.ItemsSource = ViewModel.HourlyRequests;
        }

        if (DonutChartControl != null)
        {
            DonutChartControl.Segments = ViewModel.ModelUsage.Select(m => new DonutSegment
            {
                Name = m.Name,
                Ratio = m.Ratio,
                Color = ColorHelper.FromHex(m.Color)
            }).ToList();
            DonutChartControl.CenterText = "24.8k";
            DonutChartControl.SubLabelText = "总计";
        }
    }

    private void OnLogEntryAppended(Services.LogEntry entry)
    {
        _dispatcherQueue?.TryEnqueue(() =>
        {
            ViewModel.RefreshActivity();
        });
    }
}
