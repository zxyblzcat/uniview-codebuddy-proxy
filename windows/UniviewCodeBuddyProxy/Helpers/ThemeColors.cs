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
/// or follows the OS setting. Orthogonal to ThemePreset.
/// </summary>
public enum AppearanceMode
{
    System,  // 跟随系统
    Light,   // 浅色
    Dark     // 深色
}

// ═══════════════════════════════════════════════
// Seed Tokens — foundation for all color derivation
// ═══════════════════════════════════════════════

/// <summary>
/// Theme preset name, matching macOS ThemePreset.
/// </summary>
public enum ThemePreset
{
    Deep,      // 深邃
    Bright,    // 明亮
    Midnight,  // 午夜
    Sunset     // 日落
}

/// <summary>
/// Seed tokens from which all semantic colors are derived.
/// </summary>
public sealed class SeedTokens
{
    public Color Bg { get; init; }
    public Color Fg { get; init; }
    public Color Primary { get; init; }
    public Color Accent { get; init; }
    public Color Surface { get; init; }
    public double Radius { get; init; }

    public static SeedTokens Deep { get; } = new()
    {
        Bg = ColorHelper.FromHex("#0B0F19"),
        Fg = ColorHelper.FromHex("#E8ECF4"),
        Primary = ColorHelper.FromHex("#5B9CF6"),
        Accent = ColorHelper.FromHex("#34D4AA"),
        Surface = ColorHelper.FromHex("#131926"),
        Radius = 20
    };

    public static SeedTokens Bright { get; } = new()
    {
        Bg = ColorHelper.FromHex("#F5F7FA"),
        Fg = ColorHelper.FromHex("#1A1D26"),
        Primary = ColorHelper.FromHex("#5B9CF6"),
        Accent = ColorHelper.FromHex("#34D4AA"),
        Surface = ColorHelper.FromHex("#FFFFFF"),
        Radius = 20
    };

    public static SeedTokens Midnight { get; } = new()
    {
        Bg = ColorHelper.FromHex("#050709"),
        Fg = ColorHelper.FromHex("#E8ECF4"),
        Primary = ColorHelper.FromHex("#6366F1"),
        Accent = ColorHelper.FromHex("#34D4AA"),
        Surface = ColorHelper.FromHex("#0A0C10"),
        Radius = 20
    };

    public static SeedTokens Sunset { get; } = new()
    {
        Bg = ColorHelper.FromHex("#0F1119"),
        Fg = ColorHelper.FromHex("#E8ECF4"),
        Primary = ColorHelper.FromHex("#F97316"),
        Accent = ColorHelper.FromHex("#34D4AA"),
        Surface = ColorHelper.FromHex("#161820"),
        Radius = 20
    };

    public static SeedTokens ForPreset(ThemePreset preset) => preset switch
    {
        ThemePreset.Deep => Deep,
        ThemePreset.Bright => Bright,
        ThemePreset.Midnight => Midnight,
        ThemePreset.Sunset => Sunset,
        _ => Deep
    };
}

// ═══════════════════════════════════════════════
// Semantic Colors — derived from seed tokens + appearance
// ═══════════════════════════════════════════════

/// <summary>
/// Semantic theme colors derived from seed tokens and appearance mode,
/// matching macOS ThemeColors.
/// </summary>
public sealed class ThemeColors
{
    public SeedTokens Seed { get; }
    public bool IsDark { get; }

    public ThemeColors(SeedTokens seed, bool isDark = true)
    {
        Seed = seed;
        IsDark = isDark;
    }

    // Text hierarchy
    public Color Text => Seed.Fg;
    public Color TextSecondary => Seed.Fg.WithOpacity(0.6);
    public Color TextMuted => Seed.Fg.WithOpacity(0.35);

    // Primary
    public Color Primary => Seed.Primary;
    public Color PrimaryHover => IsDark ? Seed.Primary.WithOpacity(0.85) : Seed.Primary.WithOpacity(0.8);
    public Color PrimarySubtle => Seed.Primary.WithOpacity(0.14);

    // Accent
    public Color Accent => Seed.Accent;
    public Color AccentSubtle => Seed.Accent.WithOpacity(0.14);

    // Functional colors (static — not derived from seed)
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

    // Glass material — adaptive for dark/light
    public Color GlassBg => IsDark ? Colors.White.WithOpacity(0.055) : Colors.Black.WithOpacity(0.04);
    public Color GlassBgHeavy => IsDark ? Colors.White.WithOpacity(0.09) : Colors.Black.WithOpacity(0.06);
    public Color GlassBgTabbar => IsDark ? ColorHelper.FromHex("#101420").WithOpacity(0.72) : Colors.White.WithOpacity(0.72);
    public Color GlassBorder => IsDark ? Colors.White.WithOpacity(0.09) : Colors.Black.WithOpacity(0.10);
    public Color GlassBorderLight => IsDark ? Colors.White.WithOpacity(0.15) : Colors.Black.WithOpacity(0.15);

