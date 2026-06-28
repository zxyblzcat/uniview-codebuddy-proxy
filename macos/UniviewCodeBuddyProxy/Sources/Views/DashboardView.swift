import SwiftUI

// ═══════════════════════════════════════════════
// DashboardView — 主仪表盘（匹配设计稿）
// ═══════════════════════════════════════════════

struct DashboardView: View {
    @EnvironmentObject var themeManager: ThemeManager
    @EnvironmentObject var configManager: ConfigManager
    @EnvironmentObject var tokenManager: TokenManager
    @EnvironmentObject var logBuffer: LogBuffer
    @EnvironmentObject var usageStats: UsageStats

    // MARK: - State

    @State private var hoveredKPI: Int?
    @State private var hoveredBarIndex: Int?

    private var c: ThemeColors { themeManager.colors }

    // MARK: - Computed Data

    /// 小时请求数（从 UsageStats 实时数据）
    private var hourlyRequests: [Int] {
        usageStats.hourlyBuckets.map { $0.requests }
    }

    /// 模型使用分布（从 UsageStats 实时数据）
    private var modelUsage: [(name: String, ratio: Double, color: Color)] {
        let dist = usageStats.modelDistribution
        let totalReqs = dist.values.reduce(0) { $0 + $1.requests }
        guard totalReqs > 0 else {
            return [("暂无数据", 1.0, Color(hex: "5B9CF6"))]
        }
        let colors: [Color] = [
            Color(hex: "5B9CF6"), Color(hex: "34D4AA"),
            Color(hex: "4ADE80"), Color(hex: "FBBF24"),
            Color(hex: "F87171"), Color(hex: "A78BFA"),
        ]
        let sorted = dist.sorted { $0.value.requests > $1.value.requests }
        var result: [(name: String, ratio: Double, color: Color)] = []
        for (i, entry) in sorted.enumerated() {
            if i >= 5 {
                // 合并剩余为"其他"
                let remaining = sorted.dropFirst(5).reduce(0) { $0 + $1.value.requests }
                result.append(("其他", Double(remaining) / Double(totalReqs), colors[5]))
                break
            }
            result.append((entry.key, Double(entry.value.requests) / Double(totalReqs), colors[i % colors.count]))
        }
        return result
    }

    // MARK: - Body

    var body: some View {
        ScrollView {
            VStack(spacing: 20) {
                // KPI Cards
                kpiSection

                // Charts Row
                HStack(spacing: 16) {
                    barChartCard
                    donutChartCard
                }

                // Recent Activity
                recentActivityCard
            }
            .padding(24)
        }
        .background(c.bg)
    }

    // MARK: - KPI Section

