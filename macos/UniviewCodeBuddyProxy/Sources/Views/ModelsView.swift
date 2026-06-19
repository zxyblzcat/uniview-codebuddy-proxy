import SwiftUI

// ═══════════════════════════════════════════════
// ModelsView — 模型列表
// ═══════════════════════════════════════════════

struct ModelsView: View {
    @EnvironmentObject var themeManager: ThemeManager
    @EnvironmentObject var configManager: ConfigManager

    // MARK: - State

    @State private var searchText = ""
    @State private var selectedProvider = "全部"
    @State private var selectedModel: (name: String, ownedBy: String)?
    @State private var showDetail = false

    private var c: ThemeColors { themeManager.colors }

    private let providers = ["全部", "GLM", "DeepSeek", "MiniMax", "Kimi", "Hunyuan"]

    // MARK: - Filtered Models

    private var filteredModels: [(name: String, ownedBy: String)] {
        var models = extraModels

        if selectedProvider != "全部" {
            let prefix: String
            switch selectedProvider {
            case "GLM": prefix = "glm"
            case "DeepSeek": prefix = "deepseek"
            case "MiniMax": prefix = "minimax"
            case "Kimi": prefix = "kimi"
            case "Hunyuan": prefix = "hunyuan"
            default: prefix = ""
            }
            models = models.filter { $0.name.hasPrefix(prefix) }
        }

        if !searchText.isEmpty {
            models = models.filter {
                $0.name.localizedCaseInsensitiveContains(searchText) ||
                $0.ownedBy.localizedCaseInsensitiveContains(searchText)
            }
        }

        return models
    }

    // MARK: - Body

    var body: some View {
        VStack(spacing: 0) {
            // Search & Filter Bar
            searchBarSection
                .padding(.horizontal, 24)
                .padding(.top, 20)

            // Model Grid
            ScrollView {
                LazyVGrid(
                    columns: [
                        GridItem(.flexible(), spacing: 16),
                        GridItem(.flexible(), spacing: 16),
                        GridItem(.flexible(), spacing: 16),
                    ],
                    spacing: 16
                ) {
                    ForEach(Array(filteredModels.enumerated()), id: \.offset) { _, model in
                        modelCard(model: model)
                    }
                }
                .padding(24)
            }
        }
        .background(c.seed.bg)
        .popover(isPresented: $showDetail) {
            if let model = selectedModel {
                modelDetailPopover(model: model)
            }
        }
    }

    // MARK: - Search Bar Section

