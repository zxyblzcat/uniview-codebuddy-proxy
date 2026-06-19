import SwiftUI

// ═══════════════════════════════════════════════
// LogsView — 日志查看器（终端风格，匹配设计稿）
// ═══════════════════════════════════════════════

struct LogsView: View {
    @EnvironmentObject var themeManager: ThemeManager
    @EnvironmentObject var logBuffer: LogBuffer

    // MARK: - State

    @State private var searchText = ""
    @State private var selectedLevel: LogLevel?
    @State private var autoScroll = true
    @State private var showClearConfirm = false
    @State private var isConnected = true
    @State private var pulseVisible = true

    private var c: ThemeColors { themeManager.colors }

    private let levelFilters: [(label: String, level: LogLevel?)] = [
        ("全部", nil),
        ("信息", .info),
        ("警告", .warn),
        ("错误", .error),
    ]

    // MARK: - Filtered Entries

    private var filteredEntries: [LogEntry] {
        var entries = logBuffer.recentEntries

        if let level = selectedLevel {
            entries = entries.filter { $0.level == level }
        }

        if !searchText.isEmpty {
            entries = entries.filter {
                $0.message.localizedCaseInsensitiveContains(searchText)
            }
        }

        return entries
    }

    // MARK: - Body

    var body: some View {
        VStack(spacing: 0) {
            // 页面标题
            pageHead
                .padding(.horizontal, 24)
                .padding(.top, 16)

            // 玻璃卡片包裹工具栏 + 日志体
            VStack(spacing: 0) {
                // 工具栏
                toolbar

                // 日志体
                logBody
            }
            .glassCardBackground(colors: c)
            .clipShape(RoundedRectangle(cornerRadius: c.radiusMD))
            .padding(.horizontal, 24)
            .padding(.top, 12)
        }
        .frame(maxWidth: 1200)
        .background(c.bg)
        .alert("清除日志", isPresented: $showClearConfirm) {
            Button("取消", role: .cancel) {}
            Button("清除", role: .destructive) {
                Task {
                    await logBuffer.clear()
                }
            }
        } message: {
            Text("确定要清除所有日志条目吗？此操作不可撤销。")
        }
        .onAppear {
            // 脉冲动画
            withAnimation(.easeInOut(duration: 1.5).repeatForever(autoreverses: true)) {
                pulseVisible = false
            }
        }
    }

    // MARK: - 页面标题

    private var pageHead: some View {
        HStack {
            VStack(alignment: .leading, spacing: 4) {
                Text("实时日志")
                    .font(.system(size: 26, weight: .bold))
                    .foregroundColor(c.text)
                Text("通过 SSE 实时查看服务器日志流")
                    .font(.system(size: 13))
                    .foregroundColor(c.textMuted)
            }
            Spacer()
        }
    }

    // MARK: - 工具栏

    private var toolbar: some View {
        HStack(spacing: 10) {
            // 搜索框
            searchField

            // 筛选按钮
            filterPills

            // 自动滚动
            autoScrollCheckbox

            // 清除按钮
            clearButton

            Spacer()

            // 连接指示器
            connectionIndicator
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
        .background(c.glassBgHeavy.opacity(0.3))
        .overlay(alignment: .bottom) {
            Rectangle()
                .fill(c.glassBorder.opacity(0.5))
                .frame(height: 0.5)
        }
    }

    // MARK: - 搜索框

    private var searchField: some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass")
                .font(.system(size: 13))
                .foregroundColor(c.textMuted)

            TextField("搜索日志...", text: $searchText)
                .textFieldStyle(.plain)
                .font(.system(size: 12))
                .foregroundColor(c.text)

            if !searchText.isEmpty {
                Button {
                    searchText = ""
                } label: {
                    Image(systemName: "xmark.circle.fill")
                        .font(.system(size: 11))
                        .foregroundColor(c.textMuted)
                }
                .buttonStyle(.plain)
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 7)
        .frame(maxWidth: 260)
        .background(c.glassBg)
        .clipShape(RoundedRectangle(cornerRadius: c.radiusSM))
        .overlay(
            RoundedRectangle(cornerRadius: c.radiusSM)
                .stroke(c.glassBorder, lineWidth: 1)
        )
    }

    // MARK: - 筛选按钮

