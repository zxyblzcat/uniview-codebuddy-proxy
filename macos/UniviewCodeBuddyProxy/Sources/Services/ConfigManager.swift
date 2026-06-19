import Foundation
import Combine

// ═══════════════════════════════════════════════
// ConfigManager — UserDefaults 驱动的热重载配置管理
// ═══════════════════════════════════════════════

final class ConfigManager: ObservableObject {

    // MARK: - UserDefaults 键名

    private enum Key {
        static let port                    = "port"
        static let apiPassword             = "apiPassword"
        static let cacheEnabled            = "cacheEnabled"
        static let cacheTTL                = "cacheTTL"
        static let debugMode               = "debugMode"
        static let claudeInject            = "claudeInject"
        static let maxRetries              = "maxRetries"
        static let cbMaxFailures           = "cbMaxFailures"
        static let cbResetTimeoutSecs      = "cbResetTimeoutSecs"
        static let cooldownDurationSecs    = "cooldownDurationSecs"
        static let telemetryEnabled        = "telemetryEnabled"
        static let imageAutoSwitchModel  = "imageAutoSwitchModel"
        static let visionModel             = "visionModel"
        static let maxConcurrentRequests   = "maxConcurrentRequests"
        static let upstreamMaxConnsPerHost = "upstreamMaxConnsPerHost"
        static let locale                  = "locale"
        static let logMaxSizeMB            = "logMaxSizeMB"
        static let logCleanupInterval      = "logCleanupInterval"
    }

    private let defaults = UserDefaults.standard
    private var cancellables = Set<AnyCancellable>()

    // MARK: - @Published 配置属性

    @Published var port: Int {
        didSet { defaults.set(port, forKey: Key.port) }
    }

    @Published var apiPassword: String {
        didSet { defaults.set(apiPassword, forKey: Key.apiPassword) }
    }

    @Published var cacheEnabled: Bool {
        didSet { defaults.set(cacheEnabled, forKey: Key.cacheEnabled) }
    }

    @Published var cacheTTL: Int {
        didSet { defaults.set(cacheTTL, forKey: Key.cacheTTL) }
    }

    @Published var debugMode: Bool {
        didSet { defaults.set(debugMode, forKey: Key.debugMode) }
    }

    @Published var claudeInject: Bool {
        didSet { defaults.set(claudeInject, forKey: Key.claudeInject) }
    }

    @Published var maxRetries: Int {
        didSet { defaults.set(maxRetries, forKey: Key.maxRetries) }
    }

    @Published var cbMaxFailures: Int {
        didSet { defaults.set(cbMaxFailures, forKey: Key.cbMaxFailures) }
    }

    @Published var cbResetTimeoutSecs: Int {
        didSet { defaults.set(cbResetTimeoutSecs, forKey: Key.cbResetTimeoutSecs) }
    }

    @Published var cooldownDurationSecs: Int {
        didSet { defaults.set(cooldownDurationSecs, forKey: Key.cooldownDurationSecs) }
    }

    @Published var telemetryEnabled: Bool {
        didSet { defaults.set(telemetryEnabled, forKey: Key.telemetryEnabled) }
    }

    @Published var imageAutoSwitchModel: Bool {
        didSet { defaults.set(imageAutoSwitchModel, forKey: Key.imageAutoSwitchModel) }
    }

    @Published var visionModel: String {
        didSet { defaults.set(visionModel, forKey: Key.visionModel) }
    }

    @Published var maxConcurrentRequests: Int {
        didSet { defaults.set(maxConcurrentRequests, forKey: Key.maxConcurrentRequests) }
    }

    @Published var upstreamMaxConnsPerHost: Int {
        didSet { defaults.set(upstreamMaxConnsPerHost, forKey: Key.upstreamMaxConnsPerHost) }
    }

    @Published var locale: String {
        didSet { defaults.set(locale, forKey: Key.locale) }
    }

    @Published var logMaxSizeMB: Int {
        didSet { defaults.set(logMaxSizeMB, forKey: Key.logMaxSizeMB) }
    }

    @Published var logCleanupInterval: Int {
        didSet { defaults.set(logCleanupInterval, forKey: Key.logCleanupInterval) }
    }

