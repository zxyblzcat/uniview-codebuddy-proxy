import SwiftUI

/// Key Performance Indicator card.
/// Displays a title, large animated value, and optional
/// subtitle with a trend arrow and a colored accent line at the top.
struct KPICard: View {
    @EnvironmentObject var themeManager: ThemeManager

    let title: String
    let value: String
    let subtitle: String?
    let trend: KPITrend
    let accentColor: Color

    enum KPITrend {
        case up, down, neutral
    }

    init(
        title: String,
        value: String,
        subtitle: String? = nil,
        trend: KPITrend = .neutral,
        accentColor: Color? = nil
    ) {
        self.title = title
        self.value = value
        self.subtitle = subtitle
        self.trend = trend
        self.accentColor = accentColor ?? Color.accentColor
    }

    @State private var isHovered = false

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            // Title row
            Text(title)
                .font(.system(size: 13, weight: .medium))
                .foregroundStyle(themeManager.colors.textSecondary)

            // Value
            Text(value)
                .font(.system(size: 28, weight: .bold, design: .rounded))
                .foregroundStyle(themeManager.colors.text)
                .contentTransition(.numericText())
                .animation(ThemeColors.easeHarmony, value: value)

            // Subtitle + trend
            if let subtitle = subtitle {
                HStack(spacing: 4) {
                    trendIcon
                    Text(subtitle)
                        .font(.system(size: 12, weight: .medium))
                        .foregroundStyle(trendColor)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(20)
        .background(
            VStack {
                // Accent line at top
                accentColor
                    .frame(height: 3)
                    .clipShape(UnevenRoundedRectangle(bottomLeadingRadius: 6, bottomTrailingRadius: 6))
                Spacer()
            }
        )
        .background(
            RoundedRectangle(cornerRadius: themeManager.colors.radiusMD)
                .fill(.ultraThinMaterial)
                .overlay(
                    RoundedRectangle(cornerRadius: themeManager.colors.radiusMD)
                        .stroke(themeManager.colors.glassBorder, lineWidth: 1)
                )
                .overlay(alignment: .top) {
                    LinearGradient(
                        colors: themeManager.colors.highlightGradient,
                        startPoint: .leading,
                        endPoint: .trailing
                    )
                    .frame(height: 1)
                    .padding(.horizontal, 20)
                }
                .shadow(color: themeManager.colors.shadowGlassColor, radius: themeManager.colors.shadowGlassRadius, y: themeManager.colors.shadowGlassY)
        )
        .clipShape(RoundedRectangle(cornerRadius: themeManager.colors.radiusMD))
        .hoverLift(isHovered: isHovered)
        .onHover { hovering in
            isHovered = hovering
        }
    }

    @ViewBuilder
    private var trendIcon: some View {
        switch trend {
        case .up:
            Image(systemName: "arrow.up.right")
                .font(.system(size: 10, weight: .bold))
        case .down:
            Image(systemName: "arrow.down.right")
                .font(.system(size: 10, weight: .bold))
        case .neutral:
            Image(systemName: "arrow.right")
                .font(.system(size: 10, weight: .bold))
        }
    }

    private var trendColor: Color {
        switch trend {
        case .up: return ThemeColors.success
        case .down: return ThemeColors.danger
        case .neutral: return themeManager.colors.textSecondary
        }
    }
}

#if DEBUG
struct KPICard_Previews: PreviewProvider {
    static var previews: some View {        return
    HStack {
        KPICard(title: "请求数", value: "1,234", subtitle: "+12.5%", trend: .up, accentColor: ThemeColors.success)
        KPICard(title: "错误数", value: "42", subtitle: "-3.1%", trend: .down, accentColor: ThemeColors.danger)
        KPICard(title: "延迟", value: "89ms", subtitle: "无变化", trend: .neutral, accentColor: ThemeColors.info)
    }
    .padding()
    .environmentObject(ThemeManager())
    }
}
#endif
