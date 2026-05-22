package systrayapp

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"codebuddy-proxy/internal/auth"
	"codebuddy-proxy/internal/config"
	"codebuddy-proxy/internal/logbuf"
	"codebuddy-proxy/internal/proxy"
	"codebuddy-proxy/internal/version"

	"fyne.io/systray"
	"github.com/gin-gonic/gin"
)

// App holds all state for the system tray application.
type App struct {
	mu            sync.Mutex
	server        *http.Server
	logWriter     *logbuf.MultiWriter
	authItem      *systray.MenuItem
	autostartItem *systray.MenuItem
	statusItem    *systray.MenuItem
	running       bool
}

// New creates a new App instance.
func New(logWriter *logbuf.MultiWriter) *App {
	return &App{
		logWriter: logWriter,
	}
}

// Run starts the system tray application. This call blocks until the tray exits.
func (a *App) Run() {
	systray.Run(a.onReady, a.onExit)
}

// RunHeadless runs the proxy without a system tray (fallback for environments
// where systray is unavailable, e.g., headless Linux, CI).
func (a *App) RunHeadless() {
	a.startServer()
	a.updateStatus()

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			a.updateStatus()
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	fmt.Println("\nReceived signal, shutting down...")
	a.stopServer()
}

func (a *App) onReady() {
	setIconNormal()

	a.statusItem = systray.AddMenuItem("CodeBuddy Proxy", "Service status")
	a.statusItem.Disable()

	systray.AddSeparator()

	a.authItem = systray.AddMenuItem("🔑 登录", "Start OAuth2 Device Flow")

	logItem := systray.AddMenuItem("📋 查看日志", "Open log viewer in browser")
	go func() {
		for range logItem.ClickedCh {
			auth.OpenBrowser(fmt.Sprintf("http://localhost:%d/_logs", config.Port))
		}
	}()

	restartItem := systray.AddMenuItem("🔄 重启代理", "Restart HTTP server")
	go func() {
		for range restartItem.ClickedCh {
			log.Println("Restarting HTTP server via tray menu...")
			a.stopServer()
			a.startServer()
			log.Println("HTTP server restarted")
		}
	}()

	systray.AddSeparator()

	a.autostartItem = systray.AddMenuItem("⏱ 开机自启", "Toggle autostart")
	if IsAutoStartEnabled() {
		a.autostartItem.Check()
	}
	go func() {
		for range a.autostartItem.ClickedCh {
			if a.autostartItem.Checked() {
				if err := SetAutoStart(false); err != nil {
					log.Printf("Failed to disable autostart: %v", err)
					continue
				}
				a.autostartItem.Uncheck()
				log.Println("Autostart disabled")
			} else {
				if err := SetAutoStart(true); err != nil {
					log.Printf("Failed to enable autostart: %v", err)
					continue
				}
				a.autostartItem.Check()
				log.Println("Autostart enabled")
			}
		}
	}()

	systray.AddSeparator()

	quitItem := systray.AddMenuItem("❌ 退出", "Quit CodeBuddy Proxy")
	go func() {
		for range quitItem.ClickedCh {
			log.Println("Quitting via tray menu...")
			a.stopServer()
			systray.Quit()
		}
	}()

	go func() {
		for range a.authItem.ClickedCh {
			a.handleAuth()
		}
	}()

	a.startServer()
	a.updateStatus()

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			a.updateStatus()
		}
	}()
}

func (a *App) onExit() {
	a.stopServer()
	a.logWriter.Close()
}

func (a *App) handleAuth() {
	td := auth.LoadToken()
	if td != nil {
		return
	}

	authURL, authState, err := auth.FetchAuthURL()
	if err != nil {
		log.Printf("Auth failed: %v", err)
		return
	}

	log.Println("Opening browser for authentication...")
	auth.OpenBrowser(authURL)

	if authState != "" {
		go func() {
			for i := 0; i < 60; i++ {
				result := auth.PollToken(authState)
				if result.Status == "success" {
					log.Printf("Login success! User: %s", result.UserID)
					a.updateStatus()
					return
				}
				if result.Status == "error" {
					log.Printf("Auth poll error: %s", result.Message)
					return
				}
				time.Sleep(3 * time.Second)
			}
			log.Println("Auth poll timed out after 3 minutes")
		}()
	}
}

func (a *App) updateStatus() {
	a.mu.Lock()
	defer a.mu.Unlock()

	td := auth.LoadToken()
	if td != nil {
		setIconNormal()
		if a.authItem != nil {
			a.authItem.SetTitle("✅ 已认证: " + td.UserID)
			a.authItem.SetTooltip("Authenticated as " + td.UserID)
		}
		if a.statusItem != nil {
			a.statusItem.SetTitle("🟢 CodeBuddy Proxy — Running")
		}
	} else {
		setIconError()
		if a.authItem != nil {
			a.authItem.SetTitle("🔑 登录")
			a.authItem.SetTooltip("Start OAuth2 Device Flow")
		}
		if a.statusItem != nil {
			a.statusItem.SetTitle("🔴 CodeBuddy Proxy — Not Authenticated")
		}
	}
}

func (a *App) startServer() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.running {
		return
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), maxBodySize(10<<20))

	auth.RegisterRoutes(r)
	proxy.RegisterRoutes(r)
	RegisterLogViewRoute(r, a.logWriter)

	fmt.Println()
	fmt.Println("==================================================")
	fmt.Printf("  CodeBuddy CN -> OpenAI API Proxy %s\n", version.Version)
	fmt.Printf("  Commit: %s | Built: %s\n", version.Commit, version.Date)
	fmt.Printf("  URL: http://localhost:%d\n", config.Port)
	fmt.Printf("  Auth: http://localhost:%d/auth/start\n", config.Port)
	fmt.Printf("  Logs: http://localhost:%d/_logs\n", config.Port)
	fmt.Println("==================================================")
	fmt.Println()

	if auth.LoadToken() != nil {
		log.Println("Token loaded from cache")
	} else {
		log.Println("No token. Use tray menu to login.")
	}

	srv := &http.Server{
		Addr:         config.ListenAddr(),
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	a.server = srv
	a.running = true
}

func (a *App) stopServer() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.running || a.server == nil {
		return
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer shutdownCancel()
	_ = a.server.Shutdown(shutdownCtx)
	a.server = nil
	a.running = false
	log.Println("HTTP server stopped")
}

func maxBodySize(maxBytes int) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, int64(maxBytes))
		c.Next()
	}
}
