import Foundation
import Combine

// ═══════════════════════════════════════════════
// LogEntry — 日志条目
// ═══════════════════════════════════════════════

struct LogEntry: Identifiable, Sendable {
    let id: UUID
    let timestamp: Date
    let level: LogLevel
    let message: String

    init(timestamp: Date = Date(), level: LogLevel, message: String) {
        self.id = UUID()
        self.timestamp = timestamp
        self.level = level
        self.message = message
    }

    /// SSE 格式的时间戳
    var sseTimestamp: String {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter.string(from: timestamp)
    }

    /// 可读的简短时间
    var shortTime: String {
        let formatter = DateFormatter()
        formatter.dateFormat = "HH:mm:ss.SSS"
        return formatter.string(from: timestamp)
    }
}

// ═══════════════════════════════════════════════
// LogLevel — 日志级别
// ═══════════════════════════════════════════════

enum LogLevel: String, Sendable, CaseIterable {
    case debug = "debug"
    case info = "info"
    case warn = "warn"
    case error = "error"

    var emoji: String {
        switch self {
        case .debug: return "🔍"
        case .info:  return "ℹ️"
        case .warn:  return "⚠️"
        case .error: return "❌"
        }
    }

    /// 用于 SSE 事件的标签
    var sseTag: String {
        switch self {
        case .debug: return "debug"
        case .info:  return "info"
        case .warn:  return "warn"
        case .error: return "error"
        }
    }
}

// ═══════════════════════════════════════════════
// LogBuffer — 线程安全的环形日志缓冲区
// ═══════════════════════════════════════════════

final class LogBuffer: ObservableObject {

    // MARK: - 常量

    /// 环形缓冲区容量
    static let capacity = 10_000

    /// 心跳间隔（秒）
    private static let heartbeatInterval: TimeInterval = 15

    // MARK: - 线程安全存储

    /// 内部存储 actor，隔离并发访问
    private let storage = LogStorage()

    // MARK: - 订阅管理

    /// 活跃的 SSE 订阅者续命令牌
    private var subscriberContinuations: [UUID: AsyncStream<LogEvent>.Continuation] = [:]
    private let continuationsLock = NSLock()

    // MARK: - 文件日志

    private let fileQueue = DispatchQueue(label: "com.codebuddy.logbuffer.file", qos: .utility)
    private var logFileHandle: FileHandle?
    private var currentLogSize: Int64 = 0

    /// 日志文件最大字节数（由 ConfigManager 驱动，默认 50MB）
    var maxLogSizeMB: Int = Defaults.logMaxSizeMB {
        didSet { maxLogSizeBytes = Int64(maxLogSizeMB) * 1024 * 1024 }
    }

    private var maxLogSizeBytes: Int64 = Int64(Defaults.logMaxSizeMB) * 1024 * 1024

    // MARK: - @Published（SwiftUI 绑定）

    /// 最近的日志条目快照（供 SwiftUI 列表显示，最多保留 500 条）
    @Published private(set) var recentEntries: [LogEntry] = []

    /// 当前缓冲区中的总条目数
    @Published private(set) var totalEntryCount: Int = 0

    // MARK: - 日志事件（用于 SSE 流）

    enum LogEvent: Sendable {
        case entry(LogEntry)
        case heartbeat
    }

    // MARK: - Init

    init() {
        setupLogFile()
    }

    deinit {
        try? logFileHandle?.close()
    }

    // MARK: - 追加日志

    /// 追加一条日志
    func append(_ level: LogLevel, _ message: String) {
        let entry = LogEntry(level: level, message: message)
        appendEntry(entry)
    }

    /// 追加 info 级别日志
    func info(_ message: String) {
        append(.info, message)
    }

    /// 追加 warn 级别日志
    func warn(_ message: String) {
        append(.warn, message)
    }

    /// 追加 error 级别日志
    func error(_ message: String) {
        append(.error, message)
    }

