package systrayapp

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"uniview-codebuddy-proxy/internal/auth"
	"uniview-codebuddy-proxy/internal/cache"
	"uniview-codebuddy-proxy/internal/config"
	"uniview-codebuddy-proxy/internal/i18n"
	"uniview-codebuddy-proxy/internal/logbuf"
	"uniview-codebuddy-proxy/internal/proxy"
	"uniview-codebuddy-proxy/internal/telemetry"
	"uniview-codebuddy-proxy/internal/web"
	"uniview-codebuddy-proxy/internal/version"

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
	restartItem   *systray.MenuItem
	logItem       *systray.MenuItem
	adminItem     *systray.MenuItem
	quitItem      *systray.MenuItem
	running       bool
	stopCleanupCh chan struct{}
	authPending   bool
	uiCh          chan func() // dispatches UI updates to main goroutine
}

// New creates a new App instance.
func New(logWriter *logbuf.MultiWriter) *App {
	return &App{
		logWriter:     logWriter,
		uiCh:          make(chan func(), 32),
		stopCleanupCh: make(chan struct{}),
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
	a.applyStatus()

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			a.applyStatus()
		}
	}()

	sigCh := make(chan os.Signal, 1)
	setupSignalNotify(sigCh)
	<-sigCh
	fmt.Println("\nReceived signal, shutting down...")
	telemetry.Shutdown()
	a.stopServer()
}

func (a *App) onReady() {
	setIconNormal()

	a.statusItem = systray.AddMenuItem("UniviewCodeBuddyProxy", i18n.T("menu.status_tooltip"))
	a.statusItem.Disable()

	systray.AddSeparator()

	a.authItem = systray.AddMenuItem(i18n.T("menu.login"), i18n.T("menu.login_tooltip"))

	a.logItem = systray.AddMenuItem(i18n.T("menu.view_logs"), i18n.T("menu.view_logs_tooltip"))
	go func() {
		for range a.logItem.ClickedCh {
			auth.OpenBrowser(fmt.Sprintf("http://localhost:%d/admin/logs", config.Port))
		}
	}()

	a.adminItem = systray.AddMenuItem(i18n.T("menu.admin_panel"), i18n.T("menu.admin_panel_tooltip"))
	go func() {
		for range a.adminItem.ClickedCh {
			auth.OpenBrowser(fmt.Sprintf("http://localhost:%d/admin", config.Port))
		}
	}()

	a.restartItem = systray.AddMenuItem(i18n.T("menu.restart"), i18n.T("menu.restart_tooltip"))
	go func() {
		for range a.restartItem.ClickedCh {
			log.Println("Restarting HTTP server via tray menu...")
			a.dispatchUI(func() {
				a.restartItem.Disable()
				setIconGray()
				setTrayTitle(i18n.T("status.restarting"))
				a.statusItem.SetTitle(i18n.T("status.restarting"))
			})

			a.stopServer()
			err := a.startServerE()

			if err != nil {
				a.dispatchUI(func() {
					setIconError()
					setTrayTitle(i18n.T("status.restart_failed"))
					a.statusItem.SetTitle(i18n.T("status.restart_failed"))
					a.restartItem.Enable()
				})
				log.Printf("HTTP server restart failed: %v", err)
			} else {
				a.scheduleStatusUpdate()
				a.dispatchUI(func() {
					a.restartItem.Enable()
				})
				log.Println("HTTP server restarted")
			}
		}
	}()

	systray.AddSeparator()

	a.autostartItem = systray.AddMenuItem(i18n.T("menu.autostart"), i18n.T("menu.autostart_tooltip"))
	if IsAutoStartEnabled() {
		a.autostartItem.Check()
	}
	go func() {
		for range a.autostartItem.ClickedCh {
			if a.autostartItem.Checked() {
				if err := SetAutoStart(false); err != nil {
					log.Printf("Failed to disable autostart: %v", err)
					a.dispatchUI(func() { setTrayTitle(i18n.T("status.autostart_failed")) })
					a.scheduleClearTrayTitle()
					continue
				}
				a.autostartItem.Uncheck()
				log.Println("Autostart disabled")
				a.dispatchUI(func() { setTrayTitle(i18n.T("status.autostart_disabled")) })
				a.scheduleClearTrayTitle()
			} else {
				if err := SetAutoStart(true); err != nil {
					log.Printf("Failed to enable autostart: %v", err)
					a.dispatchUI(func() { setTrayTitle(i18n.T("status.autostart_failed")) })
					a.scheduleClearTrayTitle()
					continue
				}
				a.autostartItem.Check()
				log.Println("Autostart enabled")
				a.dispatchUI(func() { setTrayTitle(i18n.T("status.autostart_enabled")) })
				a.scheduleClearTrayTitle()
			}
		}
	}()

	systray.AddSeparator()

	a.quitItem = systray.AddMenuItem(i18n.T("menu.quit"), i18n.T("menu.quit_tooltip"))
	go func() {
		for range a.quitItem.ClickedCh {
			log.Println("Quitting via tray menu...")
			a.stopServer()
			systray.Quit()
		}
	}()

	go func() {
		for range a.authItem.ClickedCh {
			td := auth.LoadToken()
			if td != nil {
				auth.ClearToken()
				a.scheduleStatusUpdate()
			} else {
				a.handleAuth()
			}
		}
	}()

	// Register locale change callback to rebuild tray menu
	i18n.OnChange(a.rebuildMenu)

	a.startServer()

	// Dispatch UI updates on main goroutine to avoid Cocoa threading issues
	go func() {
		for fn := range a.uiCh {
			fn()
		}
	}()
	a.scheduleStatusUpdate()

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			a.scheduleStatusUpdate()
		}
	}()
}

