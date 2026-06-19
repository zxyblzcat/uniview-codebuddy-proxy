import Foundation
import os
import Security
import Combine

// MARK: - TokenInfo

/// Token status information for API display.
struct TokenInfo: Identifiable, Encodable {
    let id: String // userID
    let userID: String
    let status: TokenStatus
    let createdAt: Int64
    let expiresAt: Int64
    let requestCount: Int
    let errorRate: Double
    let lastUsed: Date?

    enum TokenStatus: String, Codable {
        case active
        case cooldown
        case unavailable
        case expired
    }
}

// MARK: - TokenEntry

/// Internal entry in the token pool with health state.
struct TokenEntry {
    var token: TokenData
    var cooldownUntil: Date?
    var unavailable: Bool

    /// Whether the entry is currently in cooldown.
    var isInCooldown: Bool {
        guard let until = cooldownUntil else { return false }
        return Date() < until
    }

    /// Whether the token is expired (with 5-second drift tolerance).
    var isExpired: Bool {
        token.isExpired
    }
}

// MARK: - TokenManager

/// Token pool manager with round-robin selection, health awareness, and Keychain persistence.
@MainActor
final class TokenManager: ObservableObject {

    // MARK: Published

    @Published private(set) var entries: [TokenEntry] = []
    @Published private(set) var activeTokenCount: Int = 0

    /// Convenience accessor for all tokens (used by App.swift AppDelegate).
    var allTokens: [TokenData] {
        entries.map(\.token)
    }

    // MARK: Private State

    private var roundRobinIndex: Int = 0
    private let keychainService = "com.uniview.codebuddy-proxy"

    // MARK: Init

    init() {
        loadFromKeychain()
        migrateLegacyTokenFile()
        updateActiveCount()
    }

    // MARK: - Token Selection

    /// Returns the next available token using round-robin with health awareness.
    /// Skips unavailable, cooling-down, and expired tokens.
    /// Falls back to the token with the earliest cooldown expiry if all are in cooldown.
    func nextToken() -> TokenData? {
        let n = entries.count
        guard n > 0 else { return nil }

        // Try from current index, skipping unhealthy entries
        for i in 0..<n {
            let idx = (roundRobinIndex + i) % n
            let entry = entries[idx]

            if entry.unavailable { continue }
            if entry.isInCooldown { continue }
            if entry.isExpired { continue }
            if entry.token.bearer.isEmpty { continue }

            // Available: advance index
            roundRobinIndex = (idx + 1) % n
            return entry.token
        }

        // All tokens unavailable or in cooldown; find earliest cooldown expiry
        var bestEntry: TokenEntry?
        for entry in entries {
            if entry.unavailable { continue }
            if bestEntry == nil || (entry.cooldownUntil ?? .distantPast) < (bestEntry?.cooldownUntil ?? .distantPast) {
                bestEntry = entry
            }
        }
        if let best = bestEntry, !best.token.bearer.isEmpty {
            return best.token
        }

        return nil
    }

    // MARK: - Health Management

    /// Marks a token as in cooldown after a 429 response.
    /// - Parameters:
    ///   - userID: The user ID whose token should be cooled down.
    ///   - duration: Cooldown duration. Defaults to 30 seconds if zero or negative.
    func markCooldown(userID: String, duration: TimeInterval = 30) {
        let effectiveDuration = duration > 0 ? duration : 30
        for i in entries.indices {
            if entries[i].token.userID == userID {
                entries[i].cooldownUntil = Date().addingTimeInterval(effectiveDuration)
                os_log(.info, "Token for %{public}@ cooled down for %.0f seconds", userID, effectiveDuration)
                break
            }
        }
        updateActiveCount()
    }

    /// Marks a token as permanently unavailable after a 401 response.
    func markUnavailable(userID: String) {
        for i in entries.indices {
            if entries[i].token.userID == userID {
                entries[i].unavailable = true
                os_log(.info, "Token for %{public}@ marked unavailable", userID)
                break
            }
        }
        updateActiveCount()
    }

    // MARK: - Token Management

    /// Adds a token to the pool. Deduplicates by userID (updates existing).
    func addToken(_ token: TokenData) {
        // Deduplicate: update existing entry
        for i in entries.indices {
            if entries[i].token.userID == token.userID {
                entries[i].token = token
                entries[i].unavailable = false
                entries[i].cooldownUntil = nil
                saveToKeychain(token)
                updateActiveCount()
                return
            }
        }

        // New entry
        entries.append(TokenEntry(token: token, cooldownUntil: nil, unavailable: false))
        saveToKeychain(token)
        updateActiveCount()
    }

    /// Removes a token from the pool by userID.
    func removeToken(userID: String) {
        entries.removeAll { $0.token.userID == userID }
        deleteFromKeychain(userID: userID)
        updateActiveCount()
    }

    /// Returns the raw TokenData for a given userID (used by AuthController refresh).
    func getTokenData(userID: String) -> TokenData? {
        entries.first(where: { $0.token.userID == userID })?.token
    }

    /// Returns token info for all entries (for API display).
    func getAllTokens() -> [TokenInfo] {
        entries.map { entry in
            let status: TokenInfo.TokenStatus
            if entry.unavailable {
                status = .unavailable
            } else if entry.isExpired {
                status = .expired
            } else if entry.isInCooldown {
                status = .cooldown
            } else {
                status = .active
            }

            return TokenInfo(
                id: entry.token.userID,
                userID: entry.token.userID,
                status: status,
                createdAt: entry.token.createdAt,
                expiresAt: entry.token.expiresAt,
                requestCount: 0,
                errorRate: 0,
                lastUsed: nil
            )
        }
    }

