using System.ComponentModel;
using System.Runtime.CompilerServices;
using Microsoft.UI;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Media;

namespace UniviewCodeBuddyProxy.Helpers;

// ═══════════════════════════════════════════════
// Appearance Mode — Dark/Light/Follow System
// ═══════════════════════════════════════════════

/// <summary>
/// Controls whether the app uses dark or light appearance,
/// or follows the OS setting.
/// </summary>
public enum AppearanceMode
{
    System,  // 跟随系统
    Light,   // 浅色
    Dark     // 深色
}

// ═══════════════════════════════════════════════
// Semantic Colors — derived from appearance mode
// ═══════════════════════════════════════════════

/// <summary>
/// Semantic theme colors driven by appearance mode (dark/light).
/// </summary>
public sealed class ThemeColors
{
    public bool IsDark { get; }

    public ThemeColors(bool isDark = true)
    {
        IsDark = isDark;
    }

    // ── Base colors ──

    /// <summary>Primary background</summary>
    public Color Bg => IsDark ? ColorHelper.FromHex("#0B0F19") : ColorHelper.FromHex("#F5F7FA");
    /// <summary>Foreground (main text)</summary>
    public Color Fg => IsDark ? ColorHelper.FromHex("#E8ECF4") : ColorHelper.FromHex("#1A1D26");
    /// <summary>Brand primary</summary>
    public Color Primary => ColorHelper.FromHex("#5B9CF6");
    /// <summary>Accent color</summary>
    public Color Accent => ColorHelper.FromHex("#34D4AA");
    // ── Surface ──

    /// <summary>Surface (card/secondary background)</summary>
    public Color Surface => IsDark ? ColorHelper.FromHex("#131926") : ColorHelper.FromHex("#FFFFFF");
    /// <summary>Surface as SolidColorBrush</summary>
    public SolidColorBrush SurfaceBrush => new(Surface);

    // ── Text hierarchy ──

    public Color Text => Fg;
    public Color TextSecondary => Fg.WithOpacity(0.6);
    public Color TextMuted => Fg.WithOpacity(0.35);

    public SolidColorBrush TextBrush => new(Text);
    public SolidColorBrush TextSecondaryBrush => new(TextSecondary);
    public SolidColorBrush TextMutedBrush => new(TextMuted);

    // ── Primary variants ──

    public Color PrimaryHover => IsDark ? Primary.WithOpacity(0.85) : Primary.WithOpacity(0.8);
    public Color PrimarySubtle => Primary.WithOpacity(0.14);

    /// <summary>Text color on primary background (white — primary is always #5B9CF6 blue).</summary>
    public Color TextOnPrimary => Colors.White;

    // ── Accent variants ──

    public Color AccentSubtle => Accent.WithOpacity(0.14);

    // ── Functional colors (static) ──

    public static Color Success => ColorHelper.FromHex("#4ADE80");
    public static Color SuccessSubtle => ColorHelper.FromHex("#4ADE80").WithOpacity(0.12);
    public static Color Warning => ColorHelper.FromHex("#FBBF24");
    public static Color WarningSubtle => ColorHelper.FromHex("#FBBF24").WithOpacity(0.12);
    public static Color Danger => ColorHelper.FromHex("#F87171");
    public static Color DangerSubtle => ColorHelper.FromHex("#F87171").WithOpacity(0.12);
    public static Color Info => ColorHelper.FromHex("#60A5FA");
    public static Color InfoSubtle => ColorHelper.FromHex("#60A5FA").WithOpacity(0.12);

    // Purple
    public static Color Purple => ColorHelper.FromHex("#A78BFA");
    public static Color PurpleSubtle => ColorHelper.FromHex("#A78BFA").WithOpacity(0.10);

    // ── Glass material — adaptive ──

