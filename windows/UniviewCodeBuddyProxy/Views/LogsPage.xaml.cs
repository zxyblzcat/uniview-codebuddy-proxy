using System;
using System.Collections.Generic;
using System.Linq;
using UniviewCodeBuddyProxy.Helpers;
using UniviewCodeBuddyProxy.Models;
using UniviewCodeBuddyProxy.Services;
using UniviewCodeBuddyProxy.ViewModels;
using Microsoft.UI.Dispatching;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media;

namespace UniviewCodeBuddyProxy.Views;

/// <summary>
/// Log viewer page — real-time log entries with level filtering and search.
/// Mirrors macOS LogsView.
/// </summary>
public sealed partial class LogsPage : Page
{
    public LogsViewModel ViewModel { get; }

    private readonly LogBuffer? _logBuffer;
    private DispatcherQueue? _dispatcherQueue;

    public LogsPage()
    {
        var app = (App)Application.Current;
        _logBuffer = app.LogBuffer;
        ViewModel = new LogsViewModel(_logBuffer);
        this.InitializeComponent();

        // Capture dispatcher queue in constructor since Page is created on UI thread
        _dispatcherQueue = DispatcherQueue.GetForCurrentThread();

        if (_logBuffer != null)
        {
            _logBuffer.EntryAppended += OnLogEntryAppended;
        }
    }

    private void OnLogEntryAppended(Services.LogEntry entry)
    {
        _dispatcherQueue?.TryEnqueue(() =>
        {
            var display = LogEntryDisplay.FromLogEntry(entry);
            ViewModel.AddEntry(display);
        });
    }

    private void OnLevelFilterChanged(object sender, RoutedEventArgs e)
    {
        if (sender is RadioButton rb && rb.Tag is string level)
        {
            ViewModel.SelectedLevel = level switch
            {
                "Debug" => LogLevel.Debug,
                "Info" => LogLevel.Info,
                "Warn" => LogLevel.Warn,
                "Error" => LogLevel.Error,
                _ => null // "All"
            };
        }
    }

    private async void OnClearLogs(object sender, RoutedEventArgs e)
    {
        var dialog = new ContentDialog
        {
            Title = "清除日志",
            Content = "确定要清除所有日志条目吗？",
            PrimaryButtonText = "清除",
            CloseButtonText = "取消",
            DefaultButton = ContentDialogButton.Close,
            XamlRoot = this.XamlRoot
        };

        var result = await dialog.ShowAsync();
        if (result == ContentDialogResult.Primary)
        {
            ViewModel.ClearLogs();
            _logBuffer?.Clear();
        }
    }
}

/// <summary>
/// LogEntry display helpers for level-based coloring in the ListView template.
/// These attached properties allow the DataTemplate to bind level-specific brushes.
/// </summary>
public static class LogEntryExtensions
{
    // ── Level background brush ──

    public static readonly DependencyProperty LevelBgBrushProperty =
        DependencyProperty.RegisterAttached("LevelBgBrush", typeof(Brush), typeof(LogEntryExtensions),
            new PropertyMetadata(null));

    public static Brush GetLevelBgBrush(DependencyObject obj) => (Brush)obj.GetValue(LevelBgBrushProperty);
    public static void SetLevelBgBrush(DependencyObject obj, Brush value) => obj.SetValue(LevelBgBrushProperty, value);

    // ── Level badge brush ──

    public static readonly DependencyProperty LevelBadgeBrushProperty =
        DependencyProperty.RegisterAttached("LevelBadgeBrush", typeof(Brush), typeof(LogEntryExtensions),
            new PropertyMetadata(null));

    public static Brush GetLevelBadgeBrush(DependencyObject obj) => (Brush)obj.GetValue(LevelBadgeBrushProperty);
    public static void SetLevelBadgeBrush(DependencyObject obj, Brush value) => obj.SetValue(LevelBadgeBrushProperty, value);

    /// <summary>
    /// Returns a background brush for the log row based on level.
    /// </summary>
    public static Brush GetRowBackground(LogLevel level) => level switch
    {
        LogLevel.Error => ThemeColors.DangerSubtle.ToBrush(),
        LogLevel.Warn => ThemeColors.WarningSubtle.ToBrush(),
        LogLevel.Debug => ThemeColors.InfoSubtle.ToBrush(),
        _ => new SolidColorBrush(Microsoft.UI.Colors.Transparent)
    };

    /// <summary>
    /// Returns a badge background brush for the level tag.
    /// </summary>
    public static Brush GetBadgeBrush(LogLevel level) => level switch
    {
        LogLevel.Error => ThemeColors.Danger.ToBrush(),
        LogLevel.Warn => ThemeColors.Warning.ToBrush(),
        LogLevel.Info => ThemeColors.Info.ToBrush(),
        LogLevel.Debug => ThemeColors.Purple.ToBrush(),
        _ => ColorHelper.FromHex("#888888").ToBrush()
    };
}
