using System;
using System.Collections.Generic;
using System.Collections.ObjectModel;
using System.Linq;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using UniviewCodeBuddyProxy.Helpers;

namespace UniviewCodeBuddyProxy.Controls;

/// <summary>
/// An item in the GlassPopupPicker.
/// </summary>
public sealed class GlassPopupPickerItem
{
    public string Label { get; set; } = "";
    public string Value { get; set; } = "";
    public string? Subtitle { get; set; }

    public override string ToString() => Label;
}

/// <summary>
/// Glass-themed dropdown picker with flyout, matching macOS GlassPopupPicker.
/// </summary>
public sealed partial class GlassPopupPicker : UserControl
{
    private readonly ObservableCollection<GlassPopupPickerItem> _items = [];

    public GlassPopupPicker()
    {
        this.InitializeComponent();
        OptionsList.ItemsSource = _items;
    }

    // ── Dependency Properties ──

    public static readonly DependencyProperty ItemsSourceProperty =
        DependencyProperty.Register(nameof(ItemsSource), typeof(IList<GlassPopupPickerItem>), typeof(GlassPopupPicker),
            new PropertyMetadata(null, OnItemsSourceChanged));

    public IList<GlassPopupPickerItem> ItemsSource
    {
        get => (IList<GlassPopupPickerItem>)GetValue(ItemsSourceProperty);
        set => SetValue(ItemsSourceProperty, value);
    }

    public static readonly DependencyProperty SelectedValueProperty =
        DependencyProperty.Register(nameof(SelectedValue), typeof(string), typeof(GlassPopupPicker),
            new PropertyMetadata(string.Empty, OnSelectedValueChanged));

    public string SelectedValue
    {
        get => (string)GetValue(SelectedValueProperty);
        set => SetValue(SelectedValueProperty, value);
    }

    public static readonly DependencyProperty PopupWidthProperty =
        DependencyProperty.Register(nameof(PopupWidth), typeof(double), typeof(GlassPopupPicker),
            new PropertyMetadata(180.0));

    public double PopupWidth
    {
        get => (double)GetValue(PopupWidthProperty);
        set => SetValue(PopupWidthProperty, value);
    }

    private static void OnItemsSourceChanged(DependencyObject d, DependencyPropertyChangedEventArgs e)
    {
        var picker = (GlassPopupPicker)d;
        picker._items.Clear();
        if (e.NewValue is IList<GlassPopupPickerItem> items)
        {
            foreach (var item in items)
                picker._items.Add(item);
        }
        picker.UpdateSelectedText();
    }

    private static void OnSelectedValueChanged(DependencyObject d, DependencyPropertyChangedEventArgs e)
    {
        var picker = (GlassPopupPicker)d;
        picker.UpdateSelectedText();
        picker.UpdateListSelection();
    }

    private void OnTriggerClick(object sender, RoutedEventArgs e)
    {
        OptionsList.Width = PopupWidth;
        PickerFlyout.ShowAt(TriggerButton);
    }

    private void OnSelectionChanged(object sender, SelectionChangedEventArgs e)
    {
        if (OptionsList.SelectedItem is GlassPopupPickerItem item)
        {
            SelectedValue = item.Value;
            SelectedText.Text = item.Label;
            PickerFlyout.Hide();
        }
    }

    private void UpdateSelectedText()
    {
        var selected = _items.FirstOrDefault(i => i.Value == SelectedValue);
        SelectedText.Text = selected?.Label ?? SelectedValue;
    }

    private void UpdateListSelection()
    {
        var selected = _items.FirstOrDefault(i => i.Value == SelectedValue);
        if (selected != null)
            OptionsList.SelectedItem = selected;
    }
}