func (a *App) onExit() {
	telemetry.Shutdown()
	a.stopServer()
	a.logWriter.Close()
}

func (a *App) handleAuth() {
	td := auth.LoadToken()
	if td != nil {
		return
	}
	a.mu.Lock()
	if a.authPending {
		a.mu.Unlock()
		log.Println("Auth already in progress")
		return
	}
	a.authPending = true
	a.mu.Unlock()

	authURL, authState, err := auth.FetchAuthURL()
	if err != nil {
		a.mu.Lock()
		a.authPending = false
		a.mu.Unlock()
		log.Printf("Auth failed: %v", err)
		return
	}

	log.Println("Opening browser for authentication...")
	auth.OpenBrowser(authURL)

	if authState != "" {
		go func() {
			defer func() {
				a.mu.Lock()
				a.authPending = false
				a.mu.Unlock()
			}()
			for i := 0; i < 60; i++ {
				result := auth.PollToken(authState)
				if result.Status == "success" {
					log.Printf("Login success! User: %s", result.UserID)
					a.scheduleStatusUpdate()
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

// scheduleStatusUpdate sends a status update request to the main goroutine.
// Safe to call from any goroutine.
func (a *App) scheduleStatusUpdate() {
	a.dispatchUI(a.applyStatus)
}

// dispatchUI schedules a function to run on the main goroutine.
// Safe to call from any goroutine. Logs a warning if the channel is full.
func (a *App) dispatchUI(fn func()) {
	select {
	case a.uiCh <- fn:
	default:
		log.Println("Warning: UI update channel full, dropping update")
	}
}

// scheduleClearTrayTitle clears the tray bar title after a short delay.
func (a *App) scheduleClearTrayTitle() {
	time.AfterFunc(3*time.Second, func() {
		a.dispatchUI(func() { setTrayTitle("") })
	})
}

// applyStatus updates tray icon and menu items. Must be called from the main goroutine.
func (a *App) applyStatus() {
	td := auth.LoadToken()
	if td != nil {
		setIconNormal()
		if a.authItem != nil {
			a.authItem.SetTitle(i18n.T("menu.logout") + " (" + td.UserID + ")")
			a.authItem.SetTooltip(i18n.T("menu.logout_tooltip"))
		}
		if a.statusItem != nil {
			a.statusItem.SetTitle(i18n.T("status.running"))
		}
	} else {
		setIconGray()
		if a.authItem != nil {
			a.authItem.SetTitle(i18n.T("menu.login"))
			a.authItem.SetTooltip(i18n.T("menu.login_tooltip"))
		}
		if a.statusItem != nil {
			a.statusItem.SetTitle(i18n.T("status.not_authenticated"))
		}
	}
}

func (a *App) rebuildMenu() {
	a.statusItem.SetTooltip(i18n.T("menu.status_tooltip"))
	if a.authItem != nil {
		a.authItem.SetTitle(i18n.T("menu.login"))
		a.authItem.SetTooltip(i18n.T("menu.login_tooltip"))
	}
	if a.logItem != nil {
		a.logItem.SetTitle(i18n.T("menu.view_logs"))
		a.logItem.SetTooltip(i18n.T("menu.view_logs_tooltip"))
	}
	if a.adminItem != nil {
		a.adminItem.SetTitle(i18n.T("menu.admin_panel"))
		a.adminItem.SetTooltip(i18n.T("menu.admin_panel_tooltip"))
	}
	if a.restartItem != nil {
		a.restartItem.SetTitle(i18n.T("menu.restart"))
		a.restartItem.SetTooltip(i18n.T("menu.restart_tooltip"))
	}
	if a.autostartItem != nil {
		a.autostartItem.SetTitle(i18n.T("menu.autostart"))
		a.autostartItem.SetTooltip(i18n.T("menu.autostart_tooltip"))
	}
	if a.quitItem != nil {
		a.quitItem.SetTitle(i18n.T("menu.quit"))
		a.quitItem.SetTooltip(i18n.T("menu.quit_tooltip"))
	}
	a.applyStatus()
}

func (a *App) startServer() {
	err := a.startServerE()
	if err != nil {
		log.Printf("Failed to start server: %v", err)
		a.dispatchUI(func() {
			setIconError()
			setTrayTitle(i18n.T("status.startup_failed"))
			if a.statusItem != nil {
				a.statusItem.SetTitle(i18n.T("status.startup_failed") + ": " + err.Error())
			}
		})
		time.AfterFunc(5*time.Second, systray.Quit)
	}
}

func (a *App) startServerE() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.running {
		return nil
	}

	// 初始化缓存
	if config.CacheEnabledAtomic() {
		cache.GlobalCache.SetEnabled(true)
		cache.GlobalCache.SetTTL(time.Duration(config.CacheTTLAtomic()) * time.Second)
		log.Printf("Cache enabled (TTL: %ds)", config.CacheTTLAtomic())
	}

	// 初始化遥测上报服务
	telemetry.Init()

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), maxBodySize(10<<20))

	auth.RegisterRoutes(r)
	proxy.RegisterRoutes(r)
	web.RegisterAPIRoutes(r, a.logWriter)
	web.SetupAdminUI(r)

	log.Println("==================================================")
	log.Printf("  UniviewCodeBuddy Proxy %s", version.Version)
	log.Printf("  Commit: %s | Built: %s", version.Commit, version.Date)
	log.Println(i18n.T("banner.url", map[string]interface{}{"port": config.Port}))
	log.Println(i18n.T("banner.auth", map[string]interface{}{"port": config.Port}))
	log.Println(i18n.T("banner.logs", map[string]interface{}{"port": config.Port}))
	log.Println(i18n.T("banner.admin", map[string]interface{}{"port": config.Port}))
	log.Println("==================================================")

	if auth.LoadToken() != nil {
		log.Println(i18n.T("banner.token_loaded"))
	} else {
		log.Println(i18n.T("banner.no_token"))
	}

	srv := &http.Server{
		Addr:        config.ListenAddr(),
		Handler:     r,
		ReadTimeout: 30 * time.Second,
		IdleTimeout: 120 * time.Second,
	}

	if err := a.listenWithRetry(srv); err != nil {
		return err
	}

	log.Printf("HTTP server listening on :%d", config.Port)
	a.server = srv

	// 启动时执行一次清理
	a.doCleanup()

	// 启动后台定时清理 goroutine
	go a.cleanupLoop(a.stopCleanupCh)

	a.running = true
	return nil
}

// listenWithRetry starts the HTTP server and retries once if the port is in use.
// On retry, creates a new http.Server since ListenAndServe cannot be called twice on the same instance.
func (a *App) listenWithRetry(srv *http.Server) error {
	listenErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			listenErr <- err
			return
		}
		close(listenErr)
	}()

	select {
	case err := <-listenErr:
		if isAddrInUse(err) {
			log.Printf("Port %d is in use, killing occupying process...", config.Port)
			if killErr := killProcessOnPort(config.Port); killErr != nil {
				log.Printf("Failed to kill process on port %d: %v", config.Port, killErr)
				return fmt.Errorf("listen: %w", err)
			}
			log.Printf("Port %d freed, retrying...", config.Port)

			// Create a new server for retry (ListenAndServe cannot be called twice on same instance)
			srv2 := &http.Server{
				Addr:        srv.Addr,
				Handler:     srv.Handler,
				ReadTimeout: srv.ReadTimeout,
				IdleTimeout: srv.IdleTimeout,
			}
			listenErr2 := make(chan error, 1)
			go func() {
				if err := srv2.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					listenErr2 <- err
					return
				}
				close(listenErr2)
			}()

			select {
			case err2 := <-listenErr2:
				return fmt.Errorf("listen after kill: %w", err2)
			case <-time.After(500 * time.Millisecond):
				// Update server reference to the new instance
				a.server = srv2
				return nil
			}
		}
		return fmt.Errorf("listen: %w", err)
	case <-time.After(500 * time.Millisecond):
		return nil
	}
}

