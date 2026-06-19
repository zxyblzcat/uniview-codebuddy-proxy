using System;
using System.Collections.Generic;
using System.Collections.ObjectModel;
using System.ComponentModel;
using System.Linq;
using System.Runtime.CompilerServices;
using UniviewCodeBuddyProxy.Models;
using UniviewCodeBuddyProxy.Services;

namespace UniviewCodeBuddyProxy.ViewModels;

/// <summary>
/// Models page view model — model list with search and provider filter.
/// </summary>
public sealed class ModelsViewModel : INotifyPropertyChanged
{
    private string _searchText = string.Empty;
    private string _selectedProvider = "全部";

    public ObservableCollection<ModelInfo> AllModels { get; } = [];
    public ObservableCollection<ModelInfo> FilteredModels { get; } = [];
    public ObservableCollection<string> Providers { get; } = ["全部"];

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

    public string SelectedProvider
    {
        get => _selectedProvider;
        set
        {
            if (_selectedProvider != value)
            {
                _selectedProvider = value;
                OnPropertyChanged();
                ApplyFilter();
            }
        }
    }

    public ModelsViewModel()
    {
        LoadDefaultModels();
    }

    private void LoadDefaultModels()
    {
        AllModels.Clear();
        foreach (var (name, ownedBy) in Services.Constants.ExtraModels)
        {
            AllModels.Add(new ModelInfo { Id = name, Name = name, OwnedBy = ownedBy });
        }

        RefreshProviders();
        ApplyFilter();
    }

    public void LoadModels(IEnumerable<ModelInfo> models)
    {
        AllModels.Clear();
        foreach (var m in models)
            AllModels.Add(m);
        RefreshProviders();
        ApplyFilter();
    }

    private void RefreshProviders()
    {
        Providers.Clear();
        Providers.Add("全部");
        foreach (var provider in AllModels.Select(m => m.Provider).Distinct().OrderBy(p => p))
            Providers.Add(provider);
    }

    private void ApplyFilter()
    {
        FilteredModels.Clear();
        var query = AllModels.AsEnumerable();

        if (!string.IsNullOrWhiteSpace(SearchText))
        {
            var search = SearchText.Trim();
            query = query.Where(m =>
                m.Name.Contains(search, StringComparison.OrdinalIgnoreCase) ||
                m.OwnedBy.Contains(search, StringComparison.OrdinalIgnoreCase));
        }

        if (SelectedProvider != "全部")
            query = query.Where(m => m.Provider == SelectedProvider);

        foreach (var m in query)
            FilteredModels.Add(m);
    }

    // ── INotifyPropertyChanged ──

    public event PropertyChangedEventHandler? PropertyChanged;
    private void OnPropertyChanged([CallerMemberName] string? name = null)
        => PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(name));
}