    public Color GlassBg => IsDark ? Colors.White.WithOpacity(0.055) : Colors.Black.WithOpacity(0.04);
    public Color GlassBgHeavy => IsDark ? Colors.White.WithOpacity(0.09) : Colors.Black.WithOpacity(0.06);
    public Color GlassBgTabbar => IsDark ? ColorHelper.FromHex("#101420").WithOpacity(0.72) : Colors.White.WithOpacity(0.72);
    public Color GlassBorder => IsDark ? Colors.White.WithOpacity(0.09) : Colors.Black.WithOpacity(0.10);
    public SolidColorBrush GlassBorderBrush => new(GlassBorder);
    public Color GlassBorderLight => IsDark ? Colors.White.WithOpacity(0.15) : Colors.Black.WithOpacity(0.15);

    // ── Highlight gradient — adaptive ──

    public Color HighlightGradientStart => IsDark ? Colors.White.WithOpacity(0.08) : Colors.White.WithOpacity(0.7);
    public Color HighlightGradientEnd => Colors.Transparent;

    // ── Hover background ──

    public Color HoverBg => IsDark ? Colors.White.WithOpacity(0.02) : Colors.Black.WithOpacity(0.03);

    // ── Toggle ──

    /// <summary>Toggle thumb color in off-state (white in dark, dark in light — to contrast with glassBgHeavy track).</summary>
    public Color ToggleThumbOff => IsDark ? Colors.White : Fg;

    // ── Corner radii ──

    public static double Radius => 20;
    public double RadiusSM => Radius * 0.5;
    public double RadiusMD => Radius;
    public double RadiusLG => Radius * 1.4;
    public static double RadiusPill => 999;

    // ── Animation ──

    public static double EaseHarmonyResponse => 0.35;
    public static double EaseHarmonyDamping => 0.75;

    // ── Font ──

    public static string FontMono => "Cascadia Code";
    public static double TabbarHeight => 72;
}

// ═══════════════════════════════════════════════
// Theme Manager — INotifyPropertyChanged with persistence
// ═══════════════════════════════════════════════

/// <summary>
/// Manages appearance mode and persistence.
/// Persists to the Windows Registry (HKCU\Software\UniviewCodeBuddyProxy).
/// </summary>
public sealed class ThemeManager : INotifyPropertyChanged
{
    private AppearanceMode _appearanceMode;
    private ThemeColors _colors;
    private bool _systemIsDark = true;

    public ThemeManager()
    {
        var savedAppearance = LoadSavedAppearanceMode();
        _appearanceMode = savedAppearance;
        _colors = new ThemeColors(IsDark);
    }

    public AppearanceMode AppearanceMode
    {
        get => _appearanceMode;
        set
        {
            if (_appearanceMode == value) return;
            _appearanceMode = value;
            RebuildColors();
            SaveAppearanceMode(value);
            OnPropertyChanged();
            OnPropertyChanged(nameof(Colors));
            OnPropertyChanged(nameof(IsDark));
            OnPropertyChanged(nameof(EffectiveElementTheme));
        }
    }

    public ThemeColors Colors => _colors;

    /// <summary>
    /// Whether the effective appearance is dark (combines AppearanceMode + system state).
    /// </summary>
    public bool IsDark => _appearanceMode switch
    {
        AppearanceMode.Dark => true,
        AppearanceMode.Light => false,
        AppearanceMode.System => _systemIsDark,
        _ => true
    };

    /// <summary>
    /// The ElementTheme to apply to the root element for WinUI theming.
    /// Default (System) returns ElementTheme.Default so WinUI follows the OS.
    /// </summary>
    public ElementTheme EffectiveElementTheme => _appearanceMode switch
    {
        AppearanceMode.Dark => ElementTheme.Dark,
        AppearanceMode.Light => ElementTheme.Light,
        AppearanceMode.System => ElementTheme.Default,
        _ => ElementTheme.Default
    };

