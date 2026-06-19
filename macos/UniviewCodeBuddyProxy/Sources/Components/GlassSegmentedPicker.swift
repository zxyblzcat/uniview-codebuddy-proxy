import SwiftUI

// ═══════════════════════════════════════════════
// GlassSegmentedPicker — 液态玻璃分段选择器
// 设计稿规格：选中项 btn-primary，未选中项 btn-secondary
// ═══════════════════════════════════════════════

struct GlassSegmentedPicker<T: Hashable>: View {
    @EnvironmentObject var themeManager: ThemeManager

    let options: [GlassSegmentedOption<T>]
    @Binding var selection: T

    var body: some View {
        let c = themeManager.colors

        HStack(spacing: 0) {
            ForEach(Array(options.enumerated()), id: \.element.id) { index, option in
                let isSelected = selection == option.value

                Button {
                    withAnimation(ThemeColors.easeHarmony) {
                        selection = option.value
                    }
                } label: {
                    Text(option.label)
                        .font(.system(size: 12, weight: isSelected ? .semibold : .medium))
                        .foregroundStyle(isSelected ? .white : c.textSecondary)
                        .padding(.horizontal, 16)
                        .padding(.vertical, 6)
                        .background(
                            RoundedRectangle(cornerRadius: ThemeColors.radiusPill - 4)
                                .fill(isSelected
                                    ? LinearGradient(
                                        colors: [c.primary, c.primaryHover],
                                        startPoint: .topLeading,
                                        endPoint: .bottomTrailing
                                    )
                                    : LinearGradient(
                                        colors: [c.glassBgHeavy, c.glassBgHeavy],
                                        startPoint: .topLeading,
                                        endPoint: .bottomTrailing
                                    )
                                )
                        )
                        .overlay(
                            RoundedRectangle(cornerRadius: ThemeColors.radiusPill - 4)
                                .stroke(
                                    isSelected
                                        ? c.primary.opacity(0.25)
                                        : c.glassBorderLight,
                                    lineWidth: 1
                                )
                        )
                        .shadow(
                            color: isSelected ? c.primary.opacity(0.25) : .clear,
                            radius: isSelected ? 8 : 0,
                            y: isSelected ? 2 : 0
                        )
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .disabled(isSelected)
                .opacity(isSelected ? 1 : 0.7)
            }
        }
        .padding(3)
        .background(
            RoundedRectangle(cornerRadius: ThemeColors.radiusPill)
                .fill(c.glassBg)
                .overlay(
                    RoundedRectangle(cornerRadius: ThemeColors.radiusPill)
                        .stroke(c.glassBorder.opacity(0.5), lineWidth: 1)
                )
        )
    }
}

// ═══════════════════════════════════════════════
// 选项模型
// ═══════════════════════════════════════════════

struct GlassSegmentedOption<T: Hashable>: Identifiable {
    let id: String
    let label: String
    let value: T

    init(label: String, value: T) {
        self.id = "\(label)-\(value)"
        self.label = label
        self.value = value
    }
}
