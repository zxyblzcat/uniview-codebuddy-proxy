import SwiftUI

// ═══════════════════════════════════════════════
// TokensView — Token 管理
// ═══════════════════════════════════════════════

struct TokensView: View {
    @EnvironmentObject var themeManager: ThemeManager
    @EnvironmentObject var tokenManager: TokenManager
    @EnvironmentObject var authService: AuthService

    // MARK: - State

    @State private var showAddTokenSheet = false
    @State private var newBearerToken = ""
    @State private var tokenToDelete: TokenInfo?
    @State private var showDeleteConfirm = false
    @State private var isRefreshing = false
    @State private var isStartingDeviceFlow = false
    @State private var authURL: String?
    @State private var showAuthURLCopied = false

    private var c: ThemeColors { themeManager.colors }

    // MARK: - Body

    var body: some View {
        VStack(spacing: 0) {
            // Header
            headerBar
                .padding(.horizontal, 24)
                .padding(.top, 20)
                .padding(.bottom, 12)

            // Token List
            if tokenManager.getAllTokens().isEmpty {
                emptyState
            } else {
                ScrollView {
                    VStack(spacing: 12) {
                        ForEach(tokenManager.getAllTokens()) { token in
                            tokenRow(token: token)
                        }
                    }
                    .padding(24)
                }
            }
        }
        .background(c.bg)
        .sheet(isPresented: $showAddTokenSheet) {
            addTokenSheet
        }
        .alert("删除令牌", isPresented: $showDeleteConfirm) {
            Button("取消", role: .cancel) {}
            Button("删除", role: .destructive) {
                if let token = tokenToDelete {
                    tokenManager.removeToken(userID: token.userID)
                }
            }
        } message: {
            if let token = tokenToDelete {
                Text("确定要删除令牌 \(maskEmail(token.userID)) 吗？此操作不可撤销。")
            }
        }
    }

    // MARK: - Header Bar

    private var headerBar: some View {
        HStack {
            Text("令牌")
                .font(.system(size: 20, weight: .bold))
                .foregroundColor(c.text)

            Spacer()

            // Login with Browser
            Button {
                startDeviceFlow()
            } label: {
                HStack(spacing: 6) {
                    if isStartingDeviceFlow {
                        ProgressView()
                            .controlSize(.small)
                    } else {
                        Image(systemName: "globe")
                            .font(.system(size: 12))
                    }
                    Text("浏览器登录")
                        .font(.system(size: 12, weight: .medium))
                }
                .foregroundColor(c.bg)
                .padding(.horizontal, 14)
                .padding(.vertical, 8)
                .background(c.primary)
                .clipShape(Capsule())
            }
            .buttonStyle(.plain)
            .disabled(isStartingDeviceFlow)

            // Add Token
            Button {
                showAddTokenSheet = true
            } label: {
                HStack(spacing: 6) {
                    Image(systemName: "plus")
                        .font(.system(size: 12))
                    Text("添加令牌")
                        .font(.system(size: 12, weight: .medium))
                }
                .foregroundColor(c.text)
                .padding(.horizontal, 14)
                .padding(.vertical, 8)
                .background(c.glassBg)
                .clipShape(Capsule())
                .overlay(
                    Capsule()
                        .stroke(c.glassBorder, lineWidth: 1)
                )
            }
            .buttonStyle(.plain)
        }
    }

    // MARK: - Empty State

