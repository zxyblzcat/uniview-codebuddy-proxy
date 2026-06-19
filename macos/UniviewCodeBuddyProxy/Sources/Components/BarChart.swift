import SwiftUI

/// Simple animated bar chart.
/// Displays an array of (label, value) data points
/// with theme accent gradient bars and horizontal grid lines.
struct BarChart: View {
    @EnvironmentObject var themeManager: ThemeManager

    let data: [DataPoint]
    let showGrid: Bool

    struct DataPoint: Identifiable, Equatable {
        let id = UUID()
        let label: String
        let value: Double
    }

    init(data: [DataPoint], showGrid: Bool = true) {
        self.data = data
        self.showGrid = showGrid
    }

    @State private var animatedValues: [Double] = []

    private let barSpacing: CGFloat = 12
    private let gridLineCount = 4
    private let cornerRadius: CGFloat = 6

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            chartArea
            labelsRow
        }
        .onAppear {
            animateBars()
        }
        .onChange(of: data) { _ in
            animateBars()
        }
    }

    // MARK: - Chart Area

    @ViewBuilder
    private var chartArea: some View {
        GeometryReader { geo in
            let maxValue = max(data.map(\.value).max() ?? 1, 1)

            ZStack(alignment: .bottom) {
                // Grid lines
                if showGrid {
                    gridLines(maxValue: maxValue, height: geo.size.height)
                }

                // Bars
                HStack(alignment: .bottom, spacing: barSpacing) {
                    ForEach(data.indices, id: \.self) { index in
                        let normalizedValue = animatedValues.indices.contains(index)
                            ? animatedValues[index] / maxValue
                            : 0

                        barView(
                            normalizedHeight: normalizedValue,
                            height: geo.size.height
                        )
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func barView(normalizedHeight: Double, height: CGFloat) -> some View {
        VStack {
            Spacer()
            RoundedRectangle(cornerRadius: cornerRadius)
                .fill(
                    LinearGradient(
                        colors: [
                            themeManager.colors.primary,
                            themeManager.colors.accent
                        ],
                        startPoint: .bottom,
                        endPoint: .top
                    )
                )
                .frame(height: max(CGFloat(normalizedHeight) * height, 0))
                .animation(ThemeColors.easeHarmony, value: normalizedHeight)
        }
    }

    @ViewBuilder
    private func gridLines(maxValue: Double, height: CGFloat) -> some View {
        VStack {
            ForEach(0..<gridLineCount, id: \.self) { i in
                let fraction = Double(gridLineCount - i) / Double(gridLineCount)
                HStack {
                    Text(formatValue(maxValue * fraction))
                        .font(.system(size: 10, design: .monospaced))
                        .foregroundStyle(themeManager.colors.textMuted)
                        .frame(width: 40, alignment: .trailing)
                    Rectangle()
                        .fill(themeManager.colors.glassBorder)
                        .frame(height: 0.5)
                    Spacer()
                }
                if i < gridLineCount - 1 {
                    Spacer()
                }
            }
        }
    }

    // MARK: - Labels

    @ViewBuilder
    private var labelsRow: some View {
        HStack(spacing: barSpacing) {
            ForEach(data) { point in
                Text(point.label)
                    .font(.system(size: 10, weight: .medium))
                    .foregroundStyle(themeManager.colors.textSecondary)
                    .lineLimit(1)
                    .frame(maxWidth: .infinity)
            }
        }
        .padding(.leading, 44)
        .padding(.top, 6)
    }

    // MARK: - Helpers

    private func animateBars() {
        animatedValues = Array(repeating: 0, count: data.count)
        withAnimation(ThemeColors.easeHarmony) {
            animatedValues = data.map(\.value)
        }
    }

    private func formatValue(_ value: Double) -> String {
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
struct BarChart_Previews: PreviewProvider {
    static var previews: some View {        return
    BarChart(data: [
        .init(label: "周一", value: 120),
        .init(label: "周二", value: 340),
        .init(label: "周三", value: 220),
        .init(label: "周四", value: 480),
        .init(label: "周五", value: 310),
        .init(label: "周六", value: 90),
        .init(label: "周日", value: 180),
    ])
    .frame(height: 200)
    .padding()
    .environmentObject(ThemeManager())
    }
}
#endif
