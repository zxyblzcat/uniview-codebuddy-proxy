using Microsoft.UI;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media;
using UniviewCodeBuddyProxy.Helpers;

namespace UniviewCodeBuddyProxy.Controls;

/// <summary>
/// Toast notification kind (maps to icon + color).
/// </summary>
public enum ToastKind
{
    Info,
    Success,
    Warning,
    Error
}

/// <summary>
/// Glass-morphism toast notification — capsule shape, auto-dismiss after 3 seconds.
/// Mirrors macOS Toast.swift.
/// </summary>
public sealed partial class Toast : UserControl
{
    public Toast()
    {
        this.InitializeComponent();
        UpdateAppearance();
    }

    // ── Dependency Properties ──

    public static readonly DependencyProperty MessageProperty =
        DependencyProperty.Register(nameof(Message), typeof(string), typeof(Toast),
            new PropertyMetadata(string.Empty));

    public string Message
    {
        get => (string)GetValue(MessageProperty);
        set => SetValue(MessageProperty, value);
    }

    public static readonly DependencyProperty KindProperty =
        DependencyProperty.Register(nameof(Kind), typeof(ToastKind), typeof(Toast),
            new PropertyMetadata(ToastKind.Info, OnKindChanged));

    public ToastKind Kind
    {
        get => (ToastKind)GetValue(KindProperty);
        set => SetValue(KindProperty, value);
    }

    private static void OnKindChanged(DependencyObject d, DependencyPropertyChangedEventArgs e)
    {
        ((Toast)d).UpdateAppearance();
    }

    public static readonly DependencyProperty ToastBorderColorProperty =
        DependencyProperty.Register(nameof(ToastBorderColor), typeof(Brush), typeof(Toast),
            new PropertyMetadata(new SolidColorBrush(Colors.Transparent)));

    public Brush ToastBorderColor
    {
        get => (Brush)GetValue(ToastBorderColorProperty);
        set => SetValue(ToastBorderColorProperty, value);
    }

    // ── Appearance ──

    private void UpdateAppearance()
    {
        var (icon, borderColor) = Kind switch
        {
            ToastKind.Success => ("✅", ThemeColors.SuccessSubtle),
            ToastKind.Warning => ("⚠️", ThemeColors.WarningSubtle),
            ToastKind.Error => ("❌", ThemeColors.DangerSubtle),
            _ => ("ℹ️", ThemeColors.InfoSubtle)
        };

        ToastIcon.Text = icon;
        ToastBorderColor = new SolidColorBrush(borderColor);
    }
}
