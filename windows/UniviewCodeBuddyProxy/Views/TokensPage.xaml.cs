using System;
using System.Threading.Tasks;
using UniviewCodeBuddyProxy.Controls;
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
/// Token management page — list, add, delete, refresh, browser login.
/// Mirrors macOS TokensView.
/// </summary>
public sealed partial class TokensPage : Page
{
    private readonly AuthService _authService;
    private readonly TokenManager _tokenManager;
    private DispatcherQueue? _dispatcherQueue;

    public TokensViewModel ViewModel { get; }

    public TokensPage()
    {
        var app = (App)Application.Current;
        _authService = app.AuthService;
        _tokenManager = app.TokenManager;
        ViewModel = new TokensViewModel(_tokenManager);
        this.InitializeComponent();

        ViewModel.Tokens.CollectionChanged += (_, _) => UpdateEmptyState();
        UpdateEmptyState();

        Loaded += OnLoaded;
    }

    private void OnLoaded(object sender, RoutedEventArgs e)
    {
        _dispatcherQueue = DispatcherQueue.GetForCurrentThread();
    }

    private void UpdateEmptyState()
    {
        EmptyState.Visibility = ViewModel.Tokens.Count == 0
            ? Visibility.Visible
            : Visibility.Collapsed;
        TokenList.Visibility = ViewModel.Tokens.Count > 0
            ? Visibility.Visible
            : Visibility.Collapsed;
    }

    // ── Login with Browser ──

    private async void OnLoginWithBrowser(object sender, RoutedEventArgs e)
    {
        ViewModel.IsAuthenticating = true;
        try
        {
            var (authURL, authState) = await _authService.StartDeviceFlowAsync();

            // Show auth URL dialog
            var dialog = new ContentDialog
            {
                Title = "浏览器登录",
                Content = $"请在浏览器中完成登录:\n\n{authURL}",
                PrimaryButtonText = "复制链接并打开",
                CloseButtonText = "取消",
                DefaultButton = ContentDialogButton.Primary,
                XamlRoot = this.XamlRoot
            };

            var result = await dialog.ShowAsync();
            if (result == ContentDialogResult.Primary)
            {
                // Copy to clipboard
                var dataPackage = new Windows.ApplicationModel.DataTransfer.DataPackage();
                dataPackage.SetText(authURL);
                Windows.ApplicationModel.DataTransfer.Clipboard.SetContent(dataPackage);

                // Open in browser
                await Windows.System.Launcher.LaunchUriAsync(new Uri(authURL));
            }

            // Poll for token completion
            for (int i = 0; i < 60; i++)
            {
                await Task.Delay(3000);
                var pollResult = await _authService.PollTokenAsync(authState);
                if (pollResult.Type == PollResultType.Success && pollResult.Token != null)
                {
                    _tokenManager.AddToken(pollResult.Token);
                    _dispatcherQueue?.TryEnqueue(() => ViewModel.RefreshFromManager());
                    break;
                }
                if (pollResult.Type == PollResultType.Failed)
                    break;
            }
        }
        catch (Exception ex)
        {
            var dialog = new ContentDialog
            {
                Title = "登录失败",
                Content = ex.Message,
                CloseButtonText = "确定",
                XamlRoot = this.XamlRoot
            };
            await dialog.ShowAsync();
        }
        finally
        {
            ViewModel.IsAuthenticating = false;
        }
    }

    // ── Add Token (manual entry) ──

    private async void OnAddToken(object sender, RoutedEventArgs e)
    {
        var tokenInput = new TextBox
        {
            PlaceholderText = "在此粘贴令牌...",
            AcceptsReturn = false
        };

        var dialog = new ContentDialog
        {
            Title = "手动添加令牌",
            Content = tokenInput,
            PrimaryButtonText = "添加",
            CloseButtonText = "取消",
            DefaultButton = ContentDialogButton.Primary,
            XamlRoot = this.XamlRoot
        };

        var result = await dialog.ShowAsync();
        if (result == ContentDialogResult.Primary && !string.IsNullOrWhiteSpace(tokenInput.Text))
        {
            try
            {
                var tokenData = _authService.ParseManualToken(tokenInput.Text);
                _tokenManager.AddToken(tokenData);
                ViewModel.RefreshFromManager();
            }
            catch (Exception ex)
            {
                var errorDialog = new ContentDialog
                {
                    Title = "添加失败",
                    Content = ex.Message,
                    CloseButtonText = "确定",
                    XamlRoot = this.XamlRoot
                };
                await errorDialog.ShowAsync();
            }
        }
    }

    // ── Refresh Token ──

    private async void OnRefreshToken(object sender, RoutedEventArgs e)
    {
        if (sender is not Button btn || btn.Tag is not string userId) return;

        try
        {
            var existingToken = _tokenManager.GetTokenData(userId);
            if (existingToken == null || string.IsNullOrEmpty(existingToken.RefreshToken))
            {
                var noRefreshDialog = new ContentDialog
                {
                    Title = "无法刷新",
                    Content = "此令牌没有刷新令牌，无法自动刷新。",
                    CloseButtonText = "确定",
                    XamlRoot = this.XamlRoot
                };
                await noRefreshDialog.ShowAsync();
                return;
            }

            var tokenData = await _authService.RefreshTokenAsync(existingToken.RefreshToken);
            if (tokenData != null)
            {
                _tokenManager.AddToken(tokenData);
                ViewModel.RefreshFromManager();
            }
        }
        catch (Exception ex)
        {
            var dialog = new ContentDialog
            {
                Title = "刷新失败",
                Content = ex.Message,
                CloseButtonText = "确定",
                XamlRoot = this.XamlRoot
            };
            await dialog.ShowAsync();
        }
    }

    // ── Delete Token ──

    private async void OnDeleteToken(object sender, RoutedEventArgs e)
    {
        if (sender is not Button btn || btn.Tag is not string tokenId) return;

        var dialog = new ContentDialog
        {
            Title = "删除令牌",
            Content = "确定要删除此令牌吗？",
            PrimaryButtonText = "删除",
            CloseButtonText = "取消",
            DefaultButton = ContentDialogButton.Close,
            XamlRoot = this.XamlRoot
        };

        var result = await dialog.ShowAsync();
        if (result == ContentDialogResult.Primary)
        {
            _tokenManager.RemoveToken(tokenId);
            ViewModel.RemoveToken(tokenId);
        }
    }
}
