import SwiftUI
import ServiceManagement

// ═══════════════════════════════════════════════
// SettingsView — 设置面板（液态玻璃分组卡片布局）
// ═══════════════════════════════════════════════

struct SettingsView: View {
    @EnvironmentObject var themeManager: ThemeManager
    @EnvironmentObject var configManager: ConfigManager

    private var c: ThemeColors { themeManager.colors }

    var body: some View {
        ScrollView {
            VStack(spacing: 0) {
                // 服务器信息横幅
                infoBanner

                VStack(spacing: 12) {
                    // 应用偏好
                    groupLabel("应用偏好")
                    settingsGroup {
                        settingRow(
                            icon: "power",
                            iconColor: ThemeColors.info,
                            title: configManager.localizedString("开机自动启动"),
                            description: "系统启动后自动运行代理服务，保持后台常驻"
                        ) {
                            glassToggle(isOn: Binding(
                                get: { SMAppService.mainApp.status == .enabled },
                                set: { _ in toggleAutoLaunch() }
                            ))
                        }

                        settingRow(
                            icon: "globe",
                            iconColor: ThemeColors.success,
                            title: configManager.localizedString("界面语言"),
                            description: "切换管理面板的显示语言"
                        ) {
                            GlassSegmentedPicker(
                                options: [
                                    GlassSegmentedOption(label: "中文", value: "zh-CN"),
                                    GlassSegmentedOption(label: "English", value: "en")
                                ],
                                selection: $configManager.locale
                            )
                        }

                        settingRow(
                            icon: "eye",
                            iconColor: ThemeColors.purple,
                            title: configManager.localizedString("系统托盘图标"),
                            description: "在菜单栏显示代理服务状态图标"
                        ) {
                            glassToggle(isOn: .constant(true))
                        }
                    }

                    // 智能功能
                    groupLabel("智能功能")
                    settingsGroup {
                        settingRow(
                            icon: "arrow.left.arrow.right",
                            iconColor: ThemeColors.warning,
                            title: configManager.localizedString("图片自动切换模型"),
                            description: "检测到图片请求时自动切换至视觉模型处理，无需手动指定"
                        ) {
                            glassToggle(isOn: $configManager.imageAutoSwitchModel)
                        }

                        settingRow(
                            icon: "bubble.left.and.bubble.right",
                            iconColor: ThemeColors.info,
                            title: configManager.localizedString("视觉模型选择"),
                            description: "检测到图片时自动切换的目标视觉模型"
                        ) {
                            GlassPopupPicker(
                                options: [
                                    GlassPopupOption(label: "glm-4.6v", value: "glm-4.6v"),
                                    GlassPopupOption(label: "glm-5.1", value: "glm-5.1"),
                                    GlassPopupOption(label: "glm-4.7", value: "glm-4.7")
                                ],
                                selection: $configManager.visionModel,
                                width: 160
                            )
                        }
                    }

                    // 数据与隐私
                    groupLabel("数据与隐私")
                    settingsGroup {
                        settingRow(
                            icon: "waveform.path.ecg",
                            iconColor: ThemeColors.success,
                            title: configManager.localizedString("用量上报"),
                            description: "定期向上游服务上报匿名使用统计，帮助改进服务质量"
                        ) {
                            glassToggle(isOn: $configManager.telemetryEnabled)
                        }

                        settingRow(
                            icon: "lock.fill",
                            iconColor: ThemeColors.info,
                            title: configManager.localizedString("API 访问密码"),
                            description: "为外部客户端设置访问密码，保护代理接口安全"
                        ) {
                            HStack(spacing: 8) {
                                if configManager.apiPassword.isEmpty {
                                    Text("未设置")
                                        .font(.system(size: 12))
                                        .foregroundColor(c.textMuted)
                                } else {
                                    Text("已设置")
                                        .font(.system(size: 12, weight: .medium))
                                        .foregroundColor(ThemeColors.success)
                                }
                                SecureField("", text: $configManager.apiPassword)
                                    .textFieldStyle(.plain)
                                    .font(.system(size: 13, design: .monospaced))
                                    .foregroundColor(c.text)
                                    .frame(width: 100)
                                    .multilineTextAlignment(.trailing)
                            }
                        }
                    }

                    // 代理性能
                    groupLabel("代理性能")
                    settingsGroup {
                        settingRow(
                            icon: "arrow.up.arrow.down",
                            iconColor: ThemeColors.info,
                            title: configManager.localizedString("响应缓存"),
                            description: "缓存非流式响应以减少上游请求，提升重复查询速度"
                        ) {
                            glassToggle(isOn: $configManager.cacheEnabled)
                        }

                        settingRow(
                            icon: "clock",
                            iconColor: ThemeColors.warning,
                            title: configManager.localizedString("缓存有效期"),
                            description: "非流式响应在缓存中的保留时间"
                        ) {
                            HStack(spacing: 8) {
                                Slider(
                                    value: Binding(
                                        get: { Double(configManager.cacheTTL) },
                                        set: { configManager.cacheTTL = Int($0) }
                                    ),
                                    in: 60...3600,
                                    step: 60
                                )
                                .frame(width: 100)
                                Text("\(configManager.cacheTTL) 秒")
                                    .font(.system(size: 12, design: .monospaced))
                                    .foregroundColor(c.textSecondary)
                                    .frame(width: 52, alignment: .trailing)
                            }
                        }

                        settingRow(
                            icon: "person.2",
                            iconColor: ThemeColors.purple,
                            title: configManager.localizedString("最大并发数"),
                            description: "同时允许的最大上游请求数，超出后返回 429"
                        ) {
                            HStack(spacing: 8) {
                                Slider(
                                    value: Binding(
                                        get: { Double(configManager.maxConcurrentRequests) },
                                        set: { configManager.maxConcurrentRequests = Int($0) }
                                    ),
                                    in: 1...100,
                                    step: 1
                                )
                                .frame(width: 100)
                                Text("\(configManager.maxConcurrentRequests) 个")
                                    .font(.system(size: 12, design: .monospaced))
                                    .foregroundColor(c.textSecondary)
                                    .frame(width: 36, alignment: .trailing)
                            }
                        }

                        settingRow(
                            icon: "arrow.triangle.2.circlepath",
                            iconColor: ThemeColors.success,
                            title: configManager.localizedString("最大重试次数"),
                            description: "上游请求失败时的自动重试次数"
                        ) {
                            Stepper(
                                value: $configManager.maxRetries,
                                in: 0...10
                            ) {
                                Text("\(configManager.maxRetries) 次")
                                    .font(.system(size: 13, design: .monospaced))
                                    .foregroundColor(c.text)
                                    .frame(width: 40, alignment: .trailing)
                            }
                        }

                        settingRow(
                            icon: "xmark.circle",
                            iconColor: ThemeColors.danger,
                            title: configManager.localizedString("空闲超时"),
                            description: "上游流式连接无数据传输时的自动断开时间"
                        ) {
                            HStack(spacing: 8) {
                                Slider(
                                    value: Binding(
                                        get: { Double(configManager.cbResetTimeoutSecs) },
                                        set: { configManager.cbResetTimeoutSecs = Int($0) }
                                    ),
                                    in: 5...300,
                                    step: 5
                                )
                                .frame(width: 100)
                                Text("\(configManager.cbResetTimeoutSecs) 秒")
                                    .font(.system(size: 12, design: .monospaced))
                                    .foregroundColor(c.textSecondary)
                                    .frame(width: 52, alignment: .trailing)
                            }
                        }
                    }

                    // 调试与维护
                    groupLabel("调试与维护")
                    settingsGroup {
                        settingRow(
                            icon: "pencil",
                            iconColor: ThemeColors.warning,
                            title: configManager.localizedString("调试模式"),
                            description: "启用后记录详细的请求/响应调试信息到日志"
                        ) {
                            glassToggle(isOn: $configManager.debugMode)
                        }

                        settingRow(
                            icon: "doc.text",
                            iconColor: ThemeColors.info,
                            title: configManager.localizedString("日志文件"),
                            description: "服务运行日志的存储路径"
                        ) {
                            Text("~/.codebuddy-proxy/proxy.log")
                                .font(.system(size: 11, design: .monospaced))
                                .foregroundColor(c.textMuted)
                                .lineLimit(1)
                                .truncationMode(.middle)
                                .frame(width: 180, alignment: .trailing)
                        }

                        settingRow(
                            icon: "trash",
                            iconColor: ThemeColors.danger,
                            title: configManager.localizedString("清除缓存"),
                            description: "清空所有已缓存的响应数据"
                        ) {
                            Button("清除") {
                                configManager.cacheEnabled = false
                                DispatchQueue.main.asyncAfter(deadline: .now() + 0.1) {
                                    configManager.cacheEnabled = true
                                }
                            }
                            .font(.system(size: 12, weight: .medium))
                            .foregroundColor(ThemeColors.danger)
                            .padding(.horizontal, 14)
                            .padding(.vertical, 6)
                            .background(ThemeColors.dangerSubtle)
                            .clipShape(Capsule())
                            .buttonStyle(.plain)
                        }
                    }

                    // 外观
                    groupLabel(configManager.localizedString("外观"))
                    settingsGroup {
                        settingRow(
                            icon: "circle.lefthalf.filled",
                            iconColor: ThemeColors.info,
                            title: configManager.localizedString("外观模式"),
                            description: "选择界面深色或浅色外观，跟随系统将自动适配"
                        ) {
                            GlassSegmentedPicker(
                                options: [
                                    GlassSegmentedOption(label: "系统", value: "跟随系统"),
                                    GlassSegmentedOption(label: "浅色", value: "浅色"),
                                    GlassSegmentedOption(label: "深色", value: "深色")
                                ],
                                selection: $themeManager.appearanceModeRaw
                            )
                        }

                        settingRow(
                            icon: "network",
                            iconColor: ThemeColors.info,
                            title: "监听端口",
                            description: "代理服务监听的网络端口号，修改后重启生效"
                        ) {
                            Stepper(
                                value: $configManager.port,
                                in: 1024...65535,
                                step: 1
                            ) {
                                TextField("", value: $configManager.port, format: .number)
                                    .textFieldStyle(.plain)
                                    .font(.system(size: 13, design: .monospaced))
                                    .foregroundColor(c.text)
                                    .frame(width: 50)
                                    .multilineTextAlignment(.trailing)
                            }
                        }
                    }

                    // 关于
                    groupLabel("关于")
                    settingsGroup {
                        settingRow(
                            icon: "app.badge",
                            iconColor: ThemeColors.info,
                            title: "应用",
                            description: AppMeta.name
                        ) {
                            Text(AppMeta.bundleId)
                                .font(.system(size: 11, design: .monospaced))
                                .foregroundColor(c.textMuted)
                        }

                        settingRow(
                            icon: "number",
                            iconColor: ThemeColors.purple,
                            title: "版本",
                            description: "当前安装的应用版本号"
                        ) {
                            Text(
                                Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "—"
                            )
                            .font(.system(size: 13, design: .monospaced))
                            .foregroundColor(c.textSecondary)
                        }

                        settingRow(
                            icon: "hammer",
                            iconColor: ThemeColors.warning,
                            title: "构建号",
                            description: "当前版本的构建编号"
                        ) {
                            Text(
                                Bundle.main.infoDictionary?["CFBundleVersion"] as? String ?? "—"
                            )
                            .font(.system(size: 13, design: .monospaced))
                            .foregroundColor(c.textMuted)
                        }
                    }

                    // 恢复默认
                    Button {
                        configManager.resetToDefaults()
                    } label: {
                        HStack(spacing: 6) {
                            Image(systemName: "arrow.counterclockwise")
                                .font(.system(size: 12))
                            Text("恢复默认设置")
                                .font(.system(size: 13, weight: .medium))
                        }
                        .foregroundColor(ThemeColors.danger)
                        .padding(.horizontal, 20)
                        .padding(.vertical, 10)
                        .background(ThemeColors.dangerSubtle)
                        .clipShape(Capsule())
                    }
                    .buttonStyle(.plain)
                    .padding(.top, 8)
                    .padding(.bottom, 40)
                }
                .padding(.horizontal, 24)
            }
        }
        .background(c.bg)
    }

