import SwiftUI

/// Styled button with glass-morphism aesthetics.
/// Two variants: primary (accent fill) and secondary (glass outline).
/// Supports hover lift and disabled states.
struct GlassButton: View {
    @EnvironmentObject var themeManager: ThemeManager

    let title: String
    let icon: String?
    let variant: Variant
    let shape: Shape
    let isDisabled: Bool
    let action: () -> Void

    enum Variant {
        case primary
        case secondary
    }

    enum Shape {
        case capsule
        case rounded
    }

    init(
        _ title: String,
        icon: String? = nil,
        variant: Variant = .primary,
        shape: Shape = .capsule,
        isDisabled: Bool = false,
        action: @escaping () -> Void
    ) {
        self.title = title
        self.icon = icon
        self.variant = variant
        self.shape = shape
        self.isDisabled = isDisabled
        self.action = action
    }

    @State private var isHovered = false

    var body: some View {
        Button(action: action) {
            HStack(spacing: 6) {
                if let icon = icon {
                    Image(systemName: icon)
                        .font(.system(size: 13, weight: .semibold))
                }
                Text(title)
                    .font(.system(size: 14, weight: .semibold))
            }
            .foregroundStyle(labelColor)
            .padding(.horizontal, 20)
            .padding(.vertical, 10)
            .background(backgroundView)
            .clipShape(shape == .capsule ? AnyShape(Capsule()) : AnyShape(RoundedRectangle(cornerRadius: themeManager.colors.radiusSM)))
            .offset(y: isHovered && !isDisabled ? -2 : 0)
            .animation(ThemeColors.easeHarmony, value: isHovered)
        }
        .buttonStyle(.plain)
        .disabled(isDisabled)
        .opacity(isDisabled ? 0.45 : 1.0)
        .onHover { hovering in
            isHovered = hovering
        }
    }

    @ViewBuilder
    private var backgroundView: some View {
        switch variant {
        case .primary:
            Group {
                if shape == .capsule {
                    Capsule()
                        .fill(themeManager.colors.primary)
                } else {
                    RoundedRectangle(cornerRadius: themeManager.colors.radiusSM)
                        .fill(themeManager.colors.primary)
                }
            }
            .overlay(
                Group {
                    if shape == .capsule {
                        Capsule()
                            .stroke(themeManager.colors.primaryHover, lineWidth: 1)
                    } else {
                        RoundedRectangle(cornerRadius: themeManager.colors.radiusSM)
                            .stroke(themeManager.colors.primaryHover, lineWidth: 1)
                    }
                }
            )
            .shadow(color: themeManager.colors.primary.opacity(0.3), radius: isHovered ? 16 : 8, y: isHovered ? 6 : 3)

        case .secondary:
            Group {
                if shape == .capsule {
                    Capsule()
                        .fill(.ultraThinMaterial)
                } else {
                    RoundedRectangle(cornerRadius: themeManager.colors.radiusSM)
                        .fill(.ultraThinMaterial)
                }
            }
            .overlay(
                Group {
                    if shape == .capsule {
                        Capsule()
                            .stroke(themeManager.colors.glassBorderLight, lineWidth: 1)
                    } else {
                        RoundedRectangle(cornerRadius: themeManager.colors.radiusSM)
                            .stroke(themeManager.colors.glassBorderLight, lineWidth: 1)
                    }
                }
            )
        }
    }

    private var labelColor: Color {
        switch variant {
        case .primary:  return .white
        case .secondary: return themeManager.colors.text
        }
    }
}

/// Type-erased shape helper for conditional clipping.
private struct AnyShape: Shape {
    private let _path: @Sendable (CGRect) -> Path

    init<S: Shape>(_ shape: S) {
        _path = { rect in shape.path(in: rect) }
    }

    func path(in rect: CGRect) -> Path {
        _path(rect)
    }
}

#if DEBUG
struct GlassButton_Previews: PreviewProvider {
    static var previews: some View {        return
    VStack(spacing: 16) {
        GlassButton("Save Changes", icon: "checkmark", variant: .primary) {}
        GlassButton("Cancel", variant: .secondary) {}
        GlassButton("Disabled", variant: .primary, isDisabled: true) {}
        GlassButton("Rounded", variant: .secondary, shape: .rounded) {}
    }
    .padding()
    .environmentObject(ThemeManager())
    }
}
#endif
