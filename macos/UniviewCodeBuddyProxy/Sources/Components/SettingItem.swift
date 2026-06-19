import SwiftUI

// ═══════════════════════════════════════════════
// SettingItem — 可复用的设置行组件
// ═══════════════════════════════════════════════

struct SettingItem<Content: View>: View {
    @EnvironmentObject var themeManager: ThemeManager

    let label: String
    let icon: String
    var tooltip: String?

    @ViewBuilder let content: Content

    @State private var isHovered = false

    var body: some View {
        let colors = themeManager.colors

        HStack(spacing: 12) {
            // 图标
            Image(systemName: icon)
                .font(.system(size: 16, weight: .medium))
                .foregroundStyle(colors.primary)
                .frame(width: 24, height: 24)

            // 标签文本
            Text(label)
                .font(.system(size: 13, weight: .medium))
                .foregroundStyle(colors.text)

            Spacer()

            // 右侧内容（右对齐）
            content
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
        .background(
            RoundedRectangle(cornerRadius: colors.radiusSM)
                .fill(isHovered ? colors.glassBgHeavy : Color.clear)
        )
        .overlay(alignment: .bottom) {
            // 底部细分隔线
            Rectangle()
                .fill(colors.glassBorder.opacity(0.5))
                .frame(height: 0.5)
                .padding(.horizontal, 16)
        }
        .contentShape(Rectangle())
        .onHover { hovering in
            withAnimation(ThemeColors.easeHarmony) {
                isHovered = hovering
            }
        }
        .help(tooltip ?? "")
    }
}