    // MARK: - 服务器信息横幅

    private var infoBanner: some View {
        HStack(spacing: 16) {
            // 左侧图标
            ZStack {
                RoundedRectangle(cornerRadius: 12)
                    .fill(
                        LinearGradient(
                            colors: [c.primary, ThemeColors.success],
                            startPoint: .topLeading,
                            endPoint: .bottomTrailing
                        )
                    )
                    .frame(width: 44, height: 44)
                    .shadow(color: c.primary.opacity(0.2), radius: 8, y: 4)

                Image(systemName: "server.rack")
                    .font(.system(size: 20))
                    .foregroundColor(.white)
            }

            // 中间文字
            VStack(alignment: .leading, spacing: 2) {
                Text("Uniview CodeBuddy Proxy")
                    .font(.system(size: 15, weight: .semibold))
                    .foregroundColor(c.text)
                Text("Swift · macOS · 上游 unvcoding.copilot.qq.com")
                    .font(.system(size: 12))
                    .foregroundColor(c.textMuted)
            }

            Spacer()

            // 右侧指标
            HStack(spacing: 20) {
                bannerMeta(value: "\(configManager.port)", label: "端口")
                bannerMeta(value: "v\(Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "—")", label: "版本")
            }
        }
        .padding(16)
        .background(
            RoundedRectangle(cornerRadius: c.radiusMD)
                .fill(c.primarySubtle)
                .overlay(
                    RoundedRectangle(cornerRadius: c.radiusMD)
                        .stroke(c.primary.opacity(0.1), lineWidth: 1)
                )
        )
        .padding(.horizontal, 24)
        .padding(.top, 16)
        .padding(.bottom, 4)
    }

