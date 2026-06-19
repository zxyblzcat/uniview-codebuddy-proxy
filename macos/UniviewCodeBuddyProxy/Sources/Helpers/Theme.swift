import SwiftUI

// ═══════════════════════════════════════════════
// 外观模式 — 深色/浅色/跟随系统
// ═══════════════════════════════════════════════

enum AppearanceMode: String, CaseIterable {
    case system = "跟随系统"
    case light = "浅色"
    case dark = "深色"
}

// ═══════════════════════════════════════════════
// 种子令牌 — 所有色彩派生的基础
// ═══════════════════════════════════════════════

enum ThemePreset: String, CaseIterable {
    case deep = "深邃"
    case bright = "明亮"
    case midnight = "午夜"
    case sunset = "日落"
}

struct SeedTokens {
    let bg: Color
    let fg: Color
    let primary: Color
    let accent: Color
    let surface: Color
    let radius: CGFloat

    static let deep = SeedTokens(
        bg: Color(hex: "0B0F19"),
        fg: Color(hex: "E8ECF4"),
        primary: Color(hex: "5B9CF6"),
        accent: Color(hex: "34D4AA"),
        surface: Color(hex: "131926"),
        radius: 20
    )

    static let bright = SeedTokens(
        bg: Color(hex: "F5F7FA"),
        fg: Color(hex: "1A1D26"),
        primary: Color(hex: "5B9CF6"),
        accent: Color(hex: "34D4AA"),
        surface: Color(hex: "FFFFFF"),
        radius: 20
    )

    static let midnight = SeedTokens(
        bg: Color(hex: "050709"),
        fg: Color(hex: "E8ECF4"),
        primary: Color(hex: "6366F1"),
        accent: Color(hex: "34D4AA"),
        surface: Color(hex: "0A0C10"),
        radius: 20
    )

    static let sunset = SeedTokens(
        bg: Color(hex: "0F1119"),
        fg: Color(hex: "E8ECF4"),
        primary: Color(hex: "F97316"),
        accent: Color(hex: "34D4AA"),
        surface: Color(hex: "161820"),
        radius: 20
    )

    static func forPreset(_ preset: ThemePreset) -> SeedTokens {
        switch preset {
        case .deep: return .deep
        case .bright: return .bright
        case .midnight: return .midnight
        case .sunset: return .sunset
        }
    }
}

// ═══════════════════════════════════════════════
// 语义色彩 — 从种子令牌 + 外观模式派生
// ═══════════════════════════════════════════════

struct ThemeColors {
    // 种子
    let seed: SeedTokens
    let isDark: Bool

    init(seed: SeedTokens, isDark: Bool = true) {
        self.seed = seed
        self.isDark = isDark
    }

    // 文本层级
    var text: Color { seed.fg }
    var textSecondary: Color { seed.fg.opacity(0.6) }
    var textMuted: Color { seed.fg.opacity(0.35) }

    // 主色
    var primary: Color { seed.primary }
    var primaryHover: Color {
        // 基于主色变暗，而非硬编码
        isDark ? seed.primary.opacity(0.85) : seed.primary.opacity(0.8)
    }
    var primarySubtle: Color { seed.primary.opacity(0.14) }

    // 强调色
    var accent: Color { seed.accent }
    var accentSubtle: Color { seed.accent.opacity(0.14) }

    // 功能色
    static let success = Color(hex: "4ADE80")
    static let successSubtle = Color(hex: "4ADE80").opacity(0.12)
    static let warning = Color(hex: "FBBF24")
    static let warningSubtle = Color(hex: "FBBF24").opacity(0.12)
    static let danger = Color(hex: "F87171")
    static let dangerSubtle = Color(hex: "F87171").opacity(0.12)
    static let info = Color(hex: "60A5FA")
    static let infoSubtle = Color(hex: "60A5FA").opacity(0.12)