// isAddrInUse checks if the error is "address already in use".
func isAddrInUse(err error) bool {
	return err != nil && strings.Contains(err.Error(), "address already in use")
}

// cleanupLoop runs periodic cleanup for logs and expired token files.
// It runs until stopCh is closed.
func (a *App) cleanupLoop(stopCh <-chan struct{}) {
	interval := time.Duration(config.LogCleanupIntervalAtomic()) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			a.doCleanup()
		}
	}
}

// doCleanup performs one round of log truncation and expired token file cleanup.
func (a *App) doCleanup() {
	// 日志大小检查与截断
	maxBytes := int64(config.LogMaxSizeMBAtomic()) * 1024 * 1024
	if a.logWriter.TruncateIfOver(maxBytes) {
		log.Printf("Log file exceeded %dMB, truncated", config.LogMaxSizeMBAtomic())
	}

	// 过期 Token 文件清理
	if removed, err := auth.CleanupExpiredTokenFiles(); err != nil {
		log.Printf("Token cleanup error: %v", err)
	} else if removed > 0 {
		log.Printf("Cleaned up %d expired token file(s)", removed)
	}
}

func (a *App) stopServer() {
	a.mu.Lock()
	if !a.running || a.server == nil {
		a.mu.Unlock()
		return
	}
	srv := a.server
	a.server = nil
	a.running = false
	close(a.stopCleanupCh)
	a.stopCleanupCh = make(chan struct{})
	a.mu.Unlock()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
	log.Println("HTTP server stopped")
}

func maxBodySize(maxBytes int) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, int64(maxBytes))
		c.Next()
	}
}
