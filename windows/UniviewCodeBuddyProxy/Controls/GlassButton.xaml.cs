using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media;

namespace UniviewCodeBuddyProxy.Controls;

/// <summary>
/// Glass-themed button with Primary (accent fill) and Secondary (glass outline) variants.
/// Mirrors macOS styled buttons.
/// </summary>
public sealed partial class GlassButton : UserControl
{
    public GlassButton()
    {
        this.InitializeComponent();
    }

    // ── Text ──

    public static readonly DependencyProperty TextProperty =
        DependencyProperty.Register(nameof(Text), typeof(string), typeof(GlassButton),
            new PropertyMetadata(string.Empty, (d, e) => ((GlassButton)d).ButtonText.Text = (string)e.NewValue));

    public string Text
    {
        get => (string)GetValue(TextProperty);
        set => SetValue(TextProperty, value);
    }

    // ── Icon glyph (Segoe MDL2) ──

    public static readonly DependencyProperty IconProperty =
        DependencyProperty.Register(nameof(Icon), typeof(string), typeof(GlassButton),
            new PropertyMetadata(string.Empty, OnIconChanged));

    public string Icon
    {
        get => (string)GetValue(IconProperty);
        set => SetValue(IconProperty, value);
    }

    private static void OnIconChanged(DependencyObject d, DependencyPropertyChangedEventArgs e)
    {
        var btn = (GlassButton)d;
        var glyph = e.NewValue as string ?? string.Empty;
        btn.ButtonIcon.Glyph = glyph;
        btn.ButtonIcon.Visibility = string.IsNullOrEmpty(glyph)
            ? Visibility.Collapsed
            : Visibility.Visible;
    }

    // ── ButtonStyle (Primary / Secondary) ──

    public static readonly DependencyProperty ButtonStyleProperty =
        DependencyProperty.Register(nameof(ButtonStyle), typeof(GlassButtonStyle), typeof(GlassButton),
            new PropertyMetadata(GlassButtonStyle.Primary, OnButtonStyleChanged));

    public new GlassButtonStyle ButtonStyle
    {
        get => (GlassButtonStyle)GetValue(ButtonStyleProperty);
        set => SetValue(ButtonStyleProperty, value);
    }

    private static void OnButtonStyleChanged(DependencyObject d, DependencyPropertyChangedEventArgs e)
    {
        var btn = (GlassButton)d;
        var style = (GlassButtonStyle)e.NewValue;

        switch (style)
        {
            case GlassButtonStyle.Primary:
                btn.InternalButton.Style = Application.Current.Resources["AccentButtonStyle"] as Style;
                break;
            case GlassButtonStyle.Secondary:
            default:
                btn.InternalButton.Style = Application.Current.Resources["DefaultButtonStyle"] as Style;
                break;
        }
    }

    // ── Click event ──

    public event RoutedEventHandler? Click;

    private void OnInternalClick(object sender, RoutedEventArgs e)
    {
        Click?.Invoke(this, e);
    }
}

/// <summary>
/// Glass button visual style variant.
/// </summary>
public enum GlassButtonStyle
{
    Primary,
    Secondary
}
