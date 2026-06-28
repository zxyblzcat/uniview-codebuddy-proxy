import Foundation
import Combine

// ═══════════════════════════════════════════════════════════════
// UsageStats — 代理使用量统计服务
// 跟踪请求次数、Token 用量、费用、缓存命中率、延迟等
// 按小时分桶 + 按模型分布，支持滚动 7 天持久化
// ═══════════════════════════════════════════════════════════════

@MainActor
final class UsageStats: ObservableObject {

    // MARK: - 数据模型

    /// 半小时分桶（48 个桶覆盖 24 小时）
    struct HourlyBucket: Codable {
        var hour: Int          // 0..47，半小时索引
        var requests: Int = 0
        var promptTokens: Int = 0
        var completionTokens: Int = 0
        var credit: Double = 0
    }

    /// 按模型聚合统计
    struct ModelStats: Codable {
        var requests: Int = 0
        var promptTokens: Int = 0
        var completionTokens: Int = 0
        var credit: Double = 0
    }

    /// 持久化文件格式
    struct DailySnapshot: Codable {
        var date: String
        var totalRequests: Int
        var totalPromptTokens: Int
        var totalCompletionTokens: Int
        var totalCredit: Double
        var cacheHitTokens: Int
        var cacheMissTokens: Int
        var successCount: Int
        var failureCount: Int
        var totalLatency: Double
        var hourlyBuckets: [HourlyBucket]
        var modelDistribution: [String: ModelStats]
    }

    // MARK: - @Published 状态

    @Published private(set) var totalRequests: Int = 0
    @Published private(set) var totalPromptTokens: Int = 0
    @Published private(set) var totalCompletionTokens: Int = 0
    @Published private(set) var totalCredit: Double = 0
    @Published private(set) var cacheHitTokens: Int = 0
    @Published private(set) var cacheMissTokens: Int = 0
    @Published private(set) var successCount: Int = 0
    @Published private(set) var failureCount: Int = 0
    @Published private(set) var totalLatency: Double = 0
    @Published private(set) var hourlyBuckets: [HourlyBucket] = []
    @Published private(set) var modelDistribution: [String: ModelStats] = [:]

    // MARK: - 计算属性

    /// 请求成功率 (0.0 ~ 1.0)
    var successRate: Double {
        guard totalRequests > 0 else { return 0 }
        return Double(successCount) / Double(totalRequests)
    }

    /// 平均延迟 (ms)
    var avgLatency: Double {
        guard totalRequests > 0 else { return 0 }
        return totalLatency / Double(totalRequests) * 1000
    }

    /// 缓存命中率 (0.0 ~ 1.0)
    var cacheHitRate: Double {
        let total = cacheHitTokens + cacheMissTokens
        guard total > 0 else { return 0 }
        return Double(cacheHitTokens) / Double(total)
    }

    /// 总 Token 数
    var totalTokens: Int {
        totalPromptTokens + totalCompletionTokens
    }

    // MARK: - 持久化

    /// 统计文件保存目录
    private static let statsDirectory: String = {
        let appSupport = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!
        return appSupport.appendingPathComponent("UniviewCodeBuddyProxy/stats").path
    }()

    /// 保存间隔（5 分钟）
    private static let saveInterval: TimeInterval = 300

    /// 滚动窗口天数
    private static let retentionDays: Int = 7

    /// 定时保存 timer
    private var saveTimer: Timer?

    /// 当前日期字符串（用于判断是否需要重置分桶）
    private var currentDate: String = ""

    // MARK: - Init

    init() {
        initializeBuckets()
        loadToday()
        cleanupOldFiles()
        startSaveLoop()
    }

    deinit {
        saveTimer?.invalidate()
        saveTimer = nil
    }

    // MARK: - 公开方法

    /// 记录一次请求（线程安全，可从任意上下文调用）
    func recordRequest(
        model: String,
        promptTokens: Int,
        completionTokens: Int,
        totalTokens: Int,
        credit: Double,
        cacheHitTokens: Int,
        cacheCreationInputTokens: Int,
        latency: Double,
        success: Bool
    ) {
        // 检查日期变化
        let today = Self.dateString(for: Date())
        if today != currentDate {
            save()                    // 保存旧日期数据
            resetCounters()           // 重置为新一天
            currentDate = today
            loadToday()               // 加载新日期已有数据
        }

        // 更新总计
        totalRequests += 1
        self.totalPromptTokens += promptTokens
        self.totalCompletionTokens += completionTokens
        self.totalCredit += credit
        self.cacheHitTokens += cacheHitTokens
        self.cacheMissTokens += cacheCreationInputTokens
        if success { successCount += 1 } else { failureCount += 1 }
        totalLatency += latency

        // 更新当前半小时分桶
        let bucketIdx = currentHalfHourIndex()
        if bucketIdx >= 0 && bucketIdx < hourlyBuckets.count {
            hourlyBuckets[bucketIdx].requests += 1
            hourlyBuckets[bucketIdx].promptTokens += promptTokens
            hourlyBuckets[bucketIdx].completionTokens += completionTokens
            hourlyBuckets[bucketIdx].credit += credit
        }

        // 更新模型分布
        let modelKey = simplifiedModelName(model)
        if modelDistribution[modelKey] == nil {
            modelDistribution[modelKey] = ModelStats()
        }
        modelDistribution[modelKey]?.requests += 1
        modelDistribution[modelKey]?.promptTokens += promptTokens
        modelDistribution[modelKey]?.completionTokens += completionTokens
        modelDistribution[modelKey]?.credit += credit
    }

