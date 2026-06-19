using System;
using System.Collections.Generic;
using System.ComponentModel;
using System.IO;
using System.Text;
using System.Text.Json;

namespace UniviewCodeBuddyProxy.Services;

/// <summary>Token data from the CodeBuddy OAuth2 Device Flow.</summary>
public sealed class TokenData
{
    public string BearerToken { get; set; } = "";
    public string AccessToken { get; set; } = "";
    public string RefreshToken { get; set; } = "";
    public string TokenType { get; set; } = "";
    public int ExpiresIn { get; set; }
    public string Domain { get; set; } = "";
    public string SessionState { get; set; } = "";
    public long CreatedAt { get; set; }
    public long ExpiresAt { get; set; }
    public string UserID { get; set; } = "";

    /// <summary>Whether the token is expired (with 5-second clock-drift tolerance).</summary>
    public bool IsExpired => ExpiresAt > 0 && DateTimeOffset.UtcNow.ToUnixTimeSeconds() > ExpiresAt + 5;

    /// <summary>The effective bearer string (prefers BearerToken over AccessToken).</summary>
    public string Bearer => string.IsNullOrEmpty(BearerToken) ? AccessToken : BearerToken;
}

/// <summary>Token status for API display.</summary>
public enum TokenStatus { Active, Cooldown, Unavailable, Expired }

/// <summary>Token info for API display.</summary>
public sealed class TokenInfo
{
    public string Id { get; } // userID
    public string UserID { get; }
    public TokenStatus Status { get; }
    public long CreatedAt { get; }
    public long ExpiresAt { get; }

    public TokenInfo(string userID, TokenStatus status, long createdAt, long expiresAt)
    {
        Id = userID;
        UserID = userID;
        Status = status;
        CreatedAt = createdAt;
        ExpiresAt = expiresAt;
    }
}

/// <summary>Internal entry in the token pool with health state.</summary>
internal sealed class TokenEntry
{
    public TokenData Token { get; set; }
    public DateTime? CooldownUntil { get; set; }
    public bool Unavailable { get; set; }

    public TokenEntry(TokenData token)
    {
        Token = token;
    }

    public bool IsInCooldown => CooldownUntil.HasValue && DateTime.UtcNow < CooldownUntil.Value;
    public bool IsExpired => Token.IsExpired;
}

/// <summary>
/// Token pool manager with round-robin selection, health awareness,
/// and JSON file persistence (cross-platform compatible).
/// </summary>
public sealed class TokenManager : INotifyPropertyChanged, IDisposable
{
    // ═══ Constants ═══