    private var filterPills: some View {
        HStack(spacing: 3) {
            ForEach(levelFilters, id: \.label) { filter in
                let isActive = selectedLevel == filter.level

                Button {
                    withAnimation(ThemeColors.easeHarmony) {
                        selectedLevel = filter.level
                    }
                } label: {
                    Text(filter.label)
                        .font(.system(size: 11, weight: isActive ? .semibold : .medium))
                        .foregroundStyle(isActive ? c.primary : c.textMuted)
                        .padding(.horizontal, 12)
                        .padding(.vertical, 5)
                        .background(
                            RoundedRectangle(cornerRadius: ThemeColors.radiusPill)
                                .fill(isActive ? c.primarySubtle : Color.clear)
                        )
                        .overlay(
                            RoundedRectangle(cornerRadius: ThemeColors.radiusPill)
                                .stroke(
                                    isActive ? c.primary.opacity(0.12) : Color.clear,
                                    lineWidth: 1
                                )
                        )
                }
                .buttonStyle(.plain)
            }
        }
    }

    // MARK: - 自动滚动

    private var autoScrollCheckbox: some View {
        Toggle(isOn: $autoScroll) {
            Text("自动滚动")
                .font(.system(size: 12))
                .foregroundColor(c.textSecondary)
        }
        .toggleStyle(.checkbox)
        .fixedSize()
    }

    // MARK: - 清除按钮

    private var clearButton: some View {
        Button {
            showClearConfirm = true
        } label: {
            Text("清除")
                .font(.system(size: 12, weight: .medium))
                .foregroundColor(c.textSecondary)
                .padding(.horizontal, 14)
                .padding(.vertical, 5)
        }
        .buttonStyle(.plain)
        .help("清除所有日志")
    }

    // MARK: - 连接指示器

    private var connectionIndicator: some View {
        HStack(spacing: 6) {
            // 脉冲圆点
            Circle()
                .fill(ThemeColors.success)
                .frame(width: 7, height: 7)
                .shadow(color: ThemeColors.success.opacity(pulseVisible ? 0.6 : 0.2), radius: pulseVisible ? 6 : 2)
                .opacity(pulseVisible ? 1.0 : 0.5)

            Text(isConnected ? "已连接" : "已断开")
                .font(.system(size: 11, weight: .medium))
                .foregroundColor(isConnected ? ThemeColors.success : ThemeColors.danger)
        }
    }

    // MARK: - 日志体

    private var logBody: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 0) {
                    ForEach(filteredEntries) { entry in
                        logLine(entry)
                            .id(entry.id)
                    }
                }
                .padding(16)
            }
            .onChange(of: filteredEntries.count) { _, _ in
                if autoScroll, let lastEntry = filteredEntries.last {
                    proxy.scrollTo(lastEntry.id, anchor: .bottom)
                }
            }
        }
    }

    // MARK: - 单行日志

    private func logLine(_ entry: LogEntry) -> some View {
        HStack(spacing: 0) {
            // 时间戳
            Text(entry.shortTime)
                .font(.system(size: 12, design: .monospaced))
                .foregroundColor(c.textMuted)

            Text(" ")

            // 级别标签 [LEVEL]
            Text(levelTag(entry.level))
                .font(.system(size: 12, design: .monospaced))
                .foregroundColor(levelColor(entry.level))

            Text(" ")

            // 消息
            Text(entry.message)
                .font(.system(size: 12, design: .monospaced))
                .foregroundColor(c.textSecondary)
                .textSelection(.enabled)
        }
        .padding(.vertical, 1)
        .padding(.horizontal, 8)
        .clipShape(RoundedRectangle(cornerRadius: 4))
        .onHover { isHovered in
            // SwiftUI 不支持条件性 modifier，用 background 替代
        }
        .background(
            RoundedRectangle(cornerRadius: 4)
                .fill(themeManager.colors.hoverBg)
                .opacity(0) // 默认不可见，通过 overlay 方式在 hover 时显示
        )
    }

    // MARK: - Level 格式化

    private func levelTag(_ level: LogLevel) -> String {
        let raw = level.rawValue.uppercased()
        // padEnd(5): INFO→"INFO ", WARN→"WARN ", ERROR→"ERROR", DEBUG→"DEBUG"
        return raw.padding(toLength: 5, withPad: " ", startingAt: 0)
    }

    private func levelColor(_ level: LogLevel) -> Color {
        switch level {
        case .debug: return c.textMuted
        case .info:  return ThemeColors.info
        case .warn:  return ThemeColors.warning
        case .error: return ThemeColors.danger
        }
    }
}