    // MARK: - 本地化字符串
    // 中文 → 英文 映射
    private let localizationMap: [String: [String: String]] = [
        "开机自动启动": ["zh-CN": "开机自动启动", "en": "Auto Launch"],
        "界面语言": ["zh-CN": "界面语言", "en": "Interface Language"],
        "系统托盘图标": ["zh-CN": "系统托盘图标", "en": "System Tray Icon"],
        "图片自动切换模型": ["zh-CN": "图片自动切换模型", "en": "Auto Switch Model for Images"],
        "视觉模型选择": ["zh-CN": "视觉模型选择", "en": "Vision Model Selection"],
        "用量上报": ["zh-CN": "用量上报", "en": "Telemetry"],
        "API 访问密码": ["zh-CN": "API 访问密码", "en": "API Access Password"],
        "响应缓存": ["zh-CN": "响应缓存", "en": "Response Caching"],
        "缓存有效期": ["zh-CN": "缓存有效期", "en": "Cache TTL"],
        "最大并发数": ["zh-CN": "最大并发数", "en": "Max Concurrent Requests"],
        "最大重试次数": ["zh-CN": "最大重试次数", "en": "Max Retries"],
        "空闲超时": ["zh-CN": "空闲超时", "en": "Idle Timeout"],
        "调试模式": ["zh-CN": "调试模式", "en": "Debug Mode"],
        "日志文件": ["zh-CN": "日志文件", "en": "Log File"],
        "清除缓存": ["zh-CN": "清除缓存", "en": "Clear Cache"],
        "外观": ["zh-CN": "外观", "en": "Appearance"],
        "主题预设": ["zh-CN": "主题预设", "en": "Theme Presets"],
        "监听端口": ["zh-CN": "监听端口", "en": "Listen Port"],
        "应用": ["zh-CN": "应用", "en": "Application"],
        "版本": ["zh-CN": "版本", "en": "Version"],
        "构建号": ["zh-CN": "构建号", "en": "Build Number"],
        "恢复默认设置": ["zh-CN": "恢复默认设置", "en": "Reset to Defaults"],
        "应用偏好": ["zh-CN": "应用偏好", "en": "Application Preferences"],
        "智能功能": ["zh-CN": "智能功能", "en": "Smart Features"],
        "数据与隐私": ["zh-CN": "数据与隐私", "en": "Data & Privacy"],
        "代理性能": ["zh-CN": "代理性能", "en": "Proxy Performance"],
        "调试与维护": ["zh-CN": "调试与维护", "en": "Debug & Maintenance"],
        "未设置": ["zh-CN": "未设置", "en": "Not Set"],
        "已设置": ["zh-CN": "已设置", "en": "Set"],
        "秒": ["zh-CN": "秒", "en": "s"],
        "个": ["zh-CN": "个", "en": "items"],
        "次": ["zh-CN": "次", "en": "times"],
        "关于": ["zh-CN": "关于", "en": "About"]
    ]

    /// 获取本地化字符串
    func localizedString(_ key: String) -> String {
        return localizationMap[key]?[locale] ?? key
    }

    // MARK: - Init

    init() {
        // 从 UserDefaults 读取，缺失则用 Defaults 默认值
        self.port                    = defaults.object(forKey: Key.port) as? Int ?? Defaults.port
        self.apiPassword             = defaults.string(forKey: Key.apiPassword) ?? ""
        self.cacheEnabled            = defaults.object(forKey: Key.cacheEnabled) as? Bool ?? Defaults.cacheEnabled
        self.cacheTTL                = defaults.object(forKey: Key.cacheTTL) as? Int ?? Defaults.cacheTTL
        self.debugMode               = defaults.object(forKey: Key.debugMode) as? Bool ?? Defaults.debugMode
        self.claudeInject            = defaults.object(forKey: Key.claudeInject) as? Bool ?? Defaults.claudeInject
        self.maxRetries              = defaults.object(forKey: Key.maxRetries) as? Int ?? Defaults.maxRetries
        self.cbMaxFailures           = defaults.object(forKey: Key.cbMaxFailures) as? Int ?? Defaults.cbMaxFailures
        self.cbResetTimeoutSecs      = defaults.object(forKey: Key.cbResetTimeoutSecs) as? Int ?? Defaults.cbResetTimeoutSecs
        self.cooldownDurationSecs    = defaults.object(forKey: Key.cooldownDurationSecs) as? Int ?? Defaults.cooldownDurationSecs
        self.telemetryEnabled        = defaults.object(forKey: Key.telemetryEnabled) as? Bool ?? Defaults.telemetryEnabled
        self.imageAutoSwitchModel   = defaults.object(forKey: Key.imageAutoSwitchModel) as? Bool ?? Defaults.imageAutoSwitchModel
        self.visionModel             = defaults.string(forKey: Key.visionModel) ?? Defaults.visionModel
        self.maxConcurrentRequests   = defaults.object(forKey: Key.maxConcurrentRequests) as? Int ?? Defaults.maxConcurrentRequests
        self.upstreamMaxConnsPerHost = defaults.object(forKey: Key.upstreamMaxConnsPerHost) as? Int ?? Defaults.upstreamMaxConnsPerHost
        self.locale                  = defaults.string(forKey: Key.locale) ?? "zh-CN"
        self.logMaxSizeMB            = defaults.object(forKey: Key.logMaxSizeMB) as? Int ?? Defaults.logMaxSizeMB
        self.logCleanupInterval      = defaults.object(forKey: Key.logCleanupInterval) as? Int ?? Defaults.logCleanupInterval

        // 监听外部 UserDefaults 变更（其他进程或 KVO 触发），实现热重载
        setupExternalChangeObserver()
    }