    private var emptyState: some View {
        VStack(spacing: 16) {
            Spacer()

            Image(systemName: "key")
                .font(.system(size: 40))
                .foregroundColor(c.textMuted)

            Text("暂无令牌")
                .font(.system(size: 18, weight: .semibold))
                .foregroundColor(c.text)

            Text("手动添加令牌或通过浏览器登录开始使用。")
                .font(.system(size: 13))
                .foregroundColor(c.textSecondary)
                .multilineTextAlignment(.center)
                .frame(maxWidth: 280)

            HStack(spacing: 12) {
                Button {
                    startDeviceFlow()
                } label: {
                    Text("浏览器登录")
                        .font(.system(size: 13, weight: .medium))
                        .foregroundColor(c.bg)
                        .padding(.horizontal, 20)
                        .padding(.vertical, 10)
                        .background(c.primary)
                        .clipShape(Capsule())
                }
                .buttonStyle(.plain)

                Button {
                    showAddTokenSheet = true
                } label: {
                    Text("手动输入令牌")
                        .font(.system(size: 13, weight: .medium))
                        .foregroundColor(c.text)
                        .padding(.horizontal, 20)
                        .padding(.vertical, 10)
                        .background(c.glassBg)
                        .clipShape(Capsule())
                        .overlay(
                            Capsule()
                                .stroke(c.glassBorder, lineWidth: 1)
                        )
                }
                .buttonStyle(.plain)
            }

            Spacer()
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    // MARK: - Token Row

    private func tokenRow(token: TokenInfo) -> some View {
        HStack(spacing: 14) {
            // Status badge
            statusBadge(status: token.status)

            // Token info
            VStack(alignment: .leading, spacing: 4) {
                Text(maskEmail(token.userID))
                    .font(.system(size: 14, weight: .medium))
                    .foregroundColor(c.text)

                HStack(spacing: 16) {
                    Label {
                        Text(formatDate(token.createdAt))
                            .font(.system(size: 11))
                            .foregroundColor(c.textMuted)
                    } icon: {
                        Image(systemName: "calendar")
                            .font(.system(size: 10))
                            .foregroundColor(c.textMuted)
                    }

                    Label {
                        Text(formatDate(token.expiresAt))
                            .font(.system(size: 11))
                            .foregroundColor(
                                token.status == .expired ? ThemeColors.danger : c.textMuted
                            )
                    } icon: {
                        Image(systemName: "clock")
                            .font(.system(size: 10))
                            .foregroundColor(c.textMuted)
                    }
                }
            }

            Spacer()

            // Actions
            HStack(spacing: 8) {
                // Refresh
                Button {
                    refreshToken(token: token)
                } label: {
                    Image(systemName: "arrow.clockwise")
                        .font(.system(size: 12))
                        .foregroundColor(c.textSecondary)
                        .frame(width: 28, height: 28)
                        .background(c.glassBg)
                        .clipShape(Circle())
                }
                .buttonStyle(.plain)
                .help("刷新令牌")

                // Delete
                Button {
                    tokenToDelete = token
                    showDeleteConfirm = true
                } label: {
                    Image(systemName: "trash")
                        .font(.system(size: 12))
                        .foregroundColor(ThemeColors.danger)
                        .frame(width: 28, height: 28)
                        .background(ThemeColors.dangerSubtle)
                        .clipShape(Circle())
                }
                .buttonStyle(.plain)
                .help("删除令牌")
            }
        }
        .padding(14)
        .glassCardBackground(colors: c)
    }

    // MARK: - Status Badge

    private func statusBadge(status: TokenInfo.TokenStatus) -> some View {
        let (label, bgColor, fgColor): (String, Color, Color) = {
            switch status {
            case .active:    return ("活跃", ThemeColors.successSubtle, ThemeColors.success)
            case .cooldown:  return ("冷却", ThemeColors.warningSubtle, ThemeColors.warning)
            case .unavailable: return ("不可用", ThemeColors.dangerSubtle, ThemeColors.danger)
            case .expired:   return ("已过期", Color.gray.opacity(0.15), Color.gray)
            }
        }()

        return Text(label)
            .font(.system(size: 10, weight: .semibold))
            .foregroundColor(fgColor)
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(bgColor)
            .clipShape(Capsule())
    }

    // MARK: - Add Token Sheet

    private var addTokenSheet: some View {
        VStack(spacing: 20) {
            Text("添加令牌")
                .font(.system(size: 18, weight: .bold))
                .foregroundColor(c.text)

            VStack(alignment: .leading, spacing: 8) {
                Text("Bearer 令牌")
                    .font(.system(size: 12, weight: .medium))
                    .foregroundColor(c.textSecondary)

                SecureField("在此粘贴 Bearer 令牌", text: $newBearerToken)
                    .textFieldStyle(.plain)
                    .font(.system(size: 13, design: .monospaced))
                    .foregroundColor(c.text)
                    .padding(12)
                    .background(c.glassBg)
                    .clipShape(RoundedRectangle(cornerRadius: c.radiusSM))
                    .overlay(
                        RoundedRectangle(cornerRadius: c.radiusSM)
                            .stroke(c.glassBorder, lineWidth: 1)
                    )
            }

            HStack(spacing: 12) {
                Button("取消") {
                    showAddTokenSheet = false
                    newBearerToken = ""
                }
                .buttonStyle(.plain)
                .foregroundColor(c.textSecondary)
                .padding(.horizontal, 20)
                .padding(.vertical, 10)

                Button("添加") {
                    addManualToken()
                }
                .buttonStyle(.plain)
                .foregroundColor(c.bg)
                .padding(.horizontal, 20)
                .padding(.vertical, 10)
                .background(c.primary)
                .clipShape(Capsule())
                .disabled(newBearerToken.isEmpty)
            }
        }
        .padding(24)
        .frame(width: 400)
        .background(c.bg)
    }

    // MARK: - Actions

    private func startDeviceFlow() {
        isStartingDeviceFlow = true
        Task {
            do {
                let result = try await authService.startDeviceFlow()
                authService.openBrowser(url: result.authURL)
            } catch {
                // Silently handle — AuthService updates its own error state
            }
            isStartingDeviceFlow = false
        }
    }

    private func addManualToken() {
        guard !newBearerToken.isEmpty else { return }
        do {
            let tokenData = try authService.parseManualToken(newBearerToken)
            tokenManager.addToken(tokenData)
            showAddTokenSheet = false
            newBearerToken = ""
        } catch {
            // Handle parse error silently for now
        }
    }

    private func refreshToken(token: TokenInfo) {
        guard let tokenData = tokenManager.getTokenData(userID: token.userID) else { return }
        isRefreshing = true
        Task {
            do {
                let newToken = try await authService.refreshToken(refreshToken: tokenData.refreshToken)
                tokenManager.addToken(newToken)
            } catch {
                // Refresh failed — token will remain as-is
            }
            isRefreshing = false
        }
    }

    // MARK: - Helpers

    private func maskEmail(_ email: String) -> String {
        guard email.contains("@") else { return email }
        let parts = email.split(separator: "@", maxSplits: 1)
        let local = String(parts[0])
        let domain = parts.count > 1 ? String(parts[1]) : ""

        if local.count <= 2 {
            return "\(local)***@\(domain)"
        }
        let visible = String(local.prefix(2))
        return "\(visible)***@\(domain)"
    }

    private func formatDate(_ timestamp: Int64) -> String {
        guard timestamp > 0 else { return "--" }
        let date = Date(timeIntervalSince1970: TimeInterval(timestamp))
        let formatter = DateFormatter()
        formatter.dateStyle = .short
        formatter.timeStyle = .short
        return formatter.string(from: date)
    }
}