    /// 追加 debug 级别日志
    func debug(_ message: String) {
        append(.debug, message)
    }

    private func appendEntry(_ entry: LogEntry) {
        Task {
            await storage.append(entry)

            // 预先在 actor 中计算，避免在 MainActor.run 闭包中 await
            let newCount = await storage.count
            let snapshot = await storage.recent(500)

            // 更新 @Published（主线程）
            await MainActor.run {
                self.totalEntryCount = newCount
                self.recentEntries = snapshot
            }

            // 写入文件
            writeToFile(entry)

            // 推送给 SSE 订阅者
            emitToSubscribers(.entry(entry))
        }
    }

    // MARK: - 查询

    /// 获取最近 N 条日志
    func recent(_ count: Int) async -> [LogEntry] {
        await storage.recent(count)
    }

    /// 获取所有缓冲区内的日志
    func allEntries() async -> [LogEntry] {
        await storage.all()
    }

    /// 按级别过滤
    func entries(matchingLevel level: LogLevel) async -> [LogEntry] {
        await storage.filter { $0.level == level }
    }

    // MARK: - SSE 日志流

    /// 创建一个 SSE 日志流：先发送历史积压，然后实时推送 + 心跳
    func logStream(backlog: Int = 100) -> AsyncStream<LogEvent> {
        AsyncStream { continuation in
            let subId = UUID()

            // 发送历史积压
            Task {
                let history = await storage.recent(backlog)
                for entry in history {
                    continuation.yield(.entry(entry))
                }
            }

            // 注册续命令牌
            continuationsLock.lock()
            subscriberContinuations[subId] = continuation
            continuationsLock.unlock()

            continuation.onTermination = { [weak self] _ in
                self?.continuationsLock.lock()
                self?.subscriberContinuations.removeValue(forKey: subId)
                self?.continuationsLock.unlock()
            }
        }
    }

    /// 生成 SSE 格式的事件文本
    func sseEventData(for event: LogEvent) -> String {
        switch event {
        case .entry(let entry):
            let data = """
            {"timestamp":"\(entry.sseTimestamp)","level":"\(entry.level.rawValue)","message":\(escapeJSON(entry.message))}
            """
            return "event: log\ndata: \(data)\n\n"

        case .heartbeat:
            return "event: heartbeat\ndata: {\"timestamp\":\"\(ISO8601DateFormatter().string(from: Date()))\"}\n\n"
        }
    }