    // MARK: - 外部变更热重载

    /// 监听 UserDefaults 的分布式通知，支持多进程场景下的配置同步
    private func setupExternalChangeObserver() {
        NotificationCenter.default.publisher(
            for: UserDefaults.didChangeNotification,
            object: defaults
        )
        .debounce(for: .milliseconds(100), scheduler: RunLoop.main)
        .sink { [weak self] _ in
            self?.reloadFromDefaults()
        }
        .store(in: &cancellables)
    }

    /// 从 UserDefaults 重新加载所有值（不触发 didSet 写回）
    private func reloadFromDefaults() {
        let newPort                    = defaults.object(forKey: Key.port) as? Int ?? Defaults.port
        let newApiPassword             = defaults.string(forKey: Key.apiPassword) ?? ""
        let newCacheEnabled            = defaults.object(forKey: Key.cacheEnabled) as? Bool ?? Defaults.cacheEnabled
        let newCacheTTL                = defaults.object(forKey: Key.cacheTTL) as? Int ?? Defaults.cacheTTL
        let newDebugMode               = defaults.object(forKey: Key.debugMode) as? Bool ?? Defaults.debugMode
        let newClaudeInject            = defaults.object(forKey: Key.claudeInject) as? Bool ?? Defaults.claudeInject
        let newMaxRetries              = defaults.object(forKey: Key.maxRetries) as? Int ?? Defaults.maxRetries
        let newCbMaxFailures           = defaults.object(forKey: Key.cbMaxFailures) as? Int ?? Defaults.cbMaxFailures
        let newCbResetTimeoutSecs      = defaults.object(forKey: Key.cbResetTimeoutSecs) as? Int ?? Defaults.cbResetTimeoutSecs
        let newCooldownDurationSecs    = defaults.object(forKey: Key.cooldownDurationSecs) as? Int ?? Defaults.cooldownDurationSecs
        let newTelemetryEnabled        = defaults.object(forKey: Key.telemetryEnabled) as? Bool ?? Defaults.telemetryEnabled
        let newImageAutoSwitchModel   = defaults.object(forKey: Key.imageAutoSwitchModel) as? Bool ?? Defaults.imageAutoSwitchModel
        let newVisionModel             = defaults.string(forKey: Key.visionModel) ?? Defaults.visionModel
        let newMaxConcurrentRequests   = defaults.object(forKey: Key.maxConcurrentRequests) as? Int ?? Defaults.maxConcurrentRequests
        let newUpstreamMaxConnsPerHost = defaults.object(forKey: Key.upstreamMaxConnsPerHost) as? Int ?? Defaults.upstreamMaxConnsPerHost
        let newLocale                  = defaults.string(forKey: Key.locale) ?? "zh-CN"
        let newLogMaxSizeMB            = defaults.object(forKey: Key.logMaxSizeMB) as? Int ?? Defaults.logMaxSizeMB
        let newLogCleanupInterval      = defaults.object(forKey: Key.logCleanupInterval) as? Int ?? Defaults.logCleanupInterval

        // 仅在值变化时更新 @Published，避免不必要的 SwiftUI 重绘
        if port != newPort { port = newPort }
        if apiPassword != newApiPassword { apiPassword = newApiPassword }
        if cacheEnabled != newCacheEnabled { cacheEnabled = newCacheEnabled }
        if cacheTTL != newCacheTTL { cacheTTL = newCacheTTL }
        if debugMode != newDebugMode { debugMode = newDebugMode }
        if claudeInject != newClaudeInject { claudeInject = newClaudeInject }
        if maxRetries != newMaxRetries { maxRetries = newMaxRetries }
        if cbMaxFailures != newCbMaxFailures { cbMaxFailures = newCbMaxFailures }
        if cbResetTimeoutSecs != newCbResetTimeoutSecs { cbResetTimeoutSecs = newCbResetTimeoutSecs }
        if cooldownDurationSecs != newCooldownDurationSecs { cooldownDurationSecs = newCooldownDurationSecs }
        if telemetryEnabled != newTelemetryEnabled { telemetryEnabled = newTelemetryEnabled }
        if imageAutoSwitchModel != newImageAutoSwitchModel { imageAutoSwitchModel = newImageAutoSwitchModel }
        if visionModel != newVisionModel { visionModel = newVisionModel }
        if maxConcurrentRequests != newMaxConcurrentRequests { maxConcurrentRequests = newMaxConcurrentRequests }
        if upstreamMaxConnsPerHost != newUpstreamMaxConnsPerHost { upstreamMaxConnsPerHost = newUpstreamMaxConnsPerHost }
        if locale != newLocale { locale = newLocale }
        if logMaxSizeMB != newLogMaxSizeMB { logMaxSizeMB = newLogMaxSizeMB }
        if logCleanupInterval != newLogCleanupInterval { logCleanupInterval = newLogCleanupInterval }
    }

