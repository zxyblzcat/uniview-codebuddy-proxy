import SwiftUI

/// Simple animated donut chart.
/// Displays segments with labels, colors, and animated drawing.
/// Shows total or percentage in the center with a legend below.
struct DonutChart: View {
    @EnvironmentObject var themeManager: ThemeManager

    let segments: [Segment]
    let centerText: String?
    let centerSubtitle: String?

    struct Segment: Identifiable, Equatable {
        let id = UUID()
        let label: String
        let value: Double
        let color: Color

        static func == (lhs: Segment, rhs: Segment) -> Bool {
            lhs.id == rhs.id && lhs.label == rhs.label && lhs.value == rhs.value
        }
    }

    init(segments: [Segment], centerText: String? = nil, centerSubtitle: String? = nil) {
        self.segments = segments
        self.centerText = centerText
        self.centerSubtitle = centerSubtitle
    }

    @State private var animatedFraction: Double = 0

    private let chartSize: CGFloat = 160
    private let lineWidth: CGFloat = 28

    var body: some View {
        VStack(spacing: 16) {
            chartView
            legendView
        }
        .onAppear {
            withAnimation(ThemeColors.easeHarmony) {
                animatedFraction = 1
            }
        }
        .onChange(of: segments) { _ in
            animatedFraction = 0
            withAnimation(ThemeColors.easeHarmony) {
                animatedFraction = 1
            }
        }
    }

    // MARK: - Chart

    @ViewBuilder
    private var chartView: some View {
        ZStack {
            // Background ring
            Circle()
                .stroke(themeManager.colors.glassBgHeavy, lineWidth: lineWidth)

            // Segments
            ForEach(segmentAngles.indices, id: \.self) { index in
                let item = segmentAngles[index]
                if item.endAngle - item.startAngle > 0.01 {
                    Circle()
                        .trim(from: item.startAngle, to: min(item.startAngle + (item.endAngle - item.startAngle) * animatedFraction, item.endAngle))
                        .stroke(
                            item.color,
                            style: StrokeStyle(lineWidth: lineWidth, lineCap: .round)
                        )
                        .rotationEffect(.degrees(-90))
                }
            }

            // Center text
            VStack(spacing: 2) {
                if let centerText = centerText {
                    Text(centerText)
                        .font(.system(size: 22, weight: .bold, design: .rounded))
                        .foregroundStyle(themeManager.colors.text)
                } else {
                    Text(formatTotal(total))
                        .font(.system(size: 22, weight: .bold, design: .rounded))
                        .foregroundStyle(themeManager.colors.text)
                }
                if let subtitle = centerSubtitle {
                    Text(subtitle)
                        .font(.system(size: 11, weight: .medium))
                        .foregroundStyle(themeManager.colors.textSecondary)
                }
            }
        }
        .frame(width: chartSize, height: chartSize)
    }

    // MARK: - Legend

    @ViewBuilder
    private var legendView: some View {
        VStack(alignment: .leading, spacing: 6) {
            ForEach(segments) { segment in
                HStack(spacing: 8) {
                    RoundedRectangle(cornerRadius: 3)
                        .fill(segment.color)
                        .frame(width: 12, height: 12)

                    Text(segment.label)
                        .font(.system(size: 12, weight: .medium))
                        .foregroundStyle(themeManager.colors.textSecondary)

                    Spacer()

                    let pct = total > 0 ? (segment.value / total * 100) : 0
                    Text(String(format: "%.1f%%", pct))
                        .font(.system(size: 12, weight: .semibold, design: .monospaced))
                        .foregroundStyle(themeManager.colors.text)
                }
            }
        }
    }

    // MARK: - Computed

    private var total: Double {
        segments.map(\.value).reduce(0, +)
    }

    private struct SegmentAngle {
        let startAngle: CGFloat
        let endAngle: CGFloat
        let color: Color
    }

    private var segmentAngles: [SegmentAngle] {
        guard total > 0 else { return [] }
        var angles: [SegmentAngle] = []
        var currentAngle: CGFloat = 0
        for segment in segments {
            let fraction = segment.value / total
            let endAngle = currentAngle + fraction
            angles.append(SegmentAngle(
                startAngle: currentAngle,
                endAngle: endAngle,
                color: segment.color
            ))
            currentAngle = endAngle
        }
        return angles
    }

    private func formatTotal(_ value: Double) -> String {
        if value >= 1_000_000 {
            return String(format: "%.1fM", value / 1_000_000)
        } else if value >= 1_000 {
            return String(format: "%.1fK", value / 1_000)
        } else {
            return String(format: "%.0f", value)
        }
    }
}

#if DEBUG
struct DonutChart_Previews: PreviewProvider {
    static var previews: some View {        return
    DonutChart(
        segments: [
            .init(label: "GPT-4", value: 450, color: ThemeColors.success),
            .init(label: "Claude", value: 320, color: ThemeColors.info),
            .init(label: "DeepSeek", value: 180, color: ThemeColors.purple),
            .init(label: "Other", value: 50, color: ThemeColors.warning),
        ],
        centerSubtitle: "Total Requests"
    )
    .padding()
    .environmentObject(ThemeManager())
    }
}
#endif
