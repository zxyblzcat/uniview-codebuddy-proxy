import SwiftUI

// ═══════════════════════════════════════════════
// GlassPopupPicker — 液态玻璃弹出选择器
// 设计稿规格：自定义下拉框，匹配液态玻璃风格
//
// 使用 .popover 实现，弹出层脱离当前视图层级，
// 不受 .clipShape 裁剪，不受兄弟视图遮挡
// ═══════════════════════════════════════════════

struct GlassPopupPicker<T: Hashable>: View {
    @EnvironmentObject var themeManager: ThemeManager

    let options: [GlassPopupOption<T>]
    @Binding var selection: T
    var width: CGFloat = 180

    @State private var isExpanded = false

    private var selectedOption: GlassPopupOption<T>? {
        options.first { $0.value == selection }
    }

    var body: some View {
        let c = themeManager.colors

        // 触发按钮
        Button {
            isExpanded.toggle()
        } label: {
            HStack(spacing: 8) {
                Text(selectedOption?.label ?? "—")
                    .font(.system(size: 12, weight: .medium))
                    .foregroundStyle(c.text)
                    .lineLimit(1)

                Spacer()

                Image(systemName: "chevron.down")
                    .font(.system(size: 10, weight: .semibold))
                    .foregroundStyle(c.textMuted)
                    .rotationEffect(.degrees(isExpanded ? 180 : 0))
                    .animation(ThemeColors.easeHarmony, value: isExpanded)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
            .frame(width: width)
            .background(
                RoundedRectangle(cornerRadius: c.radiusSM)
                    .fill(c.glassBgHeavy)
                    .overlay(
                        RoundedRectangle(cornerRadius: c.radiusSM)
                            .stroke(isExpanded ? c.primary : c.glassBorder, lineWidth: 1)
                    )
            )
            .shadow(
                color: isExpanded ? c.primary.opacity(0.12) : .clear,
                radius: isExpanded ? 8 : 0
            )
        }
        .buttonStyle(.plain)
        .popover(isPresented: $isExpanded, arrowEdge: .bottom) {
            dropdownContent
        }
    }

    // MARK: - 下拉列表内容

    private var dropdownContent: some View {
        let c = themeManager.colors

        return VStack(spacing: 0) {
            ForEach(options) { option in
                let isSelected = selection == option.value

                Button {
                    selection = option.value
                    isExpanded = false
                } label: {
                    HStack(spacing: 8) {
                        if isSelected {
                            Image(systemName: "checkmark")
                                .font(.system(size: 11, weight: .bold))
                                .foregroundStyle(c.primary)
                                .frame(width: 14)
                        } else {
                            Color.clear
                                .frame(width: 14)
                        }

                        Text(option.label)
                            .font(.system(size: 12, weight: isSelected ? .semibold : .regular))
                            .foregroundStyle(isSelected ? c.text : c.textSecondary)
                            .lineLimit(1)

                        Spacer()

                        if !option.subtitle.isEmpty {
                            Text(option.subtitle)
                                .font(.system(size: 10))
                                .foregroundStyle(c.textMuted)
                        }
                    }
                    .padding(.horizontal, 12)
                    .padding(.vertical, 9)
                    .background(
                        RoundedRectangle(cornerRadius: 6)
                            .fill(isSelected ? c.primarySubtle : Color.clear)
                    )
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)

                if option.id != options.last?.id {
                    Divider()
                        .background(c.glassBorder.opacity(0.3))
                        .padding(.horizontal, 8)
                }
            }
        }
        .padding(.vertical, 6)
        .frame(width: width)
        .background(Color(NSColor.controlBackgroundColor))
    }
}

// ═══════════════════════════════════════════════
// 选项模型
// ═══════════════════════════════════════════════

struct GlassPopupOption<T: Hashable>: Identifiable {
    let id: String
    let label: String
    let subtitle: String
    let value: T

    init(label: String, subtitle: String = "", value: T) {
        self.id = "\(label)-\(value)"
        self.label = label
        self.subtitle = subtitle
        self.value = value
    }
}