    // MARK: - 重置为默认值

    /// 将所有配置恢复为默认值
    func resetToDefaults() {
        port = Defaults.port
        apiPassword = ""
        cacheEnabled = Defaults.cacheEnabled
        cacheTTL = Defaults.cacheTTL
        debugMode = Defaults.debugMode
        claudeInject = Defaults.claudeInject
        maxRetries = Defaults.maxRetries
        cbMaxFailures = Defaults.cbMaxFailures
        cbResetTimeoutSecs = Defaults.cbResetTimeoutSecs
        cooldownDurationSecs = Defaults.cooldownDurationSecs
        telemetryEnabled = Defaults.telemetryEnabled
        imageAutoSwitchModel = Defaults.imageAutoSwitchModel
        visionModel = Defaults.visionModel
        maxConcurrentRequests = Defaults.maxConcurrentRequests
        upstreamMaxConnsPerHost = Defaults.upstreamMaxConnsPerHost
        locale = "zh-CN"
        logMaxSizeMB = Defaults.logMaxSizeMB
        logCleanupInterval = Defaults.logCleanupInterval
    }

    // MARK: - 导出为环境变量字典

    /// 生成可供子进程继承的环境变量字典
    func asEnvironmentVariables() -> [String: String] {
        var env: [String: String] = [:]
        env["PORT"] = "\(port)"
        if !apiPassword.isEmpty { env["API_PASSWORD"] = apiPassword }
        env["CACHE_ENABLED"] = cacheEnabled ? "true" : "false"
        env["CACHE_TTL"] = "\(cacheTTL)"
        env["DEBUG_MODE"] = debugMode ? "true" : "false"
        env["CLAUDE_INJECT"] = claudeInject ? "true" : "false"
        env["MAX_RETRIES"] = "\(maxRetries)"
        env["CB_MAX_FAILURES"] = "\(cbMaxFailures)"
        env["CB_RESET_TIMEOUT_SECS"] = "\(cbResetTimeoutSecs)"
        env["COOLDOWN_DURATION_SECS"] = "\(cooldownDurationSecs)"
        env["TELEMETRY_ENABLED"] = telemetryEnabled ? "true" : "false"
        env["IMAGE_AUTO_SWITCH_MODEL"] = imageAutoSwitchModel ? "true" : "false"
        env["VISION_MODEL"] = visionModel
        env["MAX_CONCURRENT_REQUESTS"] = "\(maxConcurrentRequests)"
        env["UPSTREAM_MAX_CONNS_PER_HOST"] = "\(upstreamMaxConnsPerHost)"
        env["LOCALE"] = locale
        env["LOG_MAX_SIZE_MB"] = "\(logMaxSizeMB)"
        env["LOG_CLEANUP_INTERVAL"] = "\(logCleanupInterval)"
        return env
    }
}