    // MARK: - ProxyController 便捷访问

    /// ProxyController 使用的 token 获取方法（别名）
    func getNextToken() -> TokenData? {
        nextToken()
    }

    /// 当前 bearer token（用于遥测等场景）
    var currentBearerToken: String? {
        entries.first(where: { !$0.unavailable && !$0.isExpired && !$0.isInCooldown })?.token.bearer
    }

    /// 当前用户 ID（计算属性）
    var currentUserID: String? {
        entries.first(where: { !$0.unavailable && !$0.isExpired && !$0.isInCooldown })?.token.userID
    }

    /// ProxyController 使用的方法别名
    func getCurrentUserID() -> String? {
        currentUserID
    }

    /// 标记当前 token 冷却（ProxyController 调用）
    func markCurrentTokenCooldown(duration: TimeInterval = 30) {
        guard let token = getNextToken() else { return }
        markCooldown(userID: token.userID, duration: duration)
    }

    /// 标记当前 token 失败（ProxyController 调用）
    func markCurrentTokenFailed() {
        guard let token = getNextToken() else { return }
        markUnavailable(userID: token.userID)
    }

    // MARK: - Private Helpers

    private func updateActiveCount() {
        activeTokenCount = entries.filter { !$0.unavailable && !$0.isExpired }.count
    }

    // MARK: - Keychain Persistence

    private func keychainKey(for userID: String) -> String {
        "\(keychainService).token.\(userID)"
    }

    /// Saves a token to the macOS Keychain.
    private func saveToKeychain(_ token: TokenData) {
        guard let data = try? JSONEncoder().encode(token) else {
            os_log(.error, "Failed to encode token for Keychain")
            return
        }

        let key = keychainKey(for: token.userID)
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: keychainService,
            kSecAttrAccount as String: key,
        ]

        // Delete existing entry first (upsert pattern)
        SecItemDelete(query as CFDictionary)

        let attributes: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: keychainService,
            kSecAttrAccount as String: key,
            kSecValueData as String: data,
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlock,
        ]

        let status = SecItemAdd(attributes as CFDictionary, nil)
        if status != errSecSuccess {
            os_log(.error, "Failed to save token to Keychain: %d", status)
        }
    }

    /// Deletes a token from the macOS Keychain.
    private func deleteFromKeychain(userID: String) {
        let key = keychainKey(for: userID)
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: keychainService,
            kSecAttrAccount as String: key,
        ]

        let status = SecItemDelete(query as CFDictionary)
        if status != errSecSuccess && status != errSecItemNotFound {
            os_log(.error, "Failed to delete token from Keychain: %d", status)
        }
    }

    /// Loads all tokens from the macOS Keychain.
    private func loadFromKeychain() {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: keychainService,
            kSecMatchLimit as String: kSecMatchLimitAll,
            kSecReturnAttributes as String: true,
            kSecReturnData as String: true,
        ]

        var result: AnyObject?
        let status = SecItemCopyMatching(query as CFDictionary, &result)

        guard status == errSecSuccess, let items = result as? [[String: Any]] else {
            if status != errSecItemNotFound {
                os_log(.error, "Failed to query Keychain: %d", status)
            }
            return
        }

        for item in items {
            guard let data = item[kSecValueData as String] as? Data,
                  let account = item[kSecAttrAccount as String] as? String,
                  account.hasPrefix("\(keychainService).token.") else {
                continue
            }

            guard let token = try? JSONDecoder().decode(TokenData.self, from: data) else {
                os_log(.error, "Failed to decode token from Keychain for account: %{public}@", account)
                continue
            }

            // Skip expired tokens and clean them from Keychain
            if token.isExpired {
                deleteFromKeychain(userID: token.userID)
                continue
            }

            // Deduplicate (in case of duplicate Keychain entries)
            if !entries.contains(where: { $0.token.userID == token.userID }) {
                entries.append(TokenEntry(token: token, cooldownUntil: nil, unavailable: false))
            }
        }

        os_log(.info, "TokenManager loaded %d token(s) from Keychain", entries.count)
    }

    // MARK: - Legacy Migration

    /// Migrates a legacy ~/.codebuddy-proxy/token.json file to Keychain.
    /// This matches the Go proxy's single-token-file format for interoperability.
    private func migrateLegacyTokenFile() {
        let home = FileManager.default.homeDirectoryForCurrentUser
        let legacyPath = home.appendingPathComponent(".codebuddy-proxy/token.json")

        // Only migrate if we have no entries and the legacy file exists
        guard entries.isEmpty else { return }
        guard FileManager.default.fileExists(atPath: legacyPath.path) else { return }

        do {
            let data = try Data(contentsOf: legacyPath)
            let token = try JSONDecoder().decode(TokenData.self, from: data)

            if !token.isExpired {
                addToken(token)
                os_log(.info, "Migrated legacy token for %{public}@ to Keychain", token.userID)

                // Rename the legacy file to avoid re-migration
                let backupPath = home.appendingPathComponent(".codebuddy-proxy/token.json.migrated")
                try? FileManager.default.moveItem(at: legacyPath, to: backupPath)
            } else {
                // Delete expired legacy file
                try? FileManager.default.removeItem(at: legacyPath)
                os_log(.info, "Removed expired legacy token file")
            }
        } catch {
            os_log(.error, "Failed to migrate legacy token file: %{public}@", error.localizedDescription)
        }
    }
}
