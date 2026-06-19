namespace UniviewCodeBuddyProxy.Models;

/// <summary>
/// Convenience alias for Services.UpstreamIntent so Views/ViewModels can use
/// <c>UpstreamIntentDisplay.Craft</c> without importing the Services namespace.
/// The authoritative definition lives in Services/UpstreamClient.cs.
/// </summary>
public static class UpstreamIntentDisplay
{
    public const string Craft = "craft";
    public const string CodeCompletion = "CodeCompletion";
    public const string Embedding = "embedding";

    /// <summary>
    /// Converts a Services.UpstreamIntent to its X-Agent-Intent header string value.
    /// </summary>
    public static string ToHeaderString(Services.UpstreamIntent intent) => intent switch
    {
        Services.UpstreamIntent.Craft => Craft,
        Services.UpstreamIntent.CodeCompletion => CodeCompletion,
        Services.UpstreamIntent.Embedding => Embedding,
        _ => Craft
    };
}
