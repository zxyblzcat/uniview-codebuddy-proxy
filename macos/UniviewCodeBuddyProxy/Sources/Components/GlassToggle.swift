import SwiftUI

/// Custom capsule-shaped toggle switch with animated thumb.
/// Colors follow the theme: accent when on, muted when off.
struct GlassToggle: View {
    @EnvironmentObject var themeManager: ThemeManager

    let label: String
    @Binding var isOn: Bool

    private let trackWidth: CGFloat = 44
    private let trackHeight: CGFloat = 26
    private let thumbSize: CGFloat = 20

    var body: some View {
        HStack(spacing: 12) {
            Text(label)
                .font(.system(size: 14, weight: .medium))
                .foregroundStyle(themeManager.colors.text)

            Spacer()

            toggleTrack
        }
    }

    private var toggleTrack: some View {
        ZStack(alignment: isOn ? .trailing : .leading) {
            // Track
            Capsule()
                .fill(isOn ? themeManager.colors.primary : themeManager.colors.glassBgHeavy)
                .overlay(
                    Capsule()
                        .stroke(isOn ? themeManager.colors.primary.opacity(0.5) : themeManager.colors.glassBorder, lineWidth: 1)
                )
                .frame(width: trackWidth, height: trackHeight)
                .animation(ThemeColors.easeHarmony, value: isOn)

            // Thumb
            Circle()
                .fill(.white)
                .frame(width: thumbSize, height: thumbSize)
                .shadow(color: themeManager.colors.shadowGlassSMColor, radius: themeManager.colors.shadowGlassSMRadius, y: themeManager.colors.shadowGlassSMY)
                .padding(3)
                .animation(ThemeColors.easeHarmony, value: isOn)
        }
        .onTapGesture {
            withAnimation(ThemeColors.easeHarmony) {
                isOn.toggle()
            }
        }
    }
}

#if DEBUG
struct GlassToggle_Previews: PreviewProvider {
    static var previews: some View {        return
    VStack(spacing: 20) {
        GlassToggle(label: "Auto Image Parse", isOn: .constant(true))
        GlassToggle(label: "Usage Reporting", isOn: .constant(false))
    }
    .padding()
    .frame(width: 300)
    .environmentObject(ThemeManager())
    }
}
#endif
