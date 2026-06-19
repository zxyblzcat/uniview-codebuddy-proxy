import Foundation
import os
import Combine
import AppKit

// MARK: - TokenData

/// Represents cached token data from the CodeBuddy OAuth2 Device Flow.
struct TokenData: Codable, Identifiable {
    var id: String { userID }

    var bearerToken: String
    var accessToken: String
    var refreshToken: String
    var tokenType: String
    var expiresIn: Int
    var domain: String
    var sessionState: String
    var createdAt: Int64
    var expiresAt: Int64
    var userID: String

    /// Whether the token is expired (with 5-second clock-drift tolerance).
    var isExpired: Bool {
        expiresAt > 0 && Int64(Date().timeIntervalSince1970) > expiresAt + 5
    }

    /// The effective bearer string (prefers bearerToken over accessToken).
    var bearer: String {
        bearerToken.isEmpty ? accessToken : bearerToken
    }

    enum CodingKeys: String, CodingKey {
        case bearerToken = "bearer_token"
        case accessToken = "access_token"
        case refreshToken = "refresh_token"
        case tokenType = "token_type"
        case expiresIn = "expires_in"
        case domain
        case sessionState = "session_state"
        case createdAt = "created_at"
        case expiresAt = "expires_at"
        case userID = "user_id"
    }
}

// MARK: - PollResult

/// Result of a single poll attempt during the Device Flow.
/// Uses enum with associated values to match the AuthController switch pattern.
enum PollResult {
    /// User has not yet completed login in the browser.
    case waiting
    /// Login succeeded; contains the full token data.
    case success(TokenData)
    /// Login failed; contains an error message.
    case failed(String)
}

// MARK: - AuthService

/// OAuth2 Device Flow authentication service for CodeBuddy.
@MainActor
final class AuthService: ObservableObject {

    // MARK: Published State

    @Published var isAuthenticated: Bool = false
    @Published var currentUserID: String = ""
    @Published var isAuthenticating: Bool = false
    @Published var authError: String?

    // MARK: Constants

    private let baseURL = "https://unvcoding.copilot.qq.com"
    private let domain = "unvcoding.copilot.qq.com"
    private let codebuddyVersion = "2.92.0"

    private let authStateURL: String
    private let authTokenURL: String
    private let tokenRefreshURL: String

    private let session: URLSession

    // MARK: Init

    init() {
        authStateURL = baseURL + "/v2/plugin/auth/state"
        authTokenURL = baseURL + "/v2/plugin/auth/token"
        tokenRefreshURL = baseURL + "/v2/plugin/auth/token/refresh"

        let config = URLSessionConfiguration.default
        config.timeoutIntervalForRequest = 30
        config.timeoutIntervalForResource = 60
        session = URLSession(configuration: config)
    }

    // MARK: - Device Flow

    /// Starts the Device Flow by requesting an auth URL and state from upstream.
    /// POST /v2/plugin/auth/state?platform=CLI&nonce=<16-hex>
    /// - Returns: A tuple of (authURL, authState) for the caller to open in a browser and poll.
    func startDeviceFlow() async throws -> (authURL: String, authState: String) {
        isAuthenticating = true
        authError = nil
        defer { isAuthenticating = false }

        let nonce = generateNonce()
        let urlString = "\(authStateURL)?platform=CLI&nonce=\(nonce)"
        guard let url = URL(string: urlString) else {
            throw AuthError.invalidURL
        }

        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        for (key, value) in authStartHeaders() {
            request.setValue(value, forHTTPHeaderField: key)
        }

        let body = ["nonce": nonce]
        request.httpBody = try JSONEncoder().encode(body)

        let (data, response) = try await session.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse else {
            throw AuthError.invalidResponse
        }

        guard let json = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            throw AuthError.invalidResponse
        }

        guard httpResponse.statusCode == 200,
              let code = json["code"] as? Int,
              code == 0,
              let d = json["data"] as? [String: Any],
              let state = d["state"] as? String,
              let authURL = d["authUrl"] as? String,
              !state.isEmpty,
              !authURL.isEmpty else {
            let detail = String(data: data, encoding: .utf8) ?? "unknown"
            throw AuthError.startFailed(String(detail.prefix(200)))
        }

