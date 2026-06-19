using System;
using System.ComponentModel;
using System.Runtime.CompilerServices;
using Microsoft.UI.Dispatching;
using Microsoft.UI.Xaml;
using UniviewCodeBuddyProxy.Controls;

namespace UniviewCodeBuddyProxy.Helpers;

/// <summary>
/// Manages toast notification display — show/dismiss with auto-dismiss after 3 seconds.
/// Mirrors macOS ToastManager.
/// </summary>
public sealed class ToastManager : INotifyPropertyChanged
{
    private string _message = string.Empty;
    private ToastKind _kind = ToastKind.Info;
    private bool _isVisible;
    private DispatcherQueueTimer? _dismissTimer;

    public string Message
    {
        get => _message;
        set { if (_message != value) { _message = value; OnPropertyChanged(); } }
    }

    public ToastKind Kind
    {
        get => _kind;
        set { if (_kind != value) { _kind = value; OnPropertyChanged(); } }
    }

    public bool IsVisible
    {
        get => _isVisible;
        set { if (_isVisible != value) { _isVisible = value; OnPropertyChanged(); } }
    }

    /// <summary>
    /// Shows a toast notification with the given message and kind.
    /// Auto-dismisses after 3 seconds.
    /// </summary>
    public void Show(string message, ToastKind kind = ToastKind.Info)
    {
        Message = message;
        Kind = kind;
        IsVisible = true;

        _dismissTimer?.Stop();
        _dismissTimer = DispatcherQueue.GetForCurrentThread().CreateTimer();
        _dismissTimer.Interval = TimeSpan.FromSeconds(3);
        _dismissTimer.Tick += (_, _) =>
        {
            IsVisible = false;
            _dismissTimer.Stop();
        };
        _dismissTimer.Start();
    }

    /// <summary>
    /// Dismisses the toast immediately.
    /// </summary>
    public void Dismiss() => IsVisible = false;

    public event PropertyChangedEventHandler? PropertyChanged;
    private void OnPropertyChanged([CallerMemberName] string? name = null)
        => PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(name));
}
