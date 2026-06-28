import SwiftUI
import os
import AppKit
import ServiceManagement
import Combine

// ═══════════════════════════════════════════════
// AppServices — 共享服务单例，AppDelegate 和 SwiftUI 共用
// ═══════════════════════════════════════════════

@MainActor
enum AppServices {
    static let configManager = ConfigManager()
    static let tokenManager = TokenManager()
    static let authService = AuthService()
    static let logBuffer = LogBuffer()
    static let telemetryReporter = TelemetryReporter(
        configManager: configManager,
        tokenManager: tokenManager
    )
    static let usageStats = UsageStats()
    static let themeManager = ThemeManager()
}

@main
struct UniviewCodeBuddyProxyApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) var appDelegate
    @StateObject private var themeManager = AppServices.themeManager
    @StateObject private var configManager = AppServices.configManager
    @StateObject private var tokenManager = AppServices.tokenManager
    @StateObject private var authService = AppServices.authService
    @StateObject private var logBuffer = AppServices.logBuffer
    @StateObject private var telemetryReporter = AppServices.telemetryReporter
    @StateObject private var usageStats = AppServices.usageStats

    var body: some Scene {
        WindowGroup {
            ContentView()
                .preferredColorScheme(themeManager.effectiveColorScheme)
                .environmentObject(themeManager)
                .environmentObject(configManager)
                .environmentObject(tokenManager)
                .environmentObject(authService)
                .environmentObject(logBuffer)
                .environmentObject(telemetryReporter)
                .environmentObject(usageStats)
        }
        .windowStyle(.hiddenTitleBar)
        .windowResizability(.contentSize)
        .defaultSize(width: 1200, height: 800)
        .commands {
            CommandGroup(replacing: .appInfo) {
                Button("关于 Uniview CodeBuddy Proxy") {
                    NSApp.orderFrontStandardAboutPanel(
                        options: [
                            .applicationName: "Uniview CodeBuddy Proxy",
                            .version: Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "0.1.0",
                        ]
                    )
                }
            }
        }
    }
}

// ═══════════════════════════════════════════════
// AppDelegate — 系统托盘 + 代理服务器生命周期
// ═══════════════════════════════════════════════

class AppDelegate: NSObject, NSApplicationDelegate {
    var statusItem: NSStatusItem!
    var proxyServer: ProxyServer?
    private var cancellables = Set<AnyCancellable>()

    func applicationDidFinishLaunching(_ notification: Notification) {
        setupStatusBar()
        // 在主线程异步启动，确保 @MainActor 隔离的 AppServices 可访问
        DispatchQueue.main.async {
            self.startProxyServer()
        }

        // 监听端口变更 → 重启服务器
        AppServices.configManager.$port
            .dropFirst() // 跳过初始值
            .removeDuplicates()
            .debounce(for: .milliseconds(500), scheduler: DispatchQueue.main)
            .sink { [weak self] newPort in
                guard let self else { return }
                AppServices.logBuffer.info("端口变更: \(newPort)，重启服务器...")
                self.proxyServer?.restart()
            }
            .store(in: &cancellables)
    }

    // MARK: - 系统托盘

    private func setupStatusBar() {
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)

        if let button = statusItem.button {
            button.image = NSImage(systemSymbolName: "network", accessibilityDescription: "Uniview CodeBuddy Proxy")
            button.image?.size = NSSize(width: 18, height: 18)
        }

        let menu = NSMenu()

        menu.addItem(withTitle: "打开管理面板", action: #selector(showPanel), keyEquivalent: "o")
        menu.addItem(.separator())
        menu.addItem(withTitle: "服务运行中", action: nil, keyEquivalent: "").isEnabled = false

        let autoStartItem = menu.addItem(withTitle: "开机自动启动", action: #selector(toggleAutoStart), keyEquivalent: "")
        autoStartItem.state = SMAppService.mainApp.status == .enabled ? .on : .off

        menu.addItem(.separator())
        menu.addItem(withTitle: "退出 Uniview CodeBuddy Proxy", action: #selector(quitApp), keyEquivalent: "q")

        statusItem.menu = menu
    }

    /// 启动代理服务器
    @MainActor
    private func startProxyServer() {
        guard proxyServer == nil else { return }  // 避免重复启动

        proxyServer = ProxyServer(
            configManager: AppServices.configManager,
            tokenManager: AppServices.tokenManager,
            authService: AppServices.authService,
            logBuffer: AppServices.logBuffer,
            telemetryReporter: AppServices.telemetryReporter,
            usageStats: AppServices.usageStats
        )
        proxyServer?.start()
        updateStatusIcon(running: true)
        os_log(.info, "代理服务器已在端口 %d 启动", AppServices.configManager.port)
    }

    @objc private func showPanel() {
        NSApp.activate(ignoringOtherApps: true)
        if let window = NSApp.windows.first(where: { $0.isVisible }) {
            window.makeKeyAndOrderFront(nil)
        }
    }

    @objc private func toggleAutoStart() {
        do {
            if SMAppService.mainApp.status == .enabled {
                try SMAppService.mainApp.unregister()
            } else {
                try SMAppService.mainApp.register()
            }
            if let menu = statusItem.menu,
               let item = menu.items.first(where: { $0.action == #selector(toggleAutoStart) }) {
                item.state = SMAppService.mainApp.status == .enabled ? .on : .off
            }
        } catch {
            os_log(.error, "自动启动设置失败: %{public}@", error.localizedDescription)
        }
    }

    @objc private func quitApp() {
        proxyServer?.stop()
        NSApp.terminate(nil)
    }

    private func updateStatusIcon(running: Bool) {
        DispatchQueue.main.async {
            if let button = self.statusItem.button {
                let symbolName = running ? "network" : "network.slash"
                button.image = NSImage(systemSymbolName: symbolName, accessibilityDescription: running ? "运行中" : "已停止")
                button.image?.size = NSSize(width: 18, height: 18)
            }
        }
    }

    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        if !flag {
            showPanel()
        }
        return true
    }
}
