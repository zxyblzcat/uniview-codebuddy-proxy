using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace UniviewCodeBuddyProxy.Controls;

/// <summary>
/// Glass-themed toggle switch wrapping the WinUI ToggleSwitch.
/// Mirrors macOS glassToggle.
/// </summary>
public sealed partial class GlassToggle : UserControl
{
    public GlassToggle()
    {
        this.InitializeComponent();
    }

    // ── IsOn dependency property ──

    public static readonly DependencyProperty IsOnProperty =
        DependencyProperty.Register(
            nameof(IsOn),
            typeof(bool),
            typeof(GlassToggle),
            new PropertyMetadata(false, OnIsOnChanged));

    public bool IsOn
    {
        get => (bool)GetValue(IsOnProperty);
        set => SetValue(IsOnProperty, value);
    }

    private static void OnIsOnChanged(DependencyObject d, DependencyPropertyChangedEventArgs e)
    {
        var toggle = (GlassToggle)d;
        toggle.InternalToggle.IsOn = (bool)e.NewValue;
    }
}
