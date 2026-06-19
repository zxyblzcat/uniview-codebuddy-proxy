import SwiftUI

// ═══════════════════════════════════════════════
// ContentView — 主内容视图：浮动标签栏 + 视图切换
// ═══════════════════════════════════════════════

struct ContentView: View {
    @EnvironmentObject var themeManager: ThemeManager
    @EnvironmentObject var configManager: ConfigManager
    @EnvironmentObject var tokenManager: TokenManager
    @EnvironmentObject var authService: AuthService
    @EnvironmentObject var logBuffer: LogBuffer
    @EnvironmentObject var telemetryReporter: TelemetryReporter
    @Environment(\.colorScheme) var colorScheme

    @State private var selectedTab: Int = 0
    @State private var toastManager = ToastManager()

    var body: some View {
        let colors = themeManager.colors

        ZStack {
            // ── 1. 四层径向渐变背景 ──
            backgroundGradient(colors: colors)

            // ── 2. 主内容区域 ──
            VStack(spacing: 0) {
                // 顶部状态栏
                topStatusBar(colors: colors)

                // 视图切换区
                Group {
                    switch selectedTab {
                    case 0:
                        DashboardView()
                    case 1:
                        ModelsView()
                    case 2:
                        TokensView()
                    case 3:
                        LogsView()
                    case 4:
                        SettingsView()
                    default:
                        DashboardView()
                    }
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)

            }

            // ── 3. 浮动标签栏（覆盖在底部） ──
            VStack {
                Spacer()
                FloatingTabBar(selectedIndex: $selectedTab)
                    .padding(.bottom, 16)
            }
        }
        .environmentObject(toastManager)
        .frame(minWidth: 1000, minHeight: 650)
        .onChange(of: colorScheme) { _, newScheme in
            themeManager.updateSystemColorScheme(newScheme)
        }
    }

    // MARK: - 背景

    @ViewBuilder
    private func backgroundGradient(colors: ThemeColors) -> some View {
        ZStack {
            // 第一层：基底填充
            colors.bg
                .ignoresSafeArea()

            // 第二层：中心主光晕
            RadialGradient(
                colors: [
                    colors.primary.opacity(0.08),
                    Color.clear,
                ],
                center: .center,
                startRadius: 0,
                endRadius: 500
            )
            .ignoresSafeArea()

            // 第三层：右上方强调光晕
            RadialGradient(
                colors: [
                    colors.accent.opacity(0.05),
                    Color.clear,
                ],
                center: UnitPoint(x: 0.85, y: 0.15),
                startRadius: 0,
                endRadius: 400
            )
            .ignoresSafeArea()

            // 第四层：左下方深度光晕
            RadialGradient(
                colors: [
                    colors.primary.opacity(0.04),
                    Color.clear,
                ],
                center: UnitPoint(x: 0.1, y: 0.9),
                startRadius: 0,
                endRadius: 350
            )
            .ignoresSafeArea()
        }
    }

    // MARK: - 顶部状态栏（从窗口顶部透明过渡到不透明）

    @ViewBuilder
    // MARK: - 顶部状态栏（从窗口顶部透明过渡到不透明）

    @ViewBuilder
    private func topStatusBar(colors: ThemeColors) -> some View {
        VStack(spacing: 0) {
            // 状态栏内容区
            HStack(spacing: 12) {
                // 连接指示器：绿色圆点
                Circle()
                    .fill(ThemeColors.success)
                    .frame(width: 8, height: 8)
                    .overlay(
                        Circle()
                            .stroke(ThemeColors.success.opacity(0.3), lineWidth: 3)
                    )

                // 代理状态文本
                Text("代理运行中")
                    .font(.system(size: 12, weight: .medium, design: .rounded))
                    .foregroundStyle(colors.textSecondary)

                // 分隔符
                Text("·")
                    .foregroundStyle(colors.textMuted)

                // 端口信息
                Text("端口 \(configManager.port)")
                    .font(.system(size: 12, weight: .medium, design: .rounded))
                    .foregroundStyle(colors.textSecondary)

                // 分隔符
                Text("·")
                    .foregroundStyle(colors.textMuted)

                // 当前模型数量
                Text("\(tokenManager.activeTokenCount) 个活跃令牌")
                    .font(.system(size: 12, weight: .medium, design: .rounded))
                    .foregroundStyle(colors.textSecondary)

                Spacer()
            }
            .padding(.horizontal, 24)
            .padding(.vertical, 10)
            .background(
                // 状态栏区域：从半透明到不透明
                LinearGradient(
                    colors: [
                        colors.bg.opacity(0.4),
                        colors.bg.opacity(0.7),
                    ],
                    startPoint: .top,
                    endPoint: .bottom
                )
            )
        }
    }
    }
}