    // 紫色
    static let purple = Color(hex: "A78BFA")
    static let purpleSubtle = Color(hex: "A78BFA").opacity(0.10)

    // 玻璃材质 — 自适应深色/浅色
    var glassBg: Color { isDark ? Color.white.opacity(0.055) : Color.black.opacity(0.04) }
    var glassBgHeavy: Color { isDark ? Color.white.opacity(0.09) : Color.black.opacity(0.06) }
    var glassBgTabbar: Color { isDark ? Color(hex: "101420").opacity(0.72) : Color.white.opacity(0.72) }
    var glassBorder: Color { isDark ? Color.white.opacity(0.09) : Color.black.opacity(0.10) }
    var glassBorderLight: Color { isDark ? Color.white.opacity(0.15) : Color.black.opacity(0.15) }

    // 高光线渐变 — 自适应
    var highlightGradient: [Color] {
        isDark ? [.white.opacity(0.08), .clear] : [.white.opacity(0.7), .clear]
    }

    // 悬浮背景
    var hoverBg: Color { isDark ? Color.white.opacity(0.02) : Color.black.opacity(0.03) }

    // 圆角
    var radiusSM: CGFloat { seed.radius * 0.5 }
    var radiusMD: CGFloat { seed.radius }
    var radiusLG: CGFloat { seed.radius * 1.4 }
    static let radiusPill: CGFloat = 999

    // 阴影 — 自适应深色/浅色
    var shadowGlassColor: Color { isDark ? .black.opacity(0.3) : .black.opacity(0.08) }
    var shadowGlassRadius: CGFloat { isDark ? 32 : 20 }
    var shadowGlassY: CGFloat { isDark ? 8 : 4 }

    var shadowGlassSMColor: Color { isDark ? .black.opacity(0.2) : .black.opacity(0.05) }
    var shadowGlassSMRadius: CGFloat { isDark ? 16 : 10 }
    var shadowGlassSMY: CGFloat { isDark ? 4 : 2 }

    var shadowTabbarColor: Color { isDark ? .black.opacity(0.4) : .black.opacity(0.10) }
    var shadowTabbarRadius: CGFloat { isDark ? 40 : 20 }
    var shadowTabbarY: CGFloat { isDark ? -4 : -2 }

    // 兼容旧接口的 Shadow 结构体
    var shadowGlass: Shadow { Shadow(color: shadowGlassColor, radius: shadowGlassRadius, y: shadowGlassY) }
    var shadowGlassSM: Shadow { Shadow(color: shadowGlassSMColor, radius: shadowGlassSMRadius, y: shadowGlassSMY) }
    var shadowTabbar: Shadow { Shadow(color: shadowTabbarColor, radius: shadowTabbarRadius, y: shadowTabbarY) }

    // 动画
    static let easeHarmony = Animation.spring(response: 0.35, dampingFraction: 0.75)

    // 字体
    static let fontMono = "SF Mono"
    static let tabbarHeight: CGFloat = 72
}

// ═══════════════════════════════════════════════
// 环境主题
// ═══════════════════════════════════════════════

class ThemeManager: ObservableObject {
    @Published var preset: ThemePreset {
        didSet {
            rebuildColors()
            UserDefaults.standard.set(preset.rawValue, forKey: "themePreset")
        }
    }
    @Published var appearanceMode: AppearanceMode {
        didSet {
            rebuildColors()
            UserDefaults.standard.set(appearanceMode.rawValue, forKey: "appearanceMode")
        }
    }
    @Published var systemColorScheme: ColorScheme = .dark
    @Published var colors: ThemeColors

    /// 当前是否为深色模式（综合 appearanceMode + systemColorScheme）
    var isDark: Bool {
        switch appearanceMode {
        case .dark: return true
        case .light: return false
        case .system: return systemColorScheme == .dark
        }
    }

