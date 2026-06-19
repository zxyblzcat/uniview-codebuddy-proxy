import SwiftUI

/// Toast notification shown at the top of the window.
/// Auto-dismisses after 3 seconds. Supports success, error,
/// warning, and info types with matching icons and colors.
struct Toast: View {
    @EnvironmentObject var themeManager: ThemeManager

    let message: String
    let type: ToastType

    enum ToastType {
        case success, error, warning, info

        var icon: String {
            switch self {
            case .success: return "checkmark.circle.fill"
            case .error:   return "xmark.circle.fill"
            case .warning: return "exclamationmark.triangle.fill"
            case .info:    return "info.circle.fill"
            }
        }

        var color: Color {
            switch self {
            case .success: return ThemeColors.success
            case .error:   return ThemeColors.danger
            case .warning: return ThemeColors.warning
            case .info:    return ThemeColors.info
            }
        }

        var subtleColor: Color {
            switch self {
            case .success: return ThemeColors.successSubtle
            case .error:   return ThemeColors.dangerSubtle
            case .warning: return ThemeColors.warningSubtle
            case .info:    return ThemeColors.infoSubtle
            }
        }
    }

    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: type.icon)
                .font(.system(size: 18, weight: .semibold))
                .foregroundStyle(type.color)

            Text(message)
                .font(.system(size: 13, weight: .medium))
                .foregroundStyle(themeManager.colors.text)
                .lineLimit(2)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
        .background(
            Capsule()
                .fill(.ultraThinMaterial)
                .overlay(
                    Capsule()
                        .stroke(type.subtleColor, lineWidth: 1)
                )
                .shadow(color: themeManager.colors.shadowGlassColor, radius: 24, y: 8)
        )
    }
}

// ───────────────────────────────────────────────────
// ToastManager — ObservableObject to show/hide toasts
// ───────────────────────────────────────────────────

class ToastManager: ObservableObject {
    @Published var currentToast: ToastItem?
    @Published var isVisible: Bool = false

    private var dismissTask: Task<Void, Never>?

    struct ToastItem: Identifiable {
        let id = UUID()
        let message: String
        let type: Toast.ToastType
    }

    func show(_ message: String, type: Toast.ToastType = .info) {
        dismissTask?.cancel()

        withAnimation(ThemeColors.easeHarmony) {
            currentToast = ToastItem(message: message, type: type)
            isVisible = true
        }

        dismissTask = Task { @MainActor in
            try? await Task.sleep(for: .seconds(3))
            guard !Task.isCancelled else { return }
            withAnimation(ThemeColors.easeHarmony) {
                isVisible = false
            }
            // Allow slide-out animation to finish before removing
            try? await Task.sleep(for: .milliseconds(400))
            guard !Task.isCancelled else { return }
            currentToast = nil
        }
    }

    func dismiss() {
        dismissTask?.cancel()
        withAnimation(ThemeColors.easeHarmony) {
            isVisible = false
        }
        currentToast = nil
    }
}

// ───────────────────────────────────────────────────
// ToastModifier — View modifier for overlaying toasts
// ───────────────────────────────────────────────────

struct ToastModifier: ViewModifier {
    @ObservedObject var toastManager: ToastManager

    func body(content: Content) -> some View {
        content.overlay(alignment: .top) {
            if let toast = toastManager.currentToast {
                Toast(message: toast.message, type: toast.type)
                    .padding(.top, 16)
                    .transition(.move(edge: .top).combined(with: .opacity))
                    .opacity(toastManager.isVisible ? 1 : 0)
                    .offset(y: toastManager.isVisible ? 0 : -20)
                    .animation(ThemeColors.easeHarmony, value: toastManager.isVisible)
            }
        }
    }
}

extension View {
    /// Overlay a toast notification on this view using the given ToastManager.
    func toastOverlay(_ manager: ToastManager) -> some View {
        self.modifier(ToastModifier(toastManager: manager))
    }
}

#if DEBUG
struct Toast_Previews: PreviewProvider {
    static var previews: some View {
        VStack(spacing: 20) {
            Toast(message: "Changes saved successfully", type: .success)
            Toast(message: "Connection failed", type: .error)
            Toast(message: "Token expiring soon", type: .warning)
            Toast(message: "New version available", type: .info)
        }
        .padding()
        .environmentObject(ThemeManager())
    }
}
#endif