    private func bannerMeta(value: String, label: String) -> some View {
        VStack(spacing: 2) {
            Text(value)
                .font(.system(size: 16, weight: .bold, design: .rounded))
                .foregroundColor(c.text)
            Text(label)
                .font(.system(size: 10))
                .foregroundColor(c.textMuted)
                .textCase(.uppercase)
        }
    }

    // MARK: - 分组标签

    private func groupLabel(_ text: String) -> some View {
        Text(text)
            .font(.system(size: 11, weight: .semibold))
            .foregroundColor(c.textMuted)
            .textCase(.uppercase)
            .tracking(0.06)
            .padding(.top, 8)
            .padding(.leading, 4)
    }

    // MARK: - 设置分组卡片

    private func settingsGroup<Content: View>(
        @ViewBuilder content: () -> Content
    ) -> some View {
        VStack(spacing: 0) {
            content()
        }
        .background(
            RoundedRectangle(cornerRadius: c.radiusMD)
                .fill(.ultraThinMaterial)
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
                .shadow(color: c.shadowGlassSMColor, radius: c.shadowGlassSMRadius, y: c.shadowGlassSMY)
        )
        .clipShape(RoundedRectangle(cornerRadius: c.radiusMD))
    }

    // MARK: - 设置行（图标 + 标题/描述 + 控件）

