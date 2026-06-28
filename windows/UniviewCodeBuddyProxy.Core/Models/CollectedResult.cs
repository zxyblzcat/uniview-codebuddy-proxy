namespace UniviewCodeBuddyProxy.Models;

/// <summary>
/// Result of collecting all upstream SSE chunks into a single response.
/// </summary>
public class CollectedResult
{
    public string Id { get; set; } = string.Empty;
    public string Model { get; set; } = string.Empty;
    public string Content { get; set; } = string.Empty;
    public string ReasoningContent { get; set; } = string.Empty;
    public List<object> ToolCalls { get; set; } = [];
    public string FinishReason { get; set; } = string.Empty;

    // Usage
    public int PromptTokens { get; set; }
    public int CompletionTokens { get; set; }
    public int ReasoningTokens { get; set; }
    public int CacheReadInputTokens { get; set; }
    public int CacheCreationInputTokens { get; set; }
}
