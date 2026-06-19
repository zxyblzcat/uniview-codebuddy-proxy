using System;
using System.Collections.Generic;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;

namespace UniviewCodeBuddyProxy.Services;

/// <summary>
/// Thread-safe TTL memory cache with SHA256 key generation.
/// </summary>
public sealed class CacheManager
{
    // ═══ Constants ═══

    private const int CleanupThreshold = 10_000;
    public const int DefaultTTL = 300;

    // ═══ State ═══

    private readonly Dictionary<string, CacheEntry> _store = new();
    private TimeSpan _ttl = TimeSpan.FromSeconds(DefaultTTL);
    private bool _enabled;
    private bool _isCleaningUp;
    private readonly object _lock = new();

    // ═══ Enable/Disable ═══

    public void SetEnabled(bool enabled)
    {
        lock (_lock)
        {
            _enabled = enabled;
            if (!enabled)
                _store.Clear();
        }
    }

    public bool IsEnabled
    {
        get { lock (_lock) { return _enabled; } }
    }

    public void SetTTL(int ttlSeconds)
    {
        lock (_lock) { _ttl = TimeSpan.FromSeconds(ttlSeconds); }
    }

    // ═══ Cache Key ═══

    /// <summary>
    /// Build a cache key from model, messages, tools, temperature, and maxTokens using SHA256.
    /// </summary>
    public static string BuildKey(string model, object? messages, object? tools, double temperature, int maxTokens)
    {
        using var sha = SHA256.Create();
        var modelBytes = Encoding.UTF8.GetBytes(model);
        sha.TransformBlock(modelBytes, 0, modelBytes.Length, null, 0);

        if (messages != null)
        {
            var json = JsonSerializer.Serialize(messages);
            var bytes = Encoding.UTF8.GetBytes(json);
            sha.TransformBlock(bytes, 0, bytes.Length, null, 0);
        }

        if (tools != null)
        {
            var json = JsonSerializer.Serialize(tools);
            var bytes = Encoding.UTF8.GetBytes(json);
            sha.TransformBlock(bytes, 0, bytes.Length, null, 0);
        }

        var tempBytes = Encoding.UTF8.GetBytes(temperature.ToString("R"));
        sha.TransformBlock(tempBytes, 0, tempBytes.Length, null, 0);

        var maxBytes = Encoding.UTF8.GetBytes(maxTokens.ToString());
        sha.TransformBlock(maxBytes, 0, maxBytes.Length, null, 0);

        sha.TransformFinalBlock(Array.Empty<byte>(), 0, 0);
        return Convert.ToHexString(sha.Hash!).ToLowerInvariant();
    }

    // ═══ Get/Set ═══

    /// <summary>Get a cache entry, returns null if not found or expired.</summary>
    public byte[]? Get(string key)
    {
        lock (_lock)
        {
            if (!_enabled) return null;

            if (!_store.TryGetValue(key, out var entry))
                return null;

            if (DateTime.UtcNow >= entry.Expiration)
            {
                TriggerCleanup();
                return null;
            }

            return entry.Data;
        }
    }

    /// <summary>Store a cache entry.</summary>
    public void Set(string key, byte[] value, int? ttlSeconds = null)
    {
        lock (_lock)
        {
            if (!_enabled) return;

            var effectiveTTL = ttlSeconds.HasValue
                ? TimeSpan.FromSeconds(ttlSeconds.Value)
                : _ttl;

            _store[key] = new CacheEntry(value, DateTime.UtcNow + effectiveTTL);

            if (_store.Count > CleanupThreshold)
                TriggerCleanup();
        }
    }

    // ═══ Invalidate/Clear ═══

    /// <summary>Invalidate cache entries matching a prefix.</summary>
    public void Invalidate(string prefix)
    {
        lock (_lock)
        {
            var keysToRemove = new List<string>();
            foreach (var key in _store.Keys)
            {
                if (key.StartsWith(prefix))
                    keysToRemove.Add(key);
            }
            foreach (var key in keysToRemove)
                _store.Remove(key);
        }
    }

    /// <summary>Clear all cache entries.</summary>
    public void Clear()
    {
        lock (_lock)
        {
            _store.Clear();
        }
    }

    // ═══ Cleanup ═══

    private void TriggerCleanup()
    {
        if (_isCleaningUp) return;
        _isCleaningUp = true;

        try
        {
            var now = DateTime.UtcNow;
            var keysToRemove = new List<string>();
            foreach (var (key, entry) in _store)
            {
                if (now >= entry.Expiration)
                    keysToRemove.Add(key);
            }
            foreach (var key in keysToRemove)
                _store.Remove(key);
        }
        finally
        {
            _isCleaningUp = false;
        }
    }

    // ═══ Entry ═══

    private sealed record CacheEntry(byte[] Data, DateTime Expiration);
}
