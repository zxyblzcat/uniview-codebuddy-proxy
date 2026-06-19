import SwiftUI

/// Reusable glass-morphism card container.
/// Wraps any content in a semi-transparent blurred card
/// with an optional header row (icon + title).
struct GlassCard<Content: View>: View {
    @EnvironmentObject var themeManager: ThemeManager

    let padding: CGFloat
    let cornerRadius: CGFloat
    let headerIcon: String?
    let headerTitle: String?
    let content: Content

    init(
        padding: CGFloat = 20,
        cornerRadius: CGFloat = 20,
        headerIcon: String? = nil,
        headerTitle: String? = nil,
        @ViewBuilder content: () -> Content
    ) {
        self.padding = padding
        self.cornerRadius = cornerRadius
        self.headerIcon = headerIcon
        self.headerTitle = headerTitle
        self.content = content()
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if let icon = headerIcon, let title = headerTitle {
                HStack(spacing: 8) {
                    Image(systemName: icon)
                        .font(.system(size: 14, weight: .semibold))
                        .foregroundStyle(themeManager.colors.primary)
                    Text(title)
                        .font(.system(size: 15, weight: .semibold))
                        .foregroundStyle(themeManager.colors.text)
                    Spacer()
                }
                .padding(.horizontal, padding)
                .padding(.top, padding)
                .padding(.bottom, 12)

                Divider()
                    .background(themeManager.colors.glassBorder)
                    .padding(.horizontal, padding)
            }

            content
                .padding(padding)
        }
        .background(
            RoundedRectangle(cornerRadius: cornerRadius)
                .fill(.ultraThinMaterial)
                .overlay(
                    RoundedRectangle(cornerRadius: cornerRadius)
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
        .clipShape(RoundedRectangle(cornerRadius: cornerRadius))
    }
}

#if DEBUG
struct GlassCard_Previews: PreviewProvider {
    static var previews: some View {        return
    GlassCard(headerIcon: "chart.bar.fill", headerTitle: "概览") {
        Text("Card content goes here")
            .foregroundStyle(.white)
    }
    .frame(width: 320)
    .padding()
    .environmentObject(ThemeManager())
    }
}
#endif