    // Highlight gradient — adaptive
    public Color HighlightGradientStart => IsDark ? Colors.White.WithOpacity(0.08) : Colors.White.WithOpacity(0.7);
    public Color HighlightGradientEnd => Colors.Transparent;

    // Hover background
    public Color HoverBg => IsDark ? Colors.White.WithOpacity(0.02) : Colors.Black.WithOpacity(0.03);

    // Corner radii (scaled from seed radius)
    public double RadiusSM => Seed.Radius * 0.5;
    public double RadiusMD => Seed.Radius;
    public double RadiusLG => Seed.Radius * 1.4;
    public static double RadiusPill => 999;

    // Shadows — adaptive for dark/light
    public ThemeShadow ShadowGlass => IsDark
        ? new(Colors.Black.WithOpacity(0.3), 32, 8)
        : new(Colors.Black.WithOpacity(0.08), 20, 4);
    public ThemeShadow ShadowGlassSM => IsDark
        ? new(Colors.Black.WithOpacity(0.2), 16, 4)
        : new(Colors.Black.WithOpacity(0.05), 10, 2);
    public ThemeShadow ShadowTabbar => IsDark
        ? new(Colors.Black.WithOpacity(0.4), 40, -4)
        : new(Colors.Black.WithOpacity(0.10), 20, -2);

    // Animation
    public static double EaseHarmonyResponse => 0.35;
    public static double EaseHarmonyDamping => 0.75;

    // Font
    public static string FontMono => "Cascadia Code";
    public static double TabbarHeight => 72;
}

// ═══════════════════════════════════════════════
// Theme Manager — INotifyPropertyChanged with persistence
// ═══════════════════════════════════════════════

/// <summary>
/// Manages theme preset switching, appearance mode, and persistence.
/// Persists to the Windows Registry (HKCU\Software\UniviewCodeBuddyProxy).
/// </summary>
public sealed class ThemeManager : INotifyPropertyChanged
{
    private ThemePreset _preset;
    private AppearanceMode _appearanceMode;
    private ThemeColors _colors;
    private bool _systemIsDark = true;

    public ThemeManager()
    {
        var savedPreset = LoadSavedPreset();
        var savedAppearance = LoadSavedAppearanceMode();
        _preset = savedPreset;
        _appearanceMode = savedAppearance;
        _colors = new ThemeColors(SeedTokens.ForPreset(savedPreset), IsDark);
    }

    public ThemePreset Preset
    {
        get => _preset;
        set
        {
            if (_preset == value) return;
            _preset = value;
            RebuildColors();
            SavePreset(value);
            OnPropertyChanged();
            OnPropertyChanged(nameof(Colors));
        }
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
    /// Returns the primary color for a given theme preset (for theme preview swatches).
    /// </summary>
    public Color GetPreviewColor(ThemePreset preset) => SeedTokens.ForPreset(preset).Primary;

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
        _colors = new ThemeColors(SeedTokens.ForPreset(_preset), IsDark);
    }

    public event PropertyChangedEventHandler? PropertyChanged;

    private void OnPropertyChanged([CallerMemberName] string? name = null)
        => PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(name));

    private const string RegistryKey = @"Software\UniviewCodeBuddyProxy";
    private const string PresetValue = "ThemePreset";
    private const string AppearanceValue = "AppearanceMode";

    private static ThemePreset LoadSavedPreset()
    {
        try
        {
            using var key = Microsoft.Win32.Registry.CurrentUser.OpenSubKey(RegistryKey);
            var saved = key?.GetValue(PresetValue) as string;

            return saved switch
            {
                nameof(ThemePreset.Deep) => ThemePreset.Deep,
                nameof(ThemePreset.Bright) => ThemePreset.Bright,
                nameof(ThemePreset.Midnight) => ThemePreset.Midnight,
                nameof(ThemePreset.Sunset) => ThemePreset.Sunset,
                _ => ThemePreset.Deep
            };
        }
        catch
        {
            return ThemePreset.Deep;
        }
    }

    private static void SavePreset(ThemePreset preset)
    {
        try
        {
            using var key = Microsoft.Win32.Registry.CurrentUser.CreateSubKey(RegistryKey);
            key?.SetValue(PresetValue, preset.ToString());
        }
        catch
        {
            // Silently ignore if registry write fails
        }
    }

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
/// Shadow definition for glass-morphism effects.
/// </summary>
public sealed class ThemeShadow
{
    public Color Color { get; }
    public double Radius { get; }
    public double OffsetY { get; }

    public ThemeShadow(Color color, double radius, double offsetY)
    {
        Color = color;
        Radius = radius;
        OffsetY = offsetY;
    }
}

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
    /// Converts a Color to a hex string like "#RRGGBB".
    /// </summary>
    public static string ToHex(Color color)
    {
        return $"#{color.R:X2}{color.G:X2}{color.B:X2}";
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
