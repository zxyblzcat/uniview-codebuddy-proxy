using System;
using System.Threading;

namespace UniviewCodeBuddyProxy.Services;

/// <summary>Circuit breaker state.</summary>
public enum CircuitBreakerState
{
    Closed,
    Open,
    HalfOpen,
}

/// <summary>
/// Three-state circuit breaker: Closed -> Open -> HalfOpen.
/// Thread-safe via lock. MaxFailures=5, ResetTimeout=30s.
/// </summary>
public sealed class CircuitBreaker
{
    // ═══ Configuration ═══

    public int MaxFailures { get; }
    public TimeSpan ResetTimeout { get; }

    // ═══ State ═══

    private CircuitBreakerState _state = CircuitBreakerState.Closed;
    private int _failureCount;
    private DateTime _lastFailureTime = DateTime.MinValue;
    private int _halfOpenProbes;
    private readonly object _lock = new();

    // ═══ Constructor ═══

    public CircuitBreaker(int maxFailures = Constants.Defaults.CbMaxFailures, int resetTimeoutSecs = Constants.Defaults.CbResetTimeoutSecs)
    {
        MaxFailures = Math.Max(1, maxFailures);
        ResetTimeout = TimeSpan.FromSeconds(Math.Max(1, resetTimeoutSecs));
    }

    // ═══ State Access ═══

    /// <summary>Current effective state (considers time-based transitions).</summary>
    public CircuitBreakerState CurrentState
    {
        get
        {
            lock (_lock) { return EffectiveState(); }
        }
    }

    /// <summary>Consecutive failure count.</summary>
    public int FailureCount
    {
        get { lock (_lock) { return _failureCount; } }
    }

    /// <summary>Last failure time.</summary>
    public DateTime LastFailureTime
    {
        get { lock (_lock) { return _lastFailureTime; } }
    }

    // ═══ Request Control ═══

    /// <summary>
    /// Check if a request is allowed through.
    /// - Closed: always allow
    /// - Open: check if resetTimeout has elapsed, transition to HalfOpen
    /// - HalfOpen: allow up to maxFailures probe requests
    /// </summary>
    public bool AllowRequest()
    {
        lock (_lock)
        {
            switch (EffectiveState())
            {
                case CircuitBreakerState.Closed:
                    return true;

                case CircuitBreakerState.Open:
                    return false;

                case CircuitBreakerState.HalfOpen:
                    var maxProbes = Math.Max(1, MaxFailures);
                    if (_halfOpenProbes < maxProbes)
                    {
                        _halfOpenProbes++;
                        return true;
                    }
                    return false;

                default:
                    return false;
            }
        }
    }

    // ═══ Result Recording ═══

    /// <summary>
    /// Record a successful response. Transitions HalfOpen -> Closed, resets counters.
    /// </summary>
    public void RecordSuccess()
    {
        lock (_lock)
        {
            _failureCount = 0;
            _state = CircuitBreakerState.Closed;
            _halfOpenProbes = 0;
        }
    }

    /// <summary>
    /// Record a failure response.
    /// - Closed: increment failure count, transition to Open if threshold reached
    /// - HalfOpen: transition directly to Open
    /// </summary>
    public void RecordFailure()
    {
        lock (_lock)
        {
            _failureCount++;
            _lastFailureTime = DateTime.UtcNow;

            switch (EffectiveState())
            {
                case CircuitBreakerState.Closed:
                    if (_failureCount >= MaxFailures)
                        _state = CircuitBreakerState.Open;
                    break;

                case CircuitBreakerState.HalfOpen:
                    _state = CircuitBreakerState.Open;
                    _halfOpenProbes = 0;
                    break;

                case CircuitBreakerState.Open:
                    // Already open, update time
                    break;
            }
        }
    }

    // ═══ Reset ═══

    /// <summary>Force reset to Closed state.</summary>
    public void Reset()
    {
        lock (_lock)
        {
            _state = CircuitBreakerState.Closed;
            _failureCount = 0;
            _halfOpenProbes = 0;
            _lastFailureTime = DateTime.MinValue;
        }
    }

    // ═══ Stats ═══

    /// <summary>Return a snapshot of current state.</summary>
    public (CircuitBreakerState State, int Failures, DateTime LastFailure) Stats()
    {
        lock (_lock)
        {
            return (EffectiveState(), _failureCount, _lastFailureTime);
        }
    }

    // ═══ Private ═══

    /// <summary>Compute effective state (considers Open->HalfOpen time transition).</summary>
    private CircuitBreakerState EffectiveState()
    {
        if (_state == CircuitBreakerState.Open)
        {
            var elapsed = DateTime.UtcNow - _lastFailureTime;
            if (elapsed >= ResetTimeout)
            {
                _state = CircuitBreakerState.HalfOpen;
                _halfOpenProbes = 0;
                return CircuitBreakerState.HalfOpen;
            }
            return CircuitBreakerState.Open;
        }
        return _state;
    }
}
