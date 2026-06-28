// ═══════════════════════════════════════════════════════════════════════════════
// CsWinRT Polyfill — Microsoft.UI.Color & Microsoft.UI.Colors
// ═══════════════════════════════════════════════════════════════════════════════
//
// When building with Visual Studio locally, the CsWinRT source generator produces
// the Microsoft.UI.Color struct and Microsoft.UI.Colors static class from the
// Windows App SDK .winmd metadata. The HAS_CSWINRT_COLOR compilation symbol is
// defined in that scenario and this file compiles to nothing.
//
// On CI (GitHub Actions windows-2022), the CsWinRT infrastructure fails to
// activate: the source generator doesn't run and the projected types are never
// emitted, causing CS0246. This file provides layout-compatible replacements so
// the rest of the codebase can use `using Microsoft.UI;` unchanged.
//
// Layout compatibility: WinRT Color is a value type with four byte fields
// (A, R, G, B) in that order, totaling 4 bytes. Our struct mirrors this exactly,
// so Unsafe.As reinterpret casts (if ever needed) are safe.
// ═══════════════════════════════════════════════════════════════════════════════

#if !HAS_CSWINRT_COLOR

namespace Microsoft.UI;

/// <summary>
/// Layout-compatible polyfill for WinRT's Microsoft.UI.Color value type.
/// Represents an ARGB color (alpha, red, green, blue).
/// </summary>
public struct Color : IEquatable<Color>
{
    public byte A;
    public byte R;
    public byte G;
    public byte B;

    public Color(byte a, byte r, byte g, byte b)
    {
        A = a;
        R = r;
        G = g;
        B = b;
    }

    /// <summary>
    /// Creates a Color from ARGB components. Matches the WinRT Color.FromArgb signature.
    /// </summary>
    public static Color FromArgb(byte a, byte r, byte g, byte b) => new(a, r, g, b);

    public bool Equals(Color other) => A == other.A && R == other.R && G == other.G && B == other.B;

    public override bool Equals(object? obj) => obj is Color other && Equals(other);

    public override int GetHashCode() => HashCode.Combine(A, R, G, B);

    public static bool operator ==(Color left, Color right) => left.Equals(right);

    public static bool operator !=(Color left, Color right) => !left.Equals(right);

    public override string ToString() => $"#{A:X2}{R:X2}{G:X2}{B:X2}";
}

/// <summary>
/// Polyfill for WinRT's Microsoft.UI.Colors static class.
/// Provides the named color constants used in this codebase.
/// </summary>
public static class Colors
{
    public static Color White => new(255, 255, 255, 255);
    public static Color Black => new(255, 0, 0, 0);
    public static Color Transparent => new(0, 255, 255, 255);
    public static Color Gray => new(255, 128, 128, 128);
}

#endif // !HAS_CSWINRT_COLOR