        return (authURL: authURL, authState: state)
    }

    /// Polls the upstream token endpoint until the user completes login.
    /// GET /v2/plugin/auth/token?state=<authState>
    /// - Parameter authState: The state string returned by `startDeviceFlow()`.
    /// - Returns: A `PollResult` enum: `.waiting`, `.success(TokenData)`, or `.failed(String)`.
    func pollToken(authState: String) async -> PollResult {
        let urlString = "\(authTokenURL)?state=\(authState)"
        guard let url = URL(string: urlString) else {
            return .failed("Invalid poll URL")
        }

        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        for (key, value) in authPollHeaders() {
            request.setValue(value, forHTTPHeaderField: key)
        }

        do {
            let (data, _) = try await session.data(for: request)

            guard let json = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
                return .failed("Invalid response")
            }

            let code = json["code"] as? Int ?? 0

            // 11217 = waiting for user login
            if code == 11217 {
                return .waiting
            }

            if code == 0, let d = json["data"] as? [String: Any],
               let accessToken = d["accessToken"] as? String, !accessToken.isEmpty {

                var expiresIn = d["expiresIn"] as? Int ?? 0
                if expiresIn == 0 { expiresIn = 3600 }

                let now = Int64(Date().timeIntervalSince1970)
                var userID = extractUserIDFromJWT(accessToken)
                if userID.isEmpty {
                    userID = d["domain"] as? String ?? ""
                }

                let tokenData = TokenData(
                    bearerToken: accessToken,
                    accessToken: accessToken,
                    refreshToken: d["refreshToken"] as? String ?? "",
                    tokenType: d["tokenType"] as? String ?? "",
                    expiresIn: expiresIn,
                    domain: d["domain"] as? String ?? "",
                    sessionState: d["sessionState"] as? String ?? "",
                    createdAt: now,
                    expiresAt: now + Int64(expiresIn),
                    userID: userID
                )

                return .success(tokenData)
            }

            return .failed("auth_poll_failed")

        } catch {
            return .failed(error.localizedDescription)
        }
    }

    // MARK: - Manual Token Entry

    /// Parses a manually-provided bearer token into a `TokenData` struct.
    /// Extracts userID and expiry from the JWT payload.
    /// - Parameter bearerToken: The raw bearer/access token string (JWT).
    /// - Returns: A `TokenData` with extracted claims.
    func parseManualToken(_ bearerToken: String) throws -> TokenData {
        let now = Int64(Date().timeIntervalSince1970)
        let userID = extractUserIDFromJWT(bearerToken)

        var expiresAt = now + 86400 // default 24h
        let jwtExp = extractJWTExp(bearerToken)
        if jwtExp > 0 {
            expiresAt = jwtExp
        }

        return TokenData(
            bearerToken: bearerToken,
            accessToken: bearerToken,
            refreshToken: "",
            tokenType: "",
            expiresIn: Int(expiresAt - now),
            domain: "",
            sessionState: "",
            createdAt: now,
            expiresAt: expiresAt,
            userID: userID
        )
    }

    // MARK: - Token Refresh

    /// Refreshes an access token using a refresh token.
    /// POST /v2/plugin/auth/token/refresh
    /// - Parameter refreshToken: The refresh token string.
    /// - Returns: New `TokenData` with refreshed credentials.
    func refreshToken(refreshToken: String) async throws -> TokenData {
        guard !refreshToken.isEmpty else {
            throw AuthError.noRefreshToken
        }

        guard let url = URL(string: tokenRefreshURL) else {
            throw AuthError.invalidURL
        }

        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("Bearer \(refreshToken)", forHTTPHeaderField: "Authorization")
        request.setValue(refreshToken, forHTTPHeaderField: "X-Refresh-Token")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")

        let (data, response) = try await session.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse else {
            throw AuthError.invalidResponse
        }

        if httpResponse.statusCode != 200 {
            let body = String(data: data, encoding: .utf8) ?? ""
            throw AuthError.refreshFailed("HTTP \(httpResponse.statusCode): \(body.prefix(300))")
        }

        guard let json = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            throw AuthError.invalidResponse
        }

        let code = json["code"] as? Int ?? -1
        if code != 0 {
            throw AuthError.refreshFailed("Upstream code: \(code)")
        }

        guard let d = json["data"] as? [String: Any],
              let accessToken = d["accessToken"] as? String,
              !accessToken.isEmpty else {
            throw AuthError.refreshFailed("Missing accessToken in response")
        }

        var expiresIn = d["expiresIn"] as? Int ?? 0
        if expiresIn == 0 { expiresIn = 3600 }

        let now = Int64(Date().timeIntervalSince1970)
        var userID = extractUserIDFromJWT(accessToken)
        if userID.isEmpty {
            userID = d["domain"] as? String ?? ""
        }

        var newRefreshToken = d["refreshToken"] as? String ?? ""
        if newRefreshToken.isEmpty {
            newRefreshToken = refreshToken
        }

        let newToken = TokenData(
            bearerToken: accessToken,
            accessToken: accessToken,
            refreshToken: newRefreshToken,
            tokenType: d["tokenType"] as? String ?? "",
            expiresIn: expiresIn,
            domain: d["domain"] as? String ?? "",
            sessionState: d["sessionState"] as? String ?? "",
            createdAt: now,
            expiresAt: now + Int64(expiresIn),
            userID: userID
        )

        return newToken
    }

    // MARK: - Auto-Relogin

    /// Attempts auto-relogin: first tries refresh, then falls back to Device Flow.
    /// - Parameters:
    ///   - token: The expired token to attempt refresh with.
    ///   - onAuthURL: Called with the auth URL when Device Flow fallback opens a browser.
    /// - Returns: New token data if relogin succeeds, nil otherwise.
    func autoRelogin(expiredToken token: TokenData?, onAuthURL: ((String) -> Void)? = nil) async -> TokenData? {
        // Step 1: Try refresh
        if let token = token, !token.refreshToken.isEmpty {
            do {
                let newToken = try await refreshToken(refreshToken: token.refreshToken)
                isAuthenticated = true
                currentUserID = newToken.userID
                return newToken
            } catch {
                os_log(.error, "Token refresh failed, falling back to Device Flow: %{public}@", error.localizedDescription)
            }
        }

        // Step 2: Device Flow fallback
        do {
            let (authURL, authState) = try await startDeviceFlow()
            openBrowser(url: authURL)
            onAuthURL?(authURL)

            // Poll for up to 60 iterations, 3 seconds apart (3 minutes total)
            for _ in 0..<60 {
                let result = await pollToken(authState: authState)
                switch result {
                case .success(let tokenData):
                    isAuthenticated = true
                    currentUserID = tokenData.userID
                    return tokenData
                case .failed(let message):
                    os_log(.error, "Auto-relogin poll error: %{public}@", message)
                    return nil
                case .waiting:
                    try? await Task.sleep(nanoseconds: 3_000_000_000)
                }
            }
            os_log(.error, "Auto-relogin timed out after 60 poll iterations")
        } catch {
            os_log(.error, "Auto-relogin failed (startDeviceFlow): %{public}@", error.localizedDescription)
        }

        return nil
    }

    // MARK: - Browser

    /// Opens a URL in the default macOS browser.
    func openBrowser(url: String) {
        guard let url = URL(string: url) else { return }
        NSWorkspace.shared.open(url)
    }

    // MARK: - JWT Helpers

    /// Extracts user ID from a JWT by decoding the payload and checking
    /// email, preferred_username, and sub claims in priority order.
    func extractUserIDFromJWT(_ token: String) -> String {
        let parts = splitJWT(token)
        guard parts.count >= 2 else { return "" }

        guard let payloadData = base64urlDecode(parts[1]) else { return "" }

        guard let claims = try? JSONSerialization.jsonObject(with: payloadData) as? [String: Any] else {
            return ""
        }

        for key in ["email", "preferred_username", "sub"] {
            if let value = claims[key] as? String, !value.isEmpty {
                return value
            }
        }
        return ""
    }

    /// Extracts the `exp` claim from a JWT.
    func extractJWTExp(_ token: String) -> Int64 {
        let parts = splitJWT(token)
        guard parts.count >= 2 else { return 0 }

        guard let payloadData = base64urlDecode(parts[1]) else { return 0 }

        guard let claims = try? JSONSerialization.jsonObject(with: payloadData) as? [String: Any] else {
            return 0
        }

        if let exp = claims["exp"] as? Double {
            return Int64(exp)
        }
        return 0
    }

    // MARK: - Header Builders

    /// Headers for the auth/state (start) request.
    /// Matches Go authStartHeaders() exactly.
    private func authStartHeaders() -> [String: String] {
        [
            "Host": domain,
            "Accept": "application/json, text/plain, */*",
            "Content-Type": "application/json",
            "Cache-Control": "no-cache",
            "Pragma": "no-cache",
            "Connection": "close",
            "X-Requested-With": "XMLHttpRequest",
            "X-Domain": domain,
            "X-No-Authorization": "true",
            "X-No-User-Id": "true",
            "X-No-Enterprise-Id": "true",
            "X-No-Department-Info": "true",
            "User-Agent": "CLI/\(codebuddyVersion) CodeBuddy/\(codebuddyVersion)",
            "X-Product": "SaaS",
            "X-Request-ID": generateRequestID(),
        ]
    }

    /// Headers for the auth/token (poll) request -- includes B3 tracing.
    /// Matches Go authPollHeaders() exactly.
    private func authPollHeaders() -> [String: String] {
        let rid = generateRequestID()
        let span = generateSpanID()
        return [
            "Host": domain,
            "Accept": "application/json, text/plain, */*",
            "Cache-Control": "no-cache",
            "Pragma": "no-cache",
            "Connection": "close",
            "X-Requested-With": "XMLHttpRequest",
            "X-Request-ID": rid,
            "b3": "\(rid)-\(span)-1-",
            "X-B3-TraceId": rid,
            "X-B3-ParentSpanId": "",
            "X-B3-SpanId": span,
            "X-B3-Sampled": "1",
            "X-No-Authorization": "true",
            "X-No-User-Id": "true",
            "X-No-Enterprise-Id": "true",
            "X-No-Department-Info": "true",
            "X-Domain": domain,
            "User-Agent": "CLI/\(codebuddyVersion) CodeBuddy/\(codebuddyVersion)",
            "X-Product": "SaaS",
        ]
    }

    // MARK: - ID Generation

    /// Generates a 16-character hex nonce (8 random bytes), matching Go generateNonce().
    private func generateNonce() -> String {
        var bytes = [UInt8](repeating: 0, count: 8)
        _ = SecRandomCopyBytes(kSecRandomDefault, 8, &bytes)
        return bytes.map { String(format: "%02x", $0) }.joined()
    }

    /// Generates a 32-character hex request ID (16 random bytes), matching Go generateRequestID().
    private func generateRequestID() -> String {
        var bytes = [UInt8](repeating: 0, count: 16)
        _ = SecRandomCopyBytes(kSecRandomDefault, 16, &bytes)
        return bytes.map { String(format: "%02x", $0) }.joined()
    }

    /// Generates a 16-character hex span ID (8 random bytes), matching Go generateSpanID().
    private func generateSpanID() -> String {
        var bytes = [UInt8](repeating: 0, count: 8)
        _ = SecRandomCopyBytes(kSecRandomDefault, 8, &bytes)
        return bytes.map { String(format: "%02x", $0) }.joined()
    }

    // MARK: - JWT Split / Base64url

    /// Splits a JWT on the first two dots only (avoids issues with dots in the signature).
    /// Matches Go splitJWT() behavior.
    private func splitJWT(_ token: String) -> [String] {
        var result: [String] = []
        var start = token.startIndex
        for i in token.indices {
            if token[i] == "." && result.count < 2 {
                result.append(String(token[start..<i]))
                start = token.index(after: i)
            }
        }
        result.append(String(token[start...]))
        return result
    }

    /// Base64url-decodes a string, adding padding as needed.
    /// Matches Go base64urlDecode() behavior.
    private func base64urlDecode(_ s: String) -> Data? {
        var s = s
            .replacingOccurrences(of: "-", with: "+")
            .replacingOccurrences(of: "_", with: "/")

        let remainder = s.count % 4
        if remainder == 2 {
            s += "=="
        } else if remainder == 3 {
            s += "="
        }

        return Data(base64Encoded: s)
    }
}

// MARK: - AuthError

enum AuthError: LocalizedError {
    case invalidURL
    case invalidResponse
    case startFailed(String)
    case noRefreshToken
    case refreshFailed(String)

    var errorDescription: String? {
        switch self {
        case .invalidURL: return "无效的 URL"
        case .invalidResponse: return "服务器响应无效"
        case .startFailed(let detail): return "认证启动失败：\(detail)"
        case .noRefreshToken: return "无可用刷新令牌"
        case .refreshFailed(let detail): return "令牌刷新失败：\(detail)"
        }
    }
}