    private var searchBarSection: some View {
        VStack(spacing: 12) {
            // Search field
            HStack(spacing: 10) {
                Image(systemName: "magnifyingglass")
                    .foregroundColor(c.textMuted)
                    .font(.system(size: 14))

                TextField("搜索模型...", text: $searchText)
                    .textFieldStyle(.plain)
                    .font(.system(size: 13))
                    .foregroundColor(c.text)

                if !searchText.isEmpty {
                    Button {
                        searchText = ""
                    } label: {
                        Image(systemName: "xmark.circle.fill")
                            .foregroundColor(c.textMuted)
                            .font(.system(size: 12))
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
            .background(c.glassBg)
            .clipShape(RoundedRectangle(cornerRadius: c.radiusSM))
            .overlay(
                RoundedRectangle(cornerRadius: c.radiusSM)
                    .stroke(c.glassBorder, lineWidth: 1)
            )

            // Provider filter chips
            HStack(spacing: 8) {
                ForEach(providers, id: \.self) { provider in
                    Button {
                        withAnimation(ThemeColors.easeHarmony) {
                            selectedProvider = provider
                        }
                    } label: {
                        Text(provider)
                            .font(.system(size: 12, weight: selectedProvider == provider ? .semibold : .regular))
                            .foregroundColor(selectedProvider == provider ? c.seed.bg : c.textSecondary)
                            .padding(.horizontal, 14)
                            .padding(.vertical, 6)
                            .background(
                                selectedProvider == provider
                                    ? c.primary
                                    : c.glassBg
                            )
                            .clipShape(Capsule())
                            .overlay(
                                Capsule()
                                    .stroke(
                                        selectedProvider == provider ? Color.clear : c.glassBorder,
                                        lineWidth: 1
                                    )
                            )
                    }
                    .buttonStyle(.plain)
                }
                Spacer()
            }
        }
        .padding(.bottom, 4)
    }

    // MARK: - Model Card

    private func modelCard(model: (name: String, ownedBy: String)) -> some View {
        Button {
            selectedModel = model
            showDetail = true
        } label: {
            VStack(alignment: .leading, spacing: 10) {
                // Header row
                HStack {
                    Circle()
                        .fill(providerColor(model.ownedBy).opacity(0.2))
                        .frame(width: 32, height: 32)
                        .overlay(
                            Text(String(model.ownedBy.prefix(1)))
                                .font(.system(size: 14, weight: .bold))
                                .foregroundColor(providerColor(model.ownedBy))
                        )

                    Spacer()

                    // Status dot
                    Circle()
                        .fill(ThemeColors.success)
                        .frame(width: 8, height: 8)
                }

                // Model name
                Text(model.name)
                    .font(.system(size: 14, weight: .semibold))
                    .foregroundColor(c.text)
                    .lineLimit(1)

                // Provider
                Text(model.ownedBy)
                    .font(.system(size: 11))
                    .foregroundColor(c.textSecondary)

                // Context window (mock)
                HStack(spacing: 4) {
                    Image(systemName: "text.bubble")
                        .font(.system(size: 10))
                        .foregroundColor(c.textMuted)
                    Text(contextWindowHint(model.name))
                        .font(.system(size: 10))
                        .foregroundColor(c.textMuted)
                }
            }
            .padding(14)
            .frame(maxWidth: .infinity, alignment: .leading)
            .glassCardBackground(colors: c)
        }
        .buttonStyle(.plain)
    }

    // MARK: - Model Detail Popover

    private func modelDetailPopover(model: (name: String, ownedBy: String)) -> some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack {
                Circle()
                    .fill(providerColor(model.ownedBy).opacity(0.2))
                    .frame(width: 40, height: 40)
                    .overlay(
                        Text(String(model.ownedBy.prefix(1)))
                            .font(.system(size: 18, weight: .bold))
                            .foregroundColor(providerColor(model.ownedBy))
                    )

                VStack(alignment: .leading, spacing: 2) {
                    Text(model.name)
                        .font(.system(size: 16, weight: .bold))
                        .foregroundColor(c.text)
                    Text(model.ownedBy)
                        .font(.system(size: 12))
                        .foregroundColor(c.textSecondary)
                }
                Spacer()
            }

            Divider()
                .overlay(c.glassBorder)

            detailRow(label: "提供商", value: model.ownedBy)
            detailRow(label: "上下文窗口", value: contextWindowHint(model.name))
            detailRow(label: "状态", value: "可用")
            detailRow(label: "流式输出", value: "支持")

            if model.name.hasPrefix("glm-4.6") || model.name.hasPrefix("glm-5") {
                detailRow(label: "视觉", value: "支持")
            }
        }
        .padding(20)
        .frame(width: 300)
        .background(c.seed.bg)
    }

    private func detailRow(label: String, value: String) -> some View {
        HStack {
            Text(label)
                .font(.system(size: 12))
                .foregroundColor(c.textMuted)
            Spacer()
            Text(value)
                .font(.system(size: 12, weight: .medium))
                .foregroundColor(c.text)
        }
    }

    // MARK: - Helpers

    private func providerColor(_ provider: String) -> Color {
        switch provider {
        case "Zhipu": return c.primary
        case "DeepSeek": return ThemeColors.purple
        case "MiniMax": return c.accent
        case "Moonshot": return ThemeColors.warning
        case "Tencent": return Color(hex: "F97316")
        default: return c.primary
        }
    }

    private func contextWindowHint(_ model: String) -> String {
        if model.hasPrefix("glm-5") { return "128K" }
        if model.hasPrefix("glm-4.7") { return "128K" }
        if model.hasPrefix("glm-4.6") { return "128K" }
        if model.hasPrefix("minimax") { return "1M" }
        if model.hasPrefix("kimi") { return "128K" }
        if model.hasPrefix("deepseek-r1") { return "64K" }
        if model.hasPrefix("deepseek-v3") { return "128K" }
        if model.hasPrefix("hunyuan") { return "32K" }
        return "128K"
    }
}
