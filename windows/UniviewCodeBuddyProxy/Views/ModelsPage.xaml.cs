using System;
using UniviewCodeBuddyProxy.Controls;
using UniviewCodeBuddyProxy.Models;
using UniviewCodeBuddyProxy.ViewModels;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace UniviewCodeBuddyProxy.Views;

/// <summary>
/// Models page — searchable/filterable grid of available models.
/// Mirrors macOS ModelsView.
/// </summary>
public sealed partial class ModelsPage : Page
{
    public ModelsViewModel ViewModel { get; }

    public ModelsPage()
    {
        ViewModel = new ModelsViewModel();
        this.InitializeComponent();
    }

    private void OnProviderChipChecked(object sender, RoutedEventArgs e)
    {
        if (sender is RadioButton rb && rb.Content is string provider)
        {
            ViewModel.SelectedProvider = provider;
        }
    }

    private async void OnModelClick(object sender, ItemClickEventArgs e)
    {
        if (e.ClickedItem is not ModelInfo model) return;

        var dialog = new ContentDialog
        {
            Title = model.Name,
            Content = $"""
                提供商: {model.OwnedBy}
                推断: {model.Provider}
                ID: {model.Id}
                """,
            CloseButtonText = "关闭",
            XamlRoot = this.XamlRoot
        };

        await dialog.ShowAsync();
    }
}