    private static readonly string TokenDir = Path.Combine(
        Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData),
        "UniviewCodeBuddyProxy", "tokens");
    private static readonly JsonSerializerOptions JsonOpts = new()
    {
        PropertyNamingPolicy = JsonNamingPolicy.SnakeCaseLower,
        WriteIndented = false,
    };
    private static readonly JsonSerializerOptions PrettyOpts = new()
    {
        PropertyNamingPolicy = JsonNamingPolicy.SnakeCaseLower,
        WriteIndented = true,
    };

    // ═══ State ═══

    private readonly List<TokenEntry> _entries = new();
    private int _roundRobinIndex;
    private readonly object _lock = new();

    // ═══ INotifyPropertyChanged ═══

    public event PropertyChangedEventHandler? PropertyChanged;
    private void OnPropertyChanged([System.Runtime.CompilerServices.CallerMemberName] string? name = null)
        => PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(name));

    public int ActiveTokenCount
    {
        get
        {
            lock (_lock)
            {
                return _entries.Count(e => !e.Unavailable && !e.IsExpired);
            }
        }
    }

    // ═══ Constructor ═══

    public TokenManager()
    {
        LoadFromFiles();
        MigrateLegacyTokenFile();
    }

    // ═══ Token Selection ═══

    /// <summary>
    /// Returns the next available token using round-robin with health awareness.
    /// </summary>
    public TokenData? NextToken()
    {
        lock (_lock)
        {
            var n = _entries.Count;
            if (n == 0) return null;

            // Try from current index, skipping unhealthy entries
            for (var i = 0; i < n; i++)
            {
                var idx = (_roundRobinIndex + i) % n;
                var entry = _entries[idx];

                if (entry.Unavailable) continue;
                if (entry.IsInCooldown) continue;
                if (entry.IsExpired) continue;
                if (string.IsNullOrEmpty(entry.Token.Bearer)) continue;

                _roundRobinIndex = (idx + 1) % n;
                return entry.Token;
            }

            // All tokens unavailable or in cooldown; find earliest cooldown expiry
            TokenEntry? bestEntry = null;
            foreach (var entry in _entries)
            {
                if (entry.Unavailable) continue;
                var entryTime = entry.CooldownUntil ?? DateTime.MinValue;
                var bestTime = bestEntry?.CooldownUntil ?? DateTime.MinValue;
                if (bestEntry == null || entryTime < bestTime)
                    bestEntry = entry;
            }

            if (bestEntry != null && !string.IsNullOrEmpty(bestEntry.Token.Bearer))
                return bestEntry.Token;

            return null;
        }
    }

    // ═══ Health Management ═══

    /// <summary>Mark a token as in cooldown after a 429 response.</summary>
    public void MarkCooldown(string userID, double durationSecs = 30)
    {
        var effectiveDuration = durationSecs > 0 ? durationSecs : 30;
        lock (_lock)
        {
            foreach (var entry in _entries)
            {
                if (entry.Token.UserID == userID)
                {
                    entry.CooldownUntil = DateTime.UtcNow.AddSeconds(effectiveDuration);
                    break;
                }
            }
        }
        OnPropertyChanged(nameof(ActiveTokenCount));
    }

    /// <summary>Mark a token as permanently unavailable after a 401 response.</summary>
    public void MarkUnavailable(string userID)
    {
        lock (_lock)
        {
            foreach (var entry in _entries)
            {
                if (entry.Token.UserID == userID)
                {
                    entry.Unavailable = true;
                    break;
                }
            }
        }
        OnPropertyChanged(nameof(ActiveTokenCount));
    }

    // ═══ Token Management ═══

    /// <summary>Add a token to the pool. Deduplicates by userID.</summary>
    public void AddToken(TokenData token)
    {
        lock (_lock)
        {
            for (var i = 0; i < _entries.Count; i++)
            {
                if (_entries[i].Token.UserID == token.UserID)
                {
                    _entries[i].Token = token;
                    _entries[i].Unavailable = false;
                    _entries[i].CooldownUntil = null;
                    SaveTokenToFile(token);
                    OnPropertyChanged(nameof(ActiveTokenCount));
                    return;
                }
            }

            _entries.Add(new TokenEntry(token));
            SaveTokenToFile(token);
        }
        OnPropertyChanged(nameof(ActiveTokenCount));
    }

    /// <summary>Remove a token from the pool by userID.</summary>
    public void RemoveToken(string userID)
    {
        lock (_lock)
        {
            _entries.RemoveAll(e => e.Token.UserID == userID);
            DeleteTokenFile(userID);
        }
        OnPropertyChanged(nameof(ActiveTokenCount));
    }

    /// <summary>Return the raw TokenData for a given userID.</summary>
    public TokenData? GetTokenData(string userID)
    {
        lock (_lock)
        {
            foreach (var entry in _entries)
            {
                if (entry.Token.UserID == userID)
                    return entry.Token;
            }
        }
        return null;
    }

    /// <summary>Return token info for all entries.</summary>
    public List<TokenInfo> GetAllTokens()
    {
        lock (_lock)
        {
            var result = new List<TokenInfo>();
            foreach (var entry in _entries)
            {
                var status = entry.Unavailable ? TokenStatus.Unavailable
                    : entry.IsExpired ? TokenStatus.Expired
                    : entry.IsInCooldown ? TokenStatus.Cooldown
                    : TokenStatus.Active;

                result.Add(new TokenInfo(entry.Token.UserID, status, entry.Token.CreatedAt, entry.Token.ExpiresAt));
            }
            return result;
        }
    }

    // ═══ Convenience Properties ═══

    /// <summary>Current bearer token (for telemetry etc).</summary>
    public string? CurrentBearerToken
    {
        get
        {
            lock (_lock)
            {
                foreach (var entry in _entries)
                {
                    if (!entry.Unavailable && !entry.IsExpired && !entry.IsInCooldown)
                        return entry.Token.Bearer;
                }
            }
            return null;
        }
    }

    /// <summary>Current user ID.</summary>
    public string? CurrentUserID
    {
        get
        {
            lock (_lock)
            {
                foreach (var entry in _entries)
                {
                    if (!entry.Unavailable && !entry.IsExpired && !entry.IsInCooldown)
                        return entry.Token.UserID;
                }
            }
            return null;
        }
    }

    // ═══ File-based Persistence ═══

    private void SaveTokenToFile(TokenData token)
    {
        try
        {
            Directory.CreateDirectory(TokenDir);
            var path = Path.Combine(TokenDir, $"token_{SanitizeFilename(token.UserID)}.json");
            var json = JsonSerializer.Serialize(token, PrettyOpts);
            File.WriteAllText(path, json);
        }
        catch
        {
            // Ignore persistence errors
        }
    }

    private void DeleteTokenFile(string userID)
    {
        try
        {
            var path = Path.Combine(TokenDir, $"token_{SanitizeFilename(userID)}.json");
            if (File.Exists(path))
                File.Delete(path);
        }
        catch
        {
            // Ignore
        }
    }

    private void LoadFromFiles()
    {
        try
        {
            if (!Directory.Exists(TokenDir)) return;

            foreach (var file in Directory.GetFiles(TokenDir, "token_*.json"))
            {
                try
                {
                    var json = File.ReadAllText(file);
                    var token = JsonSerializer.Deserialize<TokenData>(json, PrettyOpts);
                    if (token == null || string.IsNullOrEmpty(token.UserID)) continue;
                    if (token.IsExpired)
                    {
                        try { File.Delete(file); } catch { }
                        continue;
                    }

                    lock (_lock)
                    {
                        if (!_entries.Exists(e => e.Token.UserID == token.UserID))
                            _entries.Add(new TokenEntry(token));
                    }
                }
                catch
                {
                    // Skip malformed entries
                }
            }
        }
        catch
        {
            // Token directory not accessible
        }
    }

    // ═══ Legacy Token Migration ═══

    private void MigrateLegacyTokenFile()
    {
        lock (_lock)
        {
            if (_entries.Count > 0) return;
        }

        try
        {
            var home = Environment.GetFolderPath(Environment.SpecialFolder.UserProfile);
            var legacyPath = Path.Combine(home, ".codebuddy-proxy", "token.json");
            if (!File.Exists(legacyPath)) return;

            var json = File.ReadAllText(legacyPath);
            var token = JsonSerializer.Deserialize<TokenData>(json, new JsonSerializerOptions { PropertyNamingPolicy = JsonNamingPolicy.SnakeCaseLower });
            if (token == null || token.IsExpired)
            {
                try { File.Delete(legacyPath); } catch { }
                return;
            }

            AddToken(token);

            // Rename legacy file
            var backupPath = legacyPath + ".migrated";
            try { File.Move(legacyPath, backupPath, overwrite: true); } catch { }
        }
        catch
        {
            // Ignore migration errors
        }
    }

    private static string SanitizeFilename(string name)
    {
        var invalid = Path.GetInvalidFileNameChars();
        var sb = new StringBuilder(name.Length);
        foreach (var c in name)
            sb.Append(Array.IndexOf(invalid, c) >= 0 ? '_' : c);
        return sb.ToString();
    }

    public void Dispose()
    {
        // Nothing to dispose for file-based persistence
    }
}
