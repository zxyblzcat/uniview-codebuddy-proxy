using System;

namespace UniviewCodeBuddyProxy.Models;

/// <summary>
/// Model info for the models page.
/// </summary>
public sealed class ModelInfo
{
    public string Id { get; init; } = string.Empty;
    public string Name { get; init; } = string.Empty;
    public string OwnedBy { get; init; } = string.Empty;
    public string Provider => InferOwnedBy(Name);

    private static string InferOwnedBy(string model)
    {
        if (model.StartsWith("glm", StringComparison.OrdinalIgnoreCase)) return "Zhipu";
        if (model.StartsWith("minimax", StringComparison.OrdinalIgnoreCase)) return "MiniMax";
        if (model.StartsWith("kimi", StringComparison.OrdinalIgnoreCase)) return "Moonshot";
        if (model.StartsWith("deepseek", StringComparison.OrdinalIgnoreCase)) return "DeepSeek";
        if (model.StartsWith("hunyuan", StringComparison.OrdinalIgnoreCase)) return "Tencent";
        if (model.StartsWith("claude", StringComparison.OrdinalIgnoreCase)) return "Anthropic";
        if (model.StartsWith("gpt", StringComparison.OrdinalIgnoreCase)) return "OpenAI";
        if (model.StartsWith("gemini", StringComparison.OrdinalIgnoreCase)) return "Google";
        if (model.StartsWith("codebuddy", StringComparison.OrdinalIgnoreCase)) return "Tencent";
        return "Unknown";
    }
}
