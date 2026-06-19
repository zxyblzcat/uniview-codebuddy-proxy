namespace UniviewCodeBuddyProxy.Models;

/// <summary>
/// UI display model for auth poll results, wrapping Services.PollResult
/// with convenience factory methods.
/// The authoritative PollResult lives in Services.AuthService.cs.
/// </summary>
public class PollResultDisplay
{
    public Services.PollResultType Type { get; init; }
    public TokenDataDisplay? Token { get; init; }
    public string? ErrorMessage { get; init; }

    /// <summary>
    /// Creates a display model from a Services.PollResult.
    /// </summary>
    public static PollResultDisplay FromServiceResult(Services.PollResult result) => new()
    {
        Type = result.Type,
        Token = result.Token != null ? TokenDataDisplay.FromServiceToken(result.Token) : null,
        ErrorMessage = result.ErrorMessage,
    };
}
