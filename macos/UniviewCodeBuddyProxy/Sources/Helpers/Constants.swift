import Foundation

/// 上游服务常量
enum Upstream {
    static let baseURL = "https://unvcoding.copilot.qq.com"
    static let domain = "unvcoding.copilot.qq.com"
    static let productVersion = "2.92.0"
    static let userAgent = "CLI/\(productVersion) CodeBuddy/\(productVersion)"

    // 端点
    static let authStateURL = "/v2/plugin/auth/state"
    static let authTokenURL = "/v2/plugin/auth/token"
    static let tokenRefreshURL = "/v2/plugin/auth/token/refresh"
    static let chatURL = "/v2/chat/completions"
    static let completionURL = "/v2/completions"
    static let embeddingURL = "/v2/embeddings"
    static let configURL = "/v2/config"
    static let reportURL = "/v2/report"
}

/// 默认配置值
enum Defaults {
    static let port: Int = 1026
    static let maxConcurrentRequests: Int = 20
    static let upstreamMaxConnsPerHost: Int = 50
    static let maxRetries: Int = 3
    static let cacheTTL: Int = 300
    static let cacheEnabled: Bool = false
    static let debugMode: Bool = false
    static let claudeInject: Bool = false
    static let telemetryEnabled: Bool = true
    static let imageAutoSwitchModel: Bool = true
    static let visionModel: String = "glm-4.6v"
    static let cbMaxFailures: Int = 5
    static let cbResetTimeoutSecs: Int = 30
    static let cooldownDurationSecs: Int = 30
    static let logMaxSizeMB: Int = 50
    static let logCleanupInterval: Int = 1800
    static let maxBodySizeMB: Int = 50
    static let idleTimeoutSecs: Int = 120
    static let modelsCacheTTL: Int = 300
    static let responseHeaderTimeoutSecs: Int = 1800  // 30 min
}

/// 应用元数据
enum AppMeta {
    static let name = "Uniview CodeBuddy Proxy"
    static let bundleId = "com.uniview.codebuddy-proxy"
}

/// 额外模型列表（上游可能不返回但需要暴露的模型）
let extraModels: [(name: String, ownedBy: String)] = [
    ("glm-5.1", "智谱"),
    ("glm-5.0", "智谱"),
    ("glm-4.7", "智谱"),
    ("glm-4.6", "智谱"),
    ("minimax-m2.7", "MiniMax"),
    ("minimax-m2.5", "MiniMax"),
    ("kimi-k2.5", "月之暗面"),
    ("deepseek-r1", "深度求索"),
    ("deepseek-v3-1-lkeap", "深度求索"),
    ("hunyuan-2.0-instruct", "腾讯"),
]

/// 模型 → 提供商推断
func inferOwnedBy(model: String) -> String {
    if model.hasPrefix("glm") { return "智谱" }
    if model.hasPrefix("minimax") { return "MiniMax" }
    if model.hasPrefix("kimi") { return "月之暗面" }
    if model.hasPrefix("deepseek") { return "深度求索" }
    if model.hasPrefix("hunyuan") { return "腾讯" }
    if model.hasPrefix("claude") { return "Anthropic" }
    if model.hasPrefix("gpt") { return "OpenAI" }
    if model.hasPrefix("gemini") { return "Google" }
    if model.hasPrefix("codebuddy") { return "腾讯" }
    return "未知"
}
