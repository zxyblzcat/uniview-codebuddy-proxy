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
// 语义色彩 — 由外观模式（深色/浅色）驱动
// ═══════════════════════════════════════════════

struct ThemeColors {
    let isDark: Bool

    init(isDark: Bool = true) {
        self.isDark = isDark
    }

    // ── 基础色 ──

    /// 主背景色
    var bg: Color { isDark ? Color(hex: "0B0F19") : Color(hex: "F5F7FA") }
    /// 前景色（主文本）
    var fg: Color { isDark ? Color(hex: "E8ECF4") : Color(hex: "1A1D26") }
    /// 主色
    var primary: Color { Color(hex: "5B9CF6") }
    /// 强调色
    var accent: Color { Color(hex: "34D4AA") }
    /// 表面色（卡片/次级背景）
    var surface: Color { isDark ? Color(hex: "131926") : Color(hex: "FFFFFF") }

    // ── 文本层级 ──

    var text: Color { fg }
    var textSecondary: Color { fg.opacity(0.6) }
    var textMuted: Color { fg.opacity(0.35) }

    // ── 主色变体 ──

    var primaryHover: Color { isDark ? primary.opacity(0.85) : primary.opacity(0.8) }
    var primarySubtle: Color { primary.opacity(0.14) }

    // ── 强调色变体 ──

    var accentSubtle: Color { accent.opacity(0.14) }

    // ── 功能色 ──

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

    // ── 玻璃材质 — 自适应深色/浅色 ──

    var glassBg: Color { isDark ? Color.white.opacity(0.055) : Color.black.opacity(0.04) }
    var glassBgHeavy: Color { isDark ? Color.white.opacity(0.09) : Color.black.opacity(0.06) }
    var glassBgTabbar: Color { isDark ? Color(hex: "101420").opacity(0.72) : Color.white.opacity(0.72) }
    var glassBorder: Color { isDark ? Color.white.opacity(0.09) : Color.black.opacity(0.10) }
    var glassBorderLight: Color { isDark ? Color.white.opacity(0.15) : Color.black.opacity(0.15) }

    // ── 高光线渐变 — 自适应 ──

    var highlightGradient: [Color] {
        isDark ? [.white.opacity(0.08), .clear] : [.white.opacity(0.7), .clear]
    }

    // ── 悬浮背景 ──

    var hoverBg: Color { isDark ? Color.white.opacity(0.02) : Color.black.opacity(0.03) }

    // ── 圆角 ──

    static let radius: CGFloat = 20
    var radiusSM: CGFloat { Self.radius * 0.5 }
    var radiusMD: CGFloat { Self.radius }
    var radiusLG: CGFloat { Self.radius * 1.4 }
    static let radiusPill: CGFloat = 999

    // ── 阴影 — 自适应深色/浅色 ──

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

    // ── 动画 ──

    static let easeHarmony = Animation.spring(response: 0.35, dampingFraction: 0.75)

    // ── 字体 ──

    static let fontMono = "SF Mono"
    static let tabbarHeight: CGFloat = 72
}

// ═══════════════════════════════════════════════
// 环境主题
// ═══════════════════════════════════════════════

class ThemeManager: ObservableObject {
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
        let savedAppearance = UserDefaults.standard.string(forKey: "appearanceMode") ?? "跟随系统"
        let appearance = AppearanceMode(rawValue: savedAppearance) ?? .system

        self.appearanceMode = appearance
        // 初始化时假设深色（系统外观由 ContentView 的 onChange 推送）
        self.systemColorScheme = .dark
        self.colors = ThemeColors(isDark: appearance == .dark || (appearance == .system))
    }

    /// 系统外观变化时调用
    func updateSystemColorScheme(_ scheme: ColorScheme) {
        systemColorScheme = scheme
        if appearanceMode == .system {
            rebuildColors()
        }
    }

    private func rebuildColors() {
        colors = ThemeColors(isDark: isDark)
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
