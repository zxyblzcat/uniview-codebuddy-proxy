using System;
using System.Collections.ObjectModel;
using System.ComponentModel;
using System.Linq;
using System.Runtime.CompilerServices;
using UniviewCodeBuddyProxy.Models;
using UniviewCodeBuddyProxy.Services;

namespace UniviewCodeBuddyProxy.ViewModels;

/// <summary>
/// Log viewer view model — filtered entries, level filter, search, auto-scroll.
/// Subscribes to LogBuffer for live entry push when available.
/// </summary>
public sealed class LogsViewModel : INotifyPropertyChanged
{
    private readonly LogBuffer? _logBuffer;

    public ObservableCollection<LogEntryDisplay> AllEntries { get; } = [];
    public ObservableCollection<LogEntryDisplay> FilteredEntries { get; } = [];

    private LogLevel? _selectedLevel;
    public LogLevel? SelectedLevel
    {
        get => _selectedLevel;
        set
        {
            if (_selectedLevel != value)
            {
                _selectedLevel = value;
                OnPropertyChanged();
                ApplyFilter();
            }
        }
    }

    private string _searchText = string.Empty;
    public string SearchText
    {
        get => _searchText;
        set
        {
            if (_searchText != value)
            {
                _searchText = value;
                OnPropertyChanged();
                ApplyFilter();
            }
        }
    }

    private bool _autoScroll = true;
    public bool AutoScroll
    {
        get => _autoScroll;
        set { if (_autoScroll != value) { _autoScroll = value; OnPropertyChanged(); } }
    }

    public LogsViewModel(LogBuffer? logBuffer = null)
    {
        _logBuffer = logBuffer;
        SelectedLevel = null;

        if (_logBuffer != null)
        {
            LoadExistingEntries();
            _logBuffer.EntryAppended += OnEntryAppended;
        }
    }

    private void LoadExistingEntries()
    {
        if (_logBuffer == null) return;
        foreach (var entry in _logBuffer.Recent(500))
        {
            var display = LogEntryDisplay.FromLogEntry(entry);
            AllEntries.Add(display);
            if (MatchesFilter(display))
                FilteredEntries.Add(display);
        }
    }

    private void OnEntryAppended(LogEntry entry)
    {
        // This fires on whatever thread calls LogBuffer.Append().
        // The page code-behind subscribes separately and dispatches to the UI thread.
        // This VM subscription is for non-UI consumers; do NOT mutate ObservableCollection here.
    }

    public void AddEntry(LogEntryDisplay entry)
    {
        AllEntries.Add(entry);
        if (MatchesFilter(entry))
            FilteredEntries.Add(entry);
    }

    public void ClearLogs()
    {
        AllEntries.Clear();
        FilteredEntries.Clear();
    }

    private void ApplyFilter()
    {
        FilteredEntries.Clear();
        foreach (var entry in AllEntries)
        {
            if (MatchesFilter(entry))
                FilteredEntries.Add(entry);
        }
    }

    private bool MatchesFilter(LogEntryDisplay entry)
    {
        if (SelectedLevel.HasValue && entry.Level != SelectedLevel.Value)
            return false;

        if (!string.IsNullOrWhiteSpace(SearchText))
        {
            var search = SearchText.Trim();
            if (!entry.Message.Contains(search, StringComparison.OrdinalIgnoreCase))
                return false;
        }

        return true;
    }

    // ── INotifyPropertyChanged ──

    public event PropertyChangedEventHandler? PropertyChanged;
    private void OnPropertyChanged([CallerMemberName] string? name = null)
        => PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(name));
}