    private var kpiSection: some View {
        HStack(spacing: 14) {
            kpiCard(
                index: 0,
                title: "总请求数",
                value: usageStats.totalRequests.formatted(.number),
                trend: usageStats.totalRequests > 0 ? "本次运行" : "暂无数据",
                trendUp: usageStats.totalRequests > 0,
                icon: "waveform.path.ecg",
                color: ThemeColors.info,
                accentColor: ThemeColors.info
            )
            kpiCard(
                index: 1,
                title: "成功率",
                value: String(format: "%.1f%%", usageStats.successRate * 100),
                trend: usageStats.totalRequests > 0 ? "\(usageStats.successCount)/\(usageStats.totalRequests)" : "暂无数据",
                trendUp: usageStats.successRate > 0.95,
                icon: "checkmark.circle.fill",
                color: ThemeColors.success,
                accentColor: ThemeColors.success
            )
            kpiCard(
                index: 2,
                title: "平均延迟",
                value: usageStats.totalRequests > 0 ? String(format: "%.0fms", usageStats.avgLatency) : "--",
                trend: usageStats.totalRequests > 0 ? "均值" : "暂无数据",
                trendUp: false,
                icon: "clock",
                color: ThemeColors.purple,
                accentColor: ThemeColors.purple
            )
            kpiCard(
                index: 3,
                title: "活跃令牌",
                value: "\(tokenManager.activeTokenCount) / \(tokenManager.entries.count)",
                trend: (tokenManager.entries.count - tokenManager.activeTokenCount) > 0 ? "\(tokenManager.entries.count - tokenManager.activeTokenCount) 个冷却中" : "全部可用",
                trendUp: tokenManager.activeTokenCount == tokenManager.entries.count && tokenManager.entries.count > 0,
                icon: "key.fill",
                color: ThemeColors.warning,
                accentColor: ThemeColors.warning
            )
            kpiCard(
                index: 4,
                title: "累计费用",
                value: usageStats.totalCredit > 0 ? String(format: "¥%.2f", usageStats.totalCredit) : "¥0",
                trend: usageStats.totalCredit > 0 ? "本次运行" : "暂无数据",
                trendUp: usageStats.totalCredit > 0,
                icon: "yensign.circle.fill",
                color: Color(hex: "FBBF24"),
                accentColor: Color(hex: "FBBF24")
            )
            kpiCard(
                index: 5,
                title: "缓存命中",
                value: (usageStats.cacheHitTokens + usageStats.cacheMissTokens) > 0 ? String(format: "%.1f%%", usageStats.cacheHitRate * 100) : "--",
                trend: (usageStats.cacheHitTokens + usageStats.cacheMissTokens) > 0 ? "\(usageStats.cacheHitTokens) hits" : "暂无数据",
                trendUp: usageStats.cacheHitRate > 0.5,
                icon: "arrow.triangle.2.circlepath.circle.fill",
                color: Color(hex: "34D4AA"),
                accentColor: Color(hex: "34D4AA")
            )
            kpiCard(
                index: 6,
                title: "Token 用量",
                value: usageStats.totalTokens > 0 ? formatTokenCount(usageStats.totalTokens) : "0",
                trend: usageStats.totalTokens > 0 ? "in:\(formatTokenCount(usageStats.totalPromptTokens)) out:\(formatTokenCount(usageStats.totalCompletionTokens))" : "暂无数据",
                trendUp: usageStats.totalTokens > 0,
                icon: "number.circle.fill",
                color: Color(hex: "A78BFA"),
                accentColor: Color(hex: "A78BFA")
            )
        }
    }

    /// 格式化 Token 数量（如 1.2k, 24.8k, 1.5M）
    private func formatTokenCount(_ count: Int) -> String {
        if count >= 1_000_000 {
            return String(format: "%.1fM", Double(count) / 1_000_000.0)
        } else if count >= 1_000 {
            return String(format: "%.1fk", Double(count) / 1_000.0)
        } else {
            return "\(count)"
        }
    }

    private func kpiCard(
        index: Int,
        title: String,
        value: String,
        trend: String,
        trendUp: Bool,
        icon: String,
        color: Color,
        accentColor: Color
    ) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            // 顶部：图标 + 标签
            HStack(spacing: 6) {
                Image(systemName: icon)
                    .font(.system(size: 14))
                    .foregroundColor(color.opacity(0.5))
                Text(title)
                    .font(.system(size: 11, weight: .medium))
                    .foregroundColor(c.textMuted)
            }
            .padding(.bottom, 10)

            // 主数值
            Text(value)
                .font(.system(size: 32, weight: .bold, design: .rounded))
                .foregroundColor(c.text)
                .lineLimit(1)

