using System;
using System.Threading;
using UniviewCodeBuddyProxy.Services;

namespace UniviewCodeBuddyProxy;

/// <summary>
/// Console entry point for CodeBuddy Proxy Server.
/// Runs the ASP.NET Core Minimal API proxy as a headless service.
/// </summary>
public static class Program
{
    public static void Main(string[] args)
    {
        Console.WriteLine("╔══════════════════════════════════════════╗");
        Console.WriteLine("║     CodeBuddy Proxy Server v1.0.0        ║");
        Console.WriteLine("║     API Gateway for CodeBuddy            ║");
        Console.WriteLine("╚══════════════════════════════════════════╝");
        Console.WriteLine();

        // ═══ Initialize Services ═══

        var logBuffer = new LogBuffer();
        logBuffer.Info("initializing services...");

        var configManager = new ConfigManager();
        var tokenManager = new TokenManager();
        var authService = new AuthService();
        var telemetryReporter = new TelemetryReporter(configManager, tokenManager);
        var usageStats = new UsageStats();

        // ═══ Auto-start Device Flow if no tokens ═══

        if (tokenManager.ActiveTokenCount == 0)
        {
            logBuffer.Info("no tokens found, starting OAuth2 device flow...");
            _ = Task.Run(async () =>
            {
                try
                {
                    var token = await authService.AutoReloginAsync(null, url =>
                    {
                        Console.WriteLine($"  Open this URL to login: {url}");
                        logBuffer.Info($"device flow: {url}");
                    });
                    if (token != null)
                    {
                        tokenManager.AddToken(token);
                        logBuffer.Info($"device flow success: user {token.UserID}");
                    }
                }
                catch (Exception ex)
                {
                    logBuffer.Info($"device flow error: {ex.Message}");
                }
            });
        }
        else
        {
            logBuffer.Info($"loaded {tokenManager.ActiveTokenCount} active token(s)");
        }

        // ═══ Start Proxy Server ═══

        var server = new ProxyServer(
            configManager,
            tokenManager,
            authService,
            logBuffer,
            telemetryReporter,
            usageStats);

        server.Start();

        logBuffer.Info($"proxy server running on http://127.0.0.1:{configManager.Port}");
        Console.WriteLine($"  Proxy:   http://127.0.0.1:{configManager.Port}");
        Console.WriteLine($"  Auth:    http://127.0.0.1:{configManager.Port}/auth/start");
        Console.WriteLine($"  Health:  http://127.0.0.1:{configManager.Port}/health");
        Console.WriteLine();
        Console.WriteLine("Press Ctrl+C to stop.");
        Console.WriteLine();

        // ═══ Wait for Shutdown ═══

        var shutdownEvent = new ManualResetEventSlim(false);
        Console.CancelKeyPress += (_, e) =>
        {
            e.Cancel = true;
            logBuffer.Info("shutdown signal received...");
            shutdownEvent.Set();
        };

        shutdownEvent.Wait();

        // ═══ Cleanup ═══

        logBuffer.Info("stopping proxy server...");
        server.Stop();
        usageStats.Dispose();
        telemetryReporter.Shutdown();
        configManager.Dispose();
        tokenManager.Dispose();

        logBuffer.Info("goodbye!");
    }
}
