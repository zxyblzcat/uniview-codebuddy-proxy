using System;
using System.ComponentModel;
using System.Runtime.CompilerServices;
using UniviewCodeBuddyProxy.Helpers;
using UniviewCodeBuddyProxy.Services;

namespace UniviewCodeBuddyProxy.ViewModels;

/// <summary>
/// Main view model for the shell — tab selection, server status, toast messages.
/// </summary>
public sealed class MainViewModel : INotifyPropertyChanged
{
    private readonly ThemeManager _themeManager;
    private readonly ConfigManager _configManager;

    private int _selectedTab;
    private bool _isServerRunning = true;
    private int _activeTokenCount;
    private int _modelCount;
    private string _toastMessage = string.Empty;
    private bool _isToastVisible;
    private ToastKind _toastType;

    public MainViewModel(ThemeManager themeManager, ConfigManager configManager)
    {
        _themeManager = themeManager;
        _configManager = configManager;
    }

    // ── Tab selection ──

    public int SelectedTab
    {
        get => _selectedTab;
        set { if (_selectedTab != value) { _selectedTab = value; OnPropertyChanged(); } }
    }

    // ── Server status ──

    public bool IsServerRunning
    {
        get => _isServerRunning;
        set { if (_isServerRunning != value) { _isServerRunning = value; OnPropertyChanged(); } }
    }

    public int ActiveTokenCount
    {
        get => _activeTokenCount;
        set { if (_activeTokenCount != value) { _activeTokenCount = value; OnPropertyChanged(); } }
    }

    public int ModelCount
    {
        get => _modelCount;
        set { if (_modelCount != value) { _modelCount = value; OnPropertyChanged(); } }
    }

    // ── Port info ──

    public int Port => _configManager.Port;

    // ── Toast ──

    public string ToastMessage
    {
        get => _toastMessage;
        set { if (_toastMessage != value) { _toastMessage = value; OnPropertyChanged(); } }
    }

    public bool IsToastVisible
    {
        get => _isToastVisible;
        set { if (_isToastVisible != value) { _isToastVisible = value; OnPropertyChanged(); } }
    }

    public ToastKind ToastType
    {
        get => _toastType;
        set { if (_toastType != value) { _toastType = value; OnPropertyChanged(); } }
    }

    // ── Theme ──

    public ThemeManager ThemeManager => _themeManager;
    public ThemeColors Colors => _themeManager.Colors;

    // ── Methods ──

    public void ShowToast(string message, ToastKind kind = ToastKind.Info)
    {
        ToastMessage = message;
        ToastType = kind;
        IsToastVisible = true;
    }

    public void HideToast()
    {
        IsToastVisible = false;
    }

    // ── INotifyPropertyChanged ──

    public event PropertyChangedEventHandler? PropertyChanged;
    private void OnPropertyChanged([CallerMemberName] string? name = null)
        => PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(name));
}

/// <summary>
/// Toast notification kind.
/// </summary>
public enum ToastKind
{
    Info,
    Success,
    Warning,
    Error
}