            // 趋势
            HStack(spacing: 4) {
                if trendUp {
                    Image(systemName: "chart.line.uptrend.xyaxis")
                        .font(.system(size: 11))
                } else {
                    Image(systemName: "chart.line.downtrend.xyaxis")
                        .font(.system(size: 11))
                }
                Text(trend)
                    .font(.system(size: 11, weight: .medium))
            }
            .foregroundColor(trendUp ? ThemeColors.success : c.textMuted)
            .padding(.top, 10)
        }
        .padding(20)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(
            RoundedRectangle(cornerRadius: c.radiusMD)
                .fill(c.glassBg)
                .overlay(
                    RoundedRectangle(cornerRadius: c.radiusMD)
                        .stroke(c.glassBorder, lineWidth: 1)
                )
                .overlay(alignment: .top) {
                    // 顶部彩色线条
                    RoundedRectangle(cornerRadius: 3)
                        .fill(
                            LinearGradient(
                                colors: [accentColor, accentColor.opacity(0.2)],
                                startPoint: .leading,
                                endPoint: .trailing
                            )
                        )
                        .frame(height: 3)
                        .opacity(0.8)
                }
                .shadow(color: c.shadowGlassSMColor, radius: c.shadowGlassSMRadius, y: c.shadowGlassSMY)
        )
        .hoverLift(isHovered: hoveredKPI == index)
        .onHover { hovering in
            hoveredKPI = hovering ? index : nil
        }
    }

    // MARK: - Bar Chart (请求量趋势 24h)

    private var barChartCard: some View {
        VStack(alignment: .leading, spacing: 0) {
            // 卡片标题
            HStack {
                Text("请求量趋势（24h）")
                    .font(.system(size: 14, weight: .semibold))
                    .foregroundColor(c.text)

                Spacer()

                Text("实时")
                    .font(.system(size: 11, weight: .semibold))
                    .foregroundColor(c.primary)
                    .padding(.horizontal, 8)
                    .padding(.vertical, 3)
                    .background(c.primarySubtle)
                    .clipShape(Capsule())
            }
            .padding(.horizontal, 20)
            .padding(.vertical, 16)
            .overlay(alignment: .bottom) {
                Rectangle()
                    .fill(c.glassBorder.opacity(0.5))
                    .frame(height: 0.5)
                    .padding(.horizontal, 20)
            }

            // 图表区域
            VStack(spacing: 0) {
                let maxVal = max(hourlyRequests.max() ?? 1, 1)

                HStack(alignment: .bottom, spacing: 2) {
                    ForEach(Array(hourlyRequests.enumerated()), id: \.offset) { index, value in
                        let isRecent = index >= hourlyRequests.count - 3

                        RoundedRectangle(cornerRadius: 3)
                            .fill(
                                isRecent
                                    ? LinearGradient(
                                        colors: [c.accent, c.accent.opacity(0.3)],
                                        startPoint: .top,
                                        endPoint: .bottom
                                    )
                                    : LinearGradient(
                                        colors: [c.primary, c.primary.opacity(0.3)],
                                        startPoint: .top,
                                        endPoint: .bottom
                                    )
                            )
                            .frame(height: max(CGFloat(value) / CGFloat(maxVal) * 110, 2))
                            .clipShape(UnevenRoundedRectangle(topLeadingRadius: 3, topTrailingRadius: 3))
                            .overlay {
                                // Hover tooltip
                                if hoveredBarIndex == index {
                                    Text("\(value) 请求")
                                        .font(.system(size: 11))
                                        .foregroundColor(c.text)
                                        .padding(.horizontal, 10)
                                        .padding(.vertical, 5)
                                        .background(
                                            RoundedRectangle(cornerRadius: 8)
                                                .fill(c.surface.opacity(0.92))
                                                .overlay(
                                                    RoundedRectangle(cornerRadius: 8)
                                                        .stroke(c.glassBorder, lineWidth: 1)
                                                )
                                                .shadow(color: c.shadowTabbarColor, radius: 8, y: 4)
                                        )
                                        .offset(y: -20)
                                }
                            }
                            .onHover { hovering in
                                hoveredBarIndex = hovering ? index : nil
                            }
                    }
                }
                .frame(height: 110)
                .padding(.horizontal, 2)

                // 时间标签
                HStack {
                    Text("00:00")
                    Spacer()
                    Text("04:00")
                    Spacer()
                    Text("08:00")
                    Spacer()
                    Text("12:00")
                    Spacer()
                    Text("16:00")
                    Spacer()
                    Text("20:00")
                    Spacer()
                    Text("当前")
                }
                .font(.system(size: 10))
                .foregroundColor(c.textMuted)
                .padding(.horizontal, 2)
                .padding(.top, 8)
            }
            .padding(20)
        }
        .background(
            RoundedRectangle(cornerRadius: c.radiusMD)
                .fill(c.glassBg)
                .overlay(
                    RoundedRectangle(cornerRadius: c.radiusMD)
                        .stroke(c.glassBorder, lineWidth: 1)
                )
                .overlay(alignment: .top) {
                    LinearGradient(
                        colors: c.highlightGradient,
                        startPoint: .leading,
                        endPoint: .trailing
                    )
                    .frame(height: 1)
                    .padding(.horizontal, 20)
                }
                .shadow(color: c.shadowGlassColor, radius: c.shadowGlassRadius, y: c.shadowGlassY)
        )
    }

    // MARK: - Donut Chart (模型使用分布)

    private var donutChartCard: some View {
        VStack(alignment: .leading, spacing: 0) {
            // 卡片标题
            HStack {
                Text("模型使用分布")
                    .font(.system(size: 14, weight: .semibold))
                    .foregroundColor(c.text)

                Spacer()
            }
            .padding(.horizontal, 20)
            .padding(.vertical, 16)
            .overlay(alignment: .bottom) {
                Rectangle()
                    .fill(c.glassBorder.opacity(0.5))
                    .frame(height: 0.5)
                    .padding(.horizontal, 20)
            }

            // 图表 + 图例
            HStack(spacing: 24) {
                // 甜甜圈（130px，细线 3pt，圆角端点）
                ZStack {
                    // 背景环
                    Circle()
                        .stroke(c.glassBgHeavy, lineWidth: 3)

                    // 数据段
                    ForEach(Array(segmentAngles.enumerated()), id: \.offset) { index, item in
                        if item.endAngle - item.startAngle > 0.01 {
                            Circle()
                                .trim(from: item.startAngle, to: item.endAngle)
                                .stroke(
                                    item.color,
                                    style: StrokeStyle(lineWidth: 3, lineCap: .round)
                                )
                                .rotationEffect(.degrees(-90))
                        }
                    }

                    // 中心文字
                    VStack(spacing: 2) {
                        Text(formatTokenCount(usageStats.totalRequests))
                            .font(.system(size: 26, weight: .bold))
                            .foregroundColor(c.text)
                            .lineLimit(1)
                        Text("总计")
                            .font(.system(size: 10))
                            .foregroundColor(c.textMuted)
                    }
                }
                .frame(width: 130, height: 130)

                // 图例（右侧竖排）
                VStack(alignment: .leading, spacing: 10) {
                    ForEach(modelUsage, id: \.name) { slice in
                        HStack(spacing: 8) {
                            // 彩色方块
                            RoundedRectangle(cornerRadius: 3)
                                .fill(slice.color)
                                .frame(width: 10, height: 10)

                            Text(slice.name)
                                .font(.system(size: 13))
                                .foregroundColor(c.textSecondary)

                            Spacer()

                            Text("\(Int(slice.ratio * 100))%")
                                .font(.system(size: 12, weight: .semibold))
                                .foregroundColor(c.text)
                        }
                    }
                }
            }
            .padding(20)
        }
        .background(
            RoundedRectangle(cornerRadius: c.radiusMD)
                .fill(c.glassBg)
                .overlay(
                    RoundedRectangle(cornerRadius: c.radiusMD)
                        .stroke(c.glassBorder, lineWidth: 1)
                )
                .overlay(alignment: .top) {
                    LinearGradient(
                        colors: c.highlightGradient,
                        startPoint: .leading,
                        endPoint: .trailing
                    )
                    .frame(height: 1)
                    .padding(.horizontal, 20)
                }
                .shadow(color: c.shadowGlassColor, radius: c.shadowGlassRadius, y: c.shadowGlassY)
        )
    }

    // MARK: - Donut Segment Angles

    private struct SegmentAngle {
        let startAngle: CGFloat
        let endAngle: CGFloat
        let color: Color
    }

    private var segmentAngles: [SegmentAngle] {
        var angles: [SegmentAngle] = []
        var currentAngle: CGFloat = 0
        for slice in modelUsage {
            let endAngle = currentAngle + CGFloat(slice.ratio)
            angles.append(SegmentAngle(
                startAngle: currentAngle,
                endAngle: endAngle,
                color: slice.color
            ))
            currentAngle = endAngle
        }
        return angles
    }

    // MARK: - Recent Activity

    /// 活动事件类型，映射日志消息到可视化事件
    private enum ActivityKind {
        case tokenRefresh    // 令牌刷新
        case clientConnect   // 新客户端连接
        case rateLimit       // 限流警告
        case modelUpdate     // 模型列表更新
        case authLogin       // OAuth 登录
        case proxyRequest    // 代理请求
        case circuitBreaker  // 熔断器
        case healthCheck     // 健康探针
        case other           // 其他

        var color: Color {
            switch self {
            case .tokenRefresh:   return ThemeColors.success
            case .clientConnect:  return ThemeColors.info
            case .rateLimit:      return ThemeColors.warning
            case .modelUpdate:    return Color(hex: "34D4AA")
            case .authLogin:      return ThemeColors.success
            case .proxyRequest:   return ThemeColors.info
            case .circuitBreaker: return ThemeColors.warning
            case .healthCheck:    return ThemeColors.success
            case .other:          return ThemeColors.info
            }
        }

        /// 从日志消息推断活动类型
        static func from(message: String) -> ActivityKind {
            let lower = message.lowercased()
            if lower.contains("令牌") && (lower.contains("刷新") || lower.contains("续期") || lower.contains("refresh")) {
                return .tokenRefresh
            }
            if lower.contains("客户端") || lower.contains("client") || lower.contains("连接") {
                return .clientConnect
            }
            if lower.contains("429") || lower.contains("限流") || lower.contains("冷却") || lower.contains("rate limit") {
                return .rateLimit
            }
            if lower.contains("模型") && (lower.contains("更新") || lower.contains("刷新") || lower.contains("model")) {
                return .modelUpdate
            }
            if lower.contains("oauth") || lower.contains("登录") || lower.contains("login") || lower.contains("认证") {
                return .authLogin
            }
            if lower.contains("熔断") || lower.contains("circuit") {
                return .circuitBreaker
            }
            if lower.contains("健康") || lower.contains("探针") || lower.contains("health") {
                return .healthCheck
            }
            if lower.contains("post") || lower.contains("get") || lower.contains("代理") || lower.contains("proxy") {
                return .proxyRequest
            }
            return .other
        }
    }

    private var recentActivityCard: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text("最近活动")
                .font(.system(size: 14, weight: .semibold))
                .foregroundColor(c.text)
                .padding(.horizontal, 20)
                .padding(.vertical, 16)
                .overlay(alignment: .bottom) {
                    Rectangle()
                        .fill(c.glassBorder.opacity(0.5))
                        .frame(height: 0.5)
                        .padding(.horizontal, 20)
                }

            let entries = Array(logBuffer.recentEntries.suffix(5))
            if entries.isEmpty {
                Text("暂无活动")
                    .font(.system(size: 12))
                    .foregroundColor(c.textMuted)
                    .frame(maxWidth: .infinity, alignment: .center)
                    .padding(.vertical, 24)
            } else {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(Array(entries.enumerated()), id: \.element.id) { index, entry in
                        let kind = ActivityKind.from(message: entry.message)

                        HStack(alignment: .top, spacing: 10) {
                            // 彩色圆点
                            Circle()
                                .fill(kind.color)
                                .frame(width: 8, height: 8)
                                .padding(.top, 6)

                            // 事件内容
                            VStack(alignment: .leading, spacing: 2) {
                                Text(entry.message)
                                    .font(.system(size: 12))
                                    .foregroundColor(c.textSecondary)
                                    .lineLimit(2)

                                Text(entry.shortTime)
                                    .font(.system(size: 10, design: .monospaced))
                                    .foregroundColor(c.textMuted)
                            }
                        }
                        .padding(.vertical, 10)
                        .padding(.horizontal, 20)

                        if index < entries.count - 1 {
                            Rectangle()
                                .fill(c.glassBorder.opacity(0.3))
                                .frame(height: 0.5)
                                .padding(.horizontal, 20)
                        }
                    }
                }
            }
        }
        .background(
            RoundedRectangle(cornerRadius: c.radiusMD)
                .fill(c.glassBg)
                .overlay(
                    RoundedRectangle(cornerRadius: c.radiusMD)
                        .stroke(c.glassBorder, lineWidth: 1)
                )
                .overlay(alignment: .top) {
                    LinearGradient(
                        colors: c.highlightGradient,
                        startPoint: .leading,
                        endPoint: .trailing
                    )
                    .frame(height: 1)
                    .padding(.horizontal, 20)
                }
                .shadow(color: c.shadowGlassColor, radius: c.shadowGlassRadius, y: c.shadowGlassY)
        )
    }
}
