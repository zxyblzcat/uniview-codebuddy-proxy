using System;
using System.Collections.ObjectModel;
using System.ComponentModel;
using System.Linq;
using System.Runtime.CompilerServices;
using System.Windows.Input;
using UniviewCodeBuddyProxy.Models;

// Services namespace imports — use aliases to avoid ambiguity with Models.TokenInfo / Models.TokenStatus
using ServicesTokenStatus = UniviewCodeBuddyProxy.Services.TokenStatus;
using TokenManager = UniviewCodeBuddyProxy.Services.TokenManager;

namespace UniviewCodeBuddyProxy.ViewModels;

/// <summary>
/// Token management view model — token list, auth flow, CRUD commands.
/// Subscribes to TokenManager for live token updates when available.
/// </summary>
public sealed class TokensViewModel : INotifyPropertyChanged
{
    private readonly TokenManager? _tokenManager;

    public ObservableCollection<TokenInfo> Tokens { get; } = [];

    private bool _isAuthenticating;
    public bool IsAuthenticating
    {
        get => _isAuthenticating;
        set { if (_isAuthenticating != value) { _isAuthenticating = value; OnPropertyChanged(); } }
    }

    private string _authURL = string.Empty;
    public string AuthURL
    {
        get => _authURL;
        set { if (_authURL != value) { _authURL = value; OnPropertyChanged(); } }
    }

    private string _manualToken = string.Empty;
    public string ManualToken
    {
        get => _manualToken;
        set { if (_manualToken != value) { _manualToken = value; OnPropertyChanged(); } }
    }

    // ── Commands ──

    public ICommand AddTokenCommand { get; }
    public ICommand DeleteTokenCommand { get; }
    public ICommand RefreshTokenCommand { get; }
    public ICommand LoginWithBrowserCommand { get; }

    public TokensViewModel(TokenManager? tokenManager = null)
    {
        _tokenManager = tokenManager;

        AddTokenCommand = new RelayCommand(ExecuteAddToken);
        DeleteTokenCommand = new RelayCommand<string>(ExecuteDeleteToken);
        RefreshTokenCommand = new RelayCommand<string>(ExecuteRefreshToken);
        LoginWithBrowserCommand = new RelayCommand(ExecuteLoginWithBrowser);

        if (_tokenManager != null)
        {
            _tokenManager.PropertyChanged += OnTokenManagerChanged;
            RefreshFromManager();
        }
    }

    private void OnTokenManagerChanged(object? sender, PropertyChangedEventArgs e)
    {
        if (e.PropertyName == nameof(TokenManager.ActiveTokenCount))
        {
            RefreshFromManager();
        }
    }

    /// <summary>
    /// Refreshes the token list from TokenManager.
    /// Must be called on the UI thread.
    /// </summary>
    public void RefreshFromManager()
    {
        if (_tokenManager == null) return;

        Tokens.Clear();
        foreach (var info in _tokenManager.GetAllTokens())
        {
            Tokens.Add(new TokenInfo
            {
                Id = info.UserID,
                UserID = info.UserID,
                Status = MapStatus(info.Status),
                CreatedAt = info.CreatedAt > 0
                    ? DateTimeOffset.FromUnixTimeSeconds(info.CreatedAt)
                    : DateTimeOffset.Now,
                ExpiresAt = info.ExpiresAt > 0
                    ? DateTimeOffset.FromUnixTimeSeconds(info.ExpiresAt)
                    : null,
            });
        }
    }

    private static TokenStatus MapStatus(ServicesTokenStatus status) => status switch
    {
        ServicesTokenStatus.Active => TokenStatus.Active,
        ServicesTokenStatus.Cooldown => TokenStatus.Cooldown,
        ServicesTokenStatus.Unavailable => TokenStatus.Unavailable,
        ServicesTokenStatus.Expired => TokenStatus.Expired,
        _ => TokenStatus.Active
    };

    private void ExecuteAddToken()
    {
        AddTokenRequested?.Invoke(this, EventArgs.Empty);
    }

    private void ExecuteDeleteToken(string? tokenId)
    {
        if (tokenId == null) return;
        DeleteTokenRequested?.Invoke(this, tokenId);
    }

    private void ExecuteRefreshToken(string? tokenId)
    {
        if (tokenId == null) return;
        RefreshTokenRequested?.Invoke(this, tokenId);
    }

    private async void ExecuteLoginWithBrowser()
    {
        IsAuthenticating = true;
        try
        {
            LoginWithBrowserRequested?.Invoke(this, EventArgs.Empty);
        }
        finally
        {
            IsAuthenticating = false;
        }
    }

    public void AddToken(TokenInfo token)
    {
        Tokens.Add(token);
    }

    public void RemoveToken(string tokenId)
    {
        for (int i = Tokens.Count - 1; i >= 0; i--)
        {
            if (Tokens[i].Id == tokenId)
            {
                Tokens.RemoveAt(i);
                break;
            }
        }
    }

    // ── Events for view code-behind to handle ──

    public event EventHandler? AddTokenRequested;
    public event EventHandler<string>? DeleteTokenRequested;
    public event EventHandler<string>? RefreshTokenRequested;
    public event EventHandler? LoginWithBrowserRequested;

    // ── INotifyPropertyChanged ──

    public event PropertyChangedEventHandler? PropertyChanged;
    private void OnPropertyChanged([CallerMemberName] string? name = null)
        => PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(name));
}

/// <summary>
/// Simple ICommand implementation.
/// </summary>
public sealed class RelayCommand : ICommand
{
    private readonly Action _execute;
    private readonly Func<bool>? _canExecute;

    public RelayCommand(Action execute, Func<bool>? canExecute = null)
    {
        _execute = execute;
        _canExecute = canExecute;
    }

    public event EventHandler? CanExecuteChanged;
    public bool CanExecute(object? parameter) => _canExecute?.Invoke() ?? true;
    public void Execute(object? parameter) => _execute();
    public void RaiseCanExecuteChanged() => CanExecuteChanged?.Invoke(this, EventArgs.Empty);
}

/// <summary>
/// Generic ICommand implementation.
/// </summary>
public sealed class RelayCommand<T> : ICommand
{
    private readonly Action<T?> _execute;
    private readonly Func<T?, bool>? _canExecute;

    public RelayCommand(Action<T?> execute, Func<T?, bool>? canExecute = null)
    {
        _execute = execute;
        _canExecute = canExecute;
    }

    public event EventHandler? CanExecuteChanged;
    public bool CanExecute(object? parameter) => _canExecute?.Invoke((T?)parameter) ?? true;
    public void Execute(object? parameter) => _execute((T?)parameter);
    public void RaiseCanExecuteChanged() => CanExecuteChanged?.Invoke(this, EventArgs.Empty);
}