    /// <summary>
    /// Called when the system theme changes (via UISettings.ColorValuesChanged).
    /// </summary>
    public void UpdateSystemTheme(bool systemIsDark)
    {
        _systemIsDark = systemIsDark;
        if (_appearanceMode == AppearanceMode.System)
        {
            RebuildColors();
            OnPropertyChanged(nameof(Colors));
            OnPropertyChanged(nameof(IsDark));
            OnPropertyChanged(nameof(EffectiveElementTheme));
        }
    }

    private void RebuildColors()
    {
        _colors = new ThemeColors(IsDark);
    }

    public event PropertyChangedEventHandler? PropertyChanged;

    private void OnPropertyChanged([CallerMemberName] string? name = null)
        => PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(name));

    private const string RegistryKey = @"Software\UniviewCodeBuddyProxy";
    private const string AppearanceValue = "AppearanceMode";

    private static AppearanceMode LoadSavedAppearanceMode()
    {
        try
        {
            using var key = Microsoft.Win32.Registry.CurrentUser.OpenSubKey(RegistryKey);
            var saved = key?.GetValue(AppearanceValue) as string;

            return saved switch
            {
                nameof(AppearanceMode.System) => AppearanceMode.System,
                nameof(AppearanceMode.Light) => AppearanceMode.Light,
                nameof(AppearanceMode.Dark) => AppearanceMode.Dark,
                _ => AppearanceMode.System
            };
        }
        catch
        {
            return AppearanceMode.System;
        }
    }

    private static void SaveAppearanceMode(AppearanceMode mode)
    {
        try
        {
            using var key = Microsoft.Win32.Registry.CurrentUser.CreateSubKey(RegistryKey);
            key?.SetValue(AppearanceValue, mode.ToString());
        }
        catch
        {
            // Silently ignore if registry write fails
        }
    }
}

// ═══════════════════════════════════════════════
// Helper types
// ═══════════════════════════════════════════════

/// <summary>
/// Color helper utilities for hex parsing and opacity manipulation.
/// Uses Microsoft.UI.Color (WinUI 3).
/// </summary>
public static class ColorHelper
{
    /// <summary>
    /// Creates a Color from a hex string like "#RRGGBB" or "#AARRGGBB".
    /// </summary>
    public static Color FromHex(string hex)
    {
        hex = hex.TrimStart('#');
        var span = hex.AsSpan();

        return hex.Length switch
        {
            6 => Color.FromArgb(255,
                byte.Parse(span[..2], System.Globalization.NumberStyles.HexNumber),
                byte.Parse(span[2..4], System.Globalization.NumberStyles.HexNumber),
                byte.Parse(span[4..6], System.Globalization.NumberStyles.HexNumber)),
            8 => Color.FromArgb(
                byte.Parse(span[..2], System.Globalization.NumberStyles.HexNumber),
                byte.Parse(span[2..4], System.Globalization.NumberStyles.HexNumber),
                byte.Parse(span[4..6], System.Globalization.NumberStyles.HexNumber),
                byte.Parse(span[6..8], System.Globalization.NumberStyles.HexNumber)),
            _ => Colors.Transparent
        };
    }

    /// <summary>
    /// Converts a Color to a hex string like "#RRGGBB".
    /// </summary>
    public static string ToHex(Color color)
    {
        return $"#{color.R:X2}{color.G:X2}{color.B:X2}";
    }

    /// <summary>
    /// Returns a new Color with the specified opacity (0.0–1.0).
    /// </summary>
    public static Color WithOpacity(this Color color, double opacity)
    {
        return Color.FromArgb(
            (byte)(color.A * opacity),
            color.R,
            color.G,
            color.B);
    }

    /// <summary>
    /// Converts a Color to a SolidColorBrush.
    /// </summary>
    public static SolidColorBrush ToBrush(this Color color) => new(color);

    /// <summary>
    /// Converts a Color to a SolidColorBrush with the specified opacity.
    /// </summary>
    public static SolidColorBrush ToBrush(this Color color, double opacity) => new(color.WithOpacity(opacity));
}
