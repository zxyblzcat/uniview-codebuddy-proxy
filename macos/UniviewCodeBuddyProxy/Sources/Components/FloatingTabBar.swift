import SwiftUI

/// Bottom floating tab bar with capsule shape.
/// Displays 5 tabs: Dashboard, Models, Tokens, Logs, Settings.
/// The active tab has an elevated glass highlight with accent color.
struct FloatingTabBar: View {
    @EnvironmentObject var themeManager: ThemeManager

    @Binding var selectedIndex: Int

    private let tabs: [(label: String, icon: String)] = [
        ("仪表盘", "square.grid.2x2"),
        ("模型",    "cpu"),
        ("令牌",    "key.fill"),
        ("日志",      "list.bullet"),
        ("设置",  "gearshape.fill"),
    ]

    var body: some View {
        HStack(spacing: 4) {
            ForEach(tabs.indices, id: \.self) { index in
                tabItem(index: index)
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(
            Capsule()
                .fill(.ultraThinMaterial)
                .overlay(
                    Capsule()
                        .stroke(themeManager.colors.glassBorder, lineWidth: 1)
                )
                .shadow(color: themeManager.colors.shadowTabbarColor, radius: themeManager.colors.shadowTabbarRadius, y: themeManager.colors.shadowTabbarY)
        )
        .padding(.bottom, 16)
    }

    @ViewBuilder
    private func tabItem(index: Int) -> some View {
        let isSelected = selectedIndex == index

        Button {
            selectedIndex = index
        } label: {
            VStack(spacing: 4) {
                Image(systemName: tabs[index].icon)
                    .font(.system(size: 18))
                Text(tabs[index].label)
                    .font(.system(size: 10, weight: .medium))
            }
            .foregroundStyle(isSelected ? themeManager.colors.primary : themeManager.colors.textMuted)
            .frame(width: 72, height: 48)
            .contentShape(Rectangle())
            .background(
                RoundedRectangle(cornerRadius: 12)
                    .fill(isSelected ? themeManager.colors.primarySubtle : .clear)
            )
            .overlay(
                RoundedRectangle(cornerRadius: 12)
                    .stroke(isSelected ? themeManager.colors.primary.opacity(0.3) : .clear, lineWidth: 1)
            )
            .scaleEffect(isSelected ? 1.05 : 1.0)
        }
        .buttonStyle(TabButtonStyle())
    }
}

/// 自定义按钮样式：无默认高亮，确保单击立即响应
private struct TabButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .opacity(configuration.isPressed ? 0.7 : 1.0)
            .animation(.easeInOut(duration: 0.15), value: configuration.isPressed)
    }
}

#if DEBUG
struct FloatingTabBar_Previews: PreviewProvider {
    static var previews: some View {        return
    VStack {
        Spacer()
        FloatingTabBar(selectedIndex: .constant(0))
    }
    .frame(width: 500, height: 120)
    .environmentObject(ThemeManager())
    }
}
#endif
