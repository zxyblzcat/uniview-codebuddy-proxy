using System;
using UniviewCodeBuddyProxy.Helpers;
using UniviewCodeBuddyProxy.Services;
using UniviewCodeBuddyProxy.ViewModels;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace UniviewCodeBuddyProxy.Views;

/// <summary>
/// Settings page — all configuration sections with controls.
/// Mirrors macOS SettingsView.
/// </summary>
public sealed partial class SettingsPage : Page
{
    public SettingsViewModel ViewModel { get; }

    public SettingsPage()
    {
        var app = (App)Application.Current;
        ViewModel = new SettingsViewModel(app.ConfigManager, app.ThemeManager);
        this.InitializeComponent();

        // Set initial password without triggering save
        if (ApiPasswordBox != null)
        {
            ApiPasswordBox.Password = ViewModel.ApiPassword;
        }

        UpdateCleanupLabel();
        ViewModel.PropertyChanged += (_, e) =>
        {
            if (e.PropertyName == nameof(ViewModel.LogCleanupInterval))
                UpdateCleanupLabel();
        };
    }

    // ── Appearance mode selection ──

    private void OnAppearanceModeChanged(object sender, RoutedEventArgs e)
    {
        if (sender is RadioButton rb && rb.Tag is string tag)
        {
            ViewModel.SelectedAppearance = tag switch
            {
                "System" => AppearanceMode.System,
                "Light" => AppearanceMode.Light,
                "Dark" => AppearanceMode.Dark,
                _ => AppearanceMode.System
            };
        }
    }

    // ── Locale selection ──

    private void OnLocaleChanged(object sender, RoutedEventArgs e)
    {
        if (sender is RadioButton rb && rb.Tag is string locale)
        {
            ViewModel.Locale = locale;
        }
    }

    // ── API Password ──

    private void OnApiPasswordChanged(object sender, RoutedEventArgs e)
    {
        if (sender is PasswordBox pb)
        {
            ViewModel.ApiPassword = pb.Password;
        }
    }

    // ── Reset to defaults ──

    private async void OnResetToDefaults(object sender, RoutedEventArgs e)
    {
        var dialog = new ContentDialog
        {
            Title = "恢复默认",
            Content = "确定要将所有设置恢复为默认值吗？",
            PrimaryButtonText = "恢复",
            CloseButtonText = "取消",
            DefaultButton = ContentDialogButton.Close,
            XamlRoot = this.XamlRoot
        };

        var result = await dialog.ShowAsync();
        if (result == ContentDialogResult.Primary)
        {
            ViewModel.ResetToDefaults();
        }
    }

    // ── Helper ──

    private void UpdateCleanupLabel()
    {
        if (CleanupLabel != null)
        {
            CleanupLabel.Text = SettingsViewModel.FormatInterval((int)ViewModel.LogCleanupInterval);
        }
    }
}