    /// 手动保存（用于 shutdown）
    func shutdown() {
        save()
        saveTimer?.invalidate()
        saveTimer = nil
    }

    // MARK: - 私有方法

    /// 初始化 48 个半小时分桶
    private func initializeBuckets() {
        hourlyBuckets = (0..<48).map { HourlyBucket(hour: $0) }
        currentDate = Self.dateString(for: Date())
    }

    /// 重置所有计数器（新的一天）
    private func resetCounters() {
        totalRequests = 0
        totalPromptTokens = 0
        totalCompletionTokens = 0
        totalCredit = 0
        cacheHitTokens = 0
        cacheMissTokens = 0
        successCount = 0
        failureCount = 0
        totalLatency = 0
        hourlyBuckets = (0..<48).map { HourlyBucket(hour: $0) }
        modelDistribution = [:]
    }

    /// 当前半小时索引 (0..47)
    private func currentHalfHourIndex() -> Int {
        let calendar = Calendar.current
        let now = Date()
        let hour = calendar.component(.hour, from: now)
        let minute = calendar.component(.minute, from: now)
        return hour * 2 + (minute >= 30 ? 1 : 0)
    }

    /// 简化模型名称（去掉前缀和版本细节）
    private func simplifiedModelName(_ model: String) -> String {
        // 去掉常见前缀
        var name = model
        for prefix in ["deepseek-", "claude-", "gpt-", "qwen-", "glm-", "moonshot-"] {
            if name.hasPrefix(prefix) {
                name = String(name.dropFirst(prefix.count))
                break
            }
        }
        // 截断过长名称
        if name.count > 20 {
            name = String(name.prefix(20))
        }
        return name.isEmpty ? model : name
    }

    // MARK: - 持久化

    /// 日期字符串 "2026-06-28"
    private static func dateString(for date: Date) -> String {
        let formatter = DateFormatter()
        formatter.dateFormat = "yyyy-MM-dd"
        formatter.locale = Locale(identifier: "en_US_POSIX")
        return formatter.string(from: date)
    }

    /// 当天统计文件路径
    private func todayFilePath() -> String {
        let dir = Self.statsDirectory
        return (dir as NSString).appendingPathComponent("\(currentDate).json")
    }

    /// 加载当天数据（如果存在）
    private func loadToday() {
        let path = todayFilePath()
        guard FileManager.default.fileExists(atPath: path),
              let data = FileManager.default.contents(atPath: path),
              let snapshot = try? JSONDecoder().decode(DailySnapshot.self, from: data) else {
            return
        }

        // 只加载日期匹配的文件
        guard snapshot.date == currentDate else { return }

        totalRequests = snapshot.totalRequests
        totalPromptTokens = snapshot.totalPromptTokens
        totalCompletionTokens = snapshot.totalCompletionTokens
        totalCredit = snapshot.totalCredit
        cacheHitTokens = snapshot.cacheHitTokens
        cacheMissTokens = snapshot.cacheMissTokens
        successCount = snapshot.successCount
        failureCount = snapshot.failureCount
        totalLatency = snapshot.totalLatency
        hourlyBuckets = snapshot.hourlyBuckets
        modelDistribution = snapshot.modelDistribution
    }

    /// 保存当天数据到文件
    private func save() {
        let dir = Self.statsDirectory
        // 确保目录存在
        try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)

        let snapshot = DailySnapshot(
            date: currentDate,
            totalRequests: totalRequests,
            totalPromptTokens: totalPromptTokens,
            totalCompletionTokens: totalCompletionTokens,
            totalCredit: totalCredit,
            cacheHitTokens: cacheHitTokens,
            cacheMissTokens: cacheMissTokens,
            successCount: successCount,
            failureCount: failureCount,
            totalLatency: totalLatency,
            hourlyBuckets: hourlyBuckets,
            modelDistribution: modelDistribution
        )

        guard let data = try? JSONEncoder().encode(snapshot) else { return }
        try? data.write(to: URL(fileURLWithPath: todayFilePath()), options: .atomic)
    }

    /// 启动定时保存循环
    private func startSaveLoop() {
        saveTimer = Timer.scheduledTimer(withTimeInterval: Self.saveInterval, repeats: true) { [weak self] _ in
            Task { @MainActor in
                self?.save()
            }
        }
    }

    /// 清理 7 天前的统计文件
    private func cleanupOldFiles() {
        let dir = Self.statsDirectory
        guard let files = try? FileManager.default.contentsOfDirectory(atPath: dir) else { return }

        let calendar = Calendar.current
        let cutoffDate = calendar.date(byAdding: .day, value: -Self.retentionDays, to: Date())!

        for file in files {
            guard file.hasSuffix(".json") else { continue }
            let name = file.replacingOccurrences(of: ".json", with: "")
            // 解析日期 "2026-06-28"
            let formatter = DateFormatter()
            formatter.dateFormat = "yyyy-MM-dd"
            formatter.locale = Locale(identifier: "en_US_POSIX")
            guard let fileDate = formatter.date(from: name), fileDate < cutoffDate else { continue }

            let filePath = (dir as NSString).appendingPathComponent(file)
            try? FileManager.default.removeItem(atPath: filePath)
        }
    }
}