    private func settingRow<Control: View>(
        icon: String,
        iconColor: Color,
        title: String,
        description: String,
        @ViewBuilder control: () -> Control
    ) -> some View {
        HStack(spacing: 16) {
            // 左侧彩色图标
            RoundedRectangle(cornerRadius: 10)
                .fill(iconColor.opacity(0.12))
                .overlay(
                    RoundedRectangle(cornerRadius: 10)
                        .stroke(iconColor.opacity(0.1), lineWidth: 1)
                )
                .overlay(
                    Image(systemName: icon)
                        .font(.system(size: 18))
                        .foregroundColor(iconColor)
                )
                .frame(width: 40, height: 40)

            // 中间标题和描述
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(.system(size: 14, weight: .medium))
                    .foregroundColor(c.text)
                Text(description)
                    .font(.system(size: 12))
                    .foregroundColor(c.textMuted)
                    .lineLimit(2)
            }

            Spacer()

            // 右侧控件
            control()
        }
        .padding(.horizontal, 18)
        .padding(.vertical, 14)
        .overlay(alignment: .bottom) {
            Rectangle()
                .fill(c.glassBorder.opacity(0.5))
                .frame(height: 0.5)
                .padding(.horizontal, 18)
        }
    }

    // MARK: - Glass Toggle

    private func glassToggle(isOn: Binding<Bool>) -> some View {
        Toggle("", isOn: isOn)
            .toggleStyle(.switch)
            .controlSize(.small)
    }

    // MARK: - 自动启动切换

    private func toggleAutoLaunch() {
        do {
            if SMAppService.mainApp.status == .enabled {
                try SMAppService.mainApp.unregister()
            } else {
                try SMAppService.mainApp.register()
            }
        } catch {
            // 静默处理
        }
    }
}
