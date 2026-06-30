using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace UniviewCodeBuddyProxy.Controls;

/// <summary>
/// Settings row with icon, label, and content slot, plus divider.
/// Mirrors macOS settingsRow().
/// ContentSlot is a DependencyProperty that forwards its value to the
/// internal ContentPresenter, allowing XamlCompiler to recognize it.
/// </summary>
public sealed partial class SettingItem : UserControl
{
    public SettingItem()
    {
        this.InitializeComponent();
    }

    // ── ContentSlot ──

    /// <summary>
    /// Content to display in the right-aligned slot (e.g., toggle, slider, textbox).
    /// This is the XAML-settable property that XamlCompiler recognizes via
    /// [ContentProperty(Name = "ContentSlot")] on the class.
    /// </summary>
    public static readonly DependencyProperty ContentSlotProperty =
        DependencyProperty.Register(nameof(ContentSlot), typeof(object), typeof(SettingItem),
            new PropertyMetadata(null, OnContentSlotChanged));

    public object ContentSlot
    {
        get => GetValue(ContentSlotProperty);
        set => SetValue(ContentSlotProperty, value);
    }

    private static void OnContentSlotChanged(DependencyObject d, DependencyPropertyChangedEventArgs e)
    {
        var item = (SettingItem)d;
        item.SlotPresenter.Content = e.NewValue;
    }

    // ── Icon (Segoe MDL2 glyph) ──

    public static readonly DependencyProperty IconProperty =
        DependencyProperty.Register(nameof(Icon), typeof(string), typeof(SettingItem),
            new PropertyMetadata(string.Empty, OnIconChanged));

    public string Icon
    {
        get => (string)GetValue(IconProperty);
        set => SetValue(IconProperty, value);
    }

    private static void OnIconChanged(DependencyObject d, DependencyPropertyChangedEventArgs e)
    {
        var item = (SettingItem)d;
        var glyph = e.NewValue as string ?? string.Empty;
        item.ItemIcon.Glyph = glyph;
        item.ItemIcon.Visibility = string.IsNullOrEmpty(glyph)
            ? Visibility.Collapsed
            : Visibility.Visible;
    }

    // ── Label ──

    public static readonly DependencyProperty LabelProperty =
        DependencyProperty.Register(nameof(Label), typeof(string), typeof(SettingItem),
            new PropertyMetadata(string.Empty, (d, e) => ((SettingItem)d).LabelText.Text = (string)e.NewValue));

    public string Label
    {
        get => (string)GetValue(LabelProperty);
        set => SetValue(LabelProperty, value);
    }
}