    /// 启动心跳定时器（应在 SSE 连接建立后调用）
    func startHeartbeat() -> Task<Void, Never> {
        Task {
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: UInt64(Self.heartbeatInterval) * 1_000_000_000)
                guard !Task.isCancelled else { return }
                emitToSubscribers(.heartbeat)
            }
        }
    }

    // MARK: - 订阅者广播

    private func emitToSubscribers(_ event: LogEvent) {
        continuationsLock.lock()
        let continuations = subscriberContinuations.values
        continuationsLock.unlock()

        for continuation in continuations {
            continuation.yield(event)
        }
    }

    // MARK: - 文件日志

    private func setupLogFile() {
        fileQueue.async { [weak self] in
            self?.performLogFileSetup()
        }
    }

    private func performLogFileSetup() {
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        let dir = "\(home)/.codebuddy-proxy"

        // 确保目录存在
        try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)

        let path = "\(dir)/proxy.log"

        // 如果文件不存在则创建
        if !FileManager.default.fileExists(atPath: path) {
            FileManager.default.createFile(atPath: path, contents: nil)
        }

        // 打开文件句柄
        if let handle = FileHandle(forWritingAtPath: path) {
            handle.seekToEndOfFile()
            self.currentLogSize = Int64(handle.offsetInFile)
            self.logFileHandle = handle
        }

        // 检查是否需要截断
        checkAndTruncateIfNeeded()
    }

    private func writeToFile(_ entry: LogEntry) {
        fileQueue.async { [weak self] in
            self?.performWrite(entry)
        }
    }

    private func performWrite(_ entry: LogEntry) {
        guard let handle = logFileHandle else { return }

        let line = "\(entry.shortTime) [\(entry.level.rawValue.uppercased())] \(entry.message)\n"
        guard let data = line.data(using: .utf8) else { return }

        handle.write(data)
        currentLogSize += Int64(data.count)

        checkAndTruncateIfNeeded()
    }

    private func checkAndTruncateIfNeeded() {
        guard currentLogSize > maxLogSizeBytes else { return }

        // 截断：关闭文件 → 清空 → 重新打开
        try? logFileHandle?.close()
        logFileHandle = nil

        let home = FileManager.default.homeDirectoryForCurrentUser.path
        let path = "\(home)/.codebuddy-proxy/proxy.log"

        // 清空文件内容
        FileManager.default.createFile(atPath: path, contents: nil)

        if let handle = FileHandle(forWritingAtPath: path) {
            self.logFileHandle = handle
            self.currentLogSize = 0
        }
    }

    // MARK: - 清空缓冲区

    /// 清空内存中的所有日志条目
    func clear() async {
        await storage.clear()
        await MainActor.run {
            self.recentEntries = []
            self.totalEntryCount = 0
        }
    }

    // MARK: - JSON 转义

    private func escapeJSON(_ string: String) -> String {
        var escaped = string
        escaped = escaped.replacingOccurrences(of: "\\", with: "\\\\")
        escaped = escaped.replacingOccurrences(of: "\"", with: "\\\"")
        escaped = escaped.replacingOccurrences(of: "\n", with: "\\n")
        escaped = escaped.replacingOccurrences(of: "\r", with: "\\r")
        escaped = escaped.replacingOccurrences(of: "\t", with: "\\t")
        return "\"\(escaped)\""
    }
}

// ═══════════════════════════════════════════════
// LogStorage — Actor 隔离的环形缓冲区内部存储
// ═══════════════════════════════════════════════

actor LogStorage {

    /// 环形缓冲区
    private var ring: [LogEntry] = []

    /// 写入位置（模运算）
    private var writeIndex: Int = 0

    /// 是否已写满一圈
    private var hasWrapped: Bool = false

    /// 容量
    private let capacity: Int

    init(capacity: Int = LogBuffer.capacity) {
        self.capacity = capacity
        self.ring = []
        ring.reserveCapacity(capacity)
    }

    /// 当前存储的条目数
    var count: Int {
        hasWrapped ? capacity : writeIndex
    }

    /// 追加一条日志
    func append(_ entry: LogEntry) {
        if writeIndex < capacity {
            ring.append(entry)
        } else {
            let idx = writeIndex % capacity
            ring[idx] = entry
        }
        writeIndex += 1
        if writeIndex >= capacity && !hasWrapped {
            hasWrapped = true
        }
    }

    /// 获取最近 N 条日志（按时间升序）
    func recent(_ count: Int) -> [LogEntry] {
        let total = self.count
        let start = max(0, total - count)

        var result: [LogEntry] = []
        result.reserveCapacity(min(count, total))

        for i in start..<total {
            let idx = i % capacity
            result.append(ring[idx])
        }
        return result
    }

    /// 获取所有日志（按时间升序）
    func all() -> [LogEntry] {
        recent(count)
    }

    /// 按条件过滤
    func filter(_ isIncluded: (LogEntry) -> Bool) -> [LogEntry] {
        var result: [LogEntry] = []
        let total = count
        for i in 0..<total {
            let idx = i % capacity
            let entry = ring[idx]
            if isIncluded(entry) {
                result.append(entry)
            }
        }
        return result
    }

    /// 清空
    func clear() {
        ring.removeAll(keepingCapacity: true)
        writeIndex = 0
        hasWrapped = false
    }
}