    /// 传给 .preferredColorScheme 的值，nil 表示跟随系统
    var effectiveColorScheme: ColorScheme? {
        switch appearanceMode {
        case .dark: return .dark
        case .light: return .light
        case .system: return nil
        }
    }

    /// 用于 GlassSegmentedPicker 绑定的 raw value
    var appearanceModeRaw: String {
        get { appearanceMode.rawValue }
        set { appearanceMode = AppearanceMode(rawValue: newValue) ?? .system }
    }

    init() {
        let savedPreset = UserDefaults.standard.string(forKey: "themePreset") ?? "深邃"
        let preset = ThemePreset(rawValue: savedPreset) ?? .deep

        let savedAppearance = UserDefaults.standard.string(forKey: "appearanceMode") ?? "跟随系统"
        let appearance = AppearanceMode(rawValue: savedAppearance) ?? .system

        self.preset = preset
        self.appearanceMode = appearance
        // 初始化时假设深色（系统外观由 ContentView 的 onChange 推送）
        self.systemColorScheme = .dark
        self.colors = ThemeColors(
            seed: SeedTokens.forPreset(preset),
            isDark: appearance == .dark || (appearance == .system) // system 默认深色
        )
    }

    /// 系统外观变化时调用
    func updateSystemColorScheme(_ scheme: ColorScheme) {
        systemColorScheme = scheme
        if appearanceMode == .system {
            rebuildColors()
        }
    }

    private func rebuildColors() {
        colors = ThemeColors(
            seed: SeedTokens.forPreset(preset),
            isDark: isDark
        )
    }
}

// ═══════════════════════════════════════════════
// Color 扩展 — 支持 hex 初始化
// ═══════════════════════════════════════════════

extension Color {
    init(hex: String) {
        let hex = hex.trimmingCharacters(in: CharacterSet.alphanumerics.inverted)
        var int: UInt64 = 0
        Scanner(string: hex).scanHexInt64(&int)
        let a, r, g, b: UInt64
        switch hex.count {
        case 6:
            (a, r, g, b) = (255, int >> 16, int >> 8 & 0xFF, int & 0xFF)
        case 8:
            (a, r, g, b) = (int >> 24, int >> 16 & 0xFF, int >> 8 & 0xFF, int & 0xFF)
        default:
            (a, r, g, b) = (255, 0, 0, 0)
        }
        self.init(
            .sRGB,
            red: Double(r) / 255,
            green: Double(g) / 255,
            blue: Double(b) / 255,
            opacity: Double(a) / 255
        )
    }
}

// ═══════════════════════════════════════════════
// View 扩展 — 液态玻璃效果
// ═══════════════════════════════════════════════

extension View {
    /// 标准玻璃卡片背景
    func glassCardBackground(colors: ThemeColors) -> some View {
        self.background(
            RoundedRectangle(cornerRadius: colors.radiusMD)
                .fill(.ultraThinMaterial)
                .overlay(
                    RoundedRectangle(cornerRadius: colors.radiusMD)
                        .stroke(colors.glassBorder, lineWidth: 1)
                )
                .overlay(alignment: .top) {
                    LinearGradient(
                        colors: colors.highlightGradient,
                        startPoint: .leading,
                        endPoint: .trailing
                    )
                    .frame(height: 1)
                    .padding(.horizontal, 20)
                }
                .shadow(color: colors.shadowGlassColor, radius: colors.shadowGlassRadius, y: colors.shadowGlassY)
        )
    }

    /// KPI 卡片悬停抬升效果
    func hoverLift(isHovered: Bool) -> some View {
        self.offset(y: isHovered ? -3 : 0)
            .animation(ThemeColors.easeHarmony, value: isHovered)
    }
}

// ═══════════════════════════════════════════════
// Shadow 辅助
// ═══════════════════════════════════════════════

struct Shadow {
    let color: Color
    let radius: CGFloat
    let y: CGFloat
}

// ═══════════════════════════════════════════════
