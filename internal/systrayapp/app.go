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
	restartItem   *systray.MenuItem
	running       bool
	authPending   bool
	uiCh          chan func() // dispatches UI updates to main goroutine
}

// New creates a new App instance.
func New(logWriter *logbuf.MultiWriter) *App {
	return &App{
		logWriter: logWriter,
		uiCh:      make(chan func(), 32),
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
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	fmt.Println("\nReceived signal, shutting down...")
	a.stopServer()
}

func (a *App) onReady() {
	setIconNormal()

	a.statusItem = systray.AddMenuItem("UniviewCodeBuddyProxy", "服务状态")
	a.statusItem.Disable()

	systray.AddSeparator()

	a.authItem = systray.AddMenuItem("登录", "启动 OAuth2 设备授权流程")

	logItem := systray.AddMenuItem("查看日志", "在浏览器中查看日志")
	go func() {
		for range logItem.ClickedCh {
			auth.OpenBrowser(fmt.Sprintf("http://localhost:%d/_logs", config.Port))
		}
	}()

	a.restartItem = systray.AddMenuItem("重启代理", "重启 HTTP 服务器")
	go func() {
		for range a.restartItem.ClickedCh {
			log.Println("Restarting HTTP server via tray menu...")
			a.dispatchUI(func() {
				a.restartItem.Disable()
				setIconGray()
				setTrayTitle("重启中...")
				a.statusItem.SetTitle("重启中...")
			})

			a.stopServer()
			err := a.startServerE()

			if err != nil {
				a.dispatchUI(func() {
					setIconError()
					setTrayTitle("重启失败")
					a.statusItem.SetTitle("重启失败")
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

	a.autostartItem = systray.AddMenuItem("开机自启", "切换开机自启动")
	if IsAutoStartEnabled() {
		a.autostartItem.Check()
	}
	go func() {
		for range a.autostartItem.ClickedCh {
			if a.autostartItem.Checked() {
				if err := SetAutoStart(false); err != nil {
					log.Printf("Failed to disable autostart: %v", err)
					a.dispatchUI(func() { setTrayTitle("自启设置失败") })
					a.scheduleClearTrayTitle()
					continue
				}
				a.autostartItem.Uncheck()
				log.Println("Autostart disabled")
				a.dispatchUI(func() { setTrayTitle("已关闭自启") })
				a.scheduleClearTrayTitle()
			} else {
				if err := SetAutoStart(true); err != nil {
					log.Printf("Failed to enable autostart: %v", err)
					a.dispatchUI(func() { setTrayTitle("自启设置失败") })
					a.scheduleClearTrayTitle()
					continue
				}
				a.autostartItem.Check()
				log.Println("Autostart enabled")
				a.dispatchUI(func() { setTrayTitle("已开启自启") })
				a.scheduleClearTrayTitle()
			}
		}
	}()

	systray.AddSeparator()

	quitItem := systray.AddMenuItem("退出", "退出 UniviewCodeBuddyProxy")
	go func() {
		for range quitItem.ClickedCh {
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
	a.stopServer()
	a.logWriter.Close()
}

func (a *App) handleAuth() {
	td := auth.LoadToken()
	if td != nil {
		return
	}
	if a.authPending {
		log.Println("Auth already in progress")
		return
	}

	authURL, authState, err := auth.FetchAuthURL()
	if err != nil {
		log.Printf("Auth failed: %v", err)
		return
	}

	a.authPending = true
	log.Println("Opening browser for authentication...")
	auth.OpenBrowser(authURL)

	if authState != "" {
		go func() {
			defer func() { a.authPending = false }()
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
// Safe to call from any goroutine.
func (a *App) dispatchUI(fn func()) {
	select {
	case a.uiCh <- fn:
	default:
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
			a.authItem.SetTitle("退出登录 (" + td.UserID + ")")
			a.authItem.SetTooltip("退出登录")
		}
		if a.statusItem != nil {
			a.statusItem.SetTitle("运行中")
		}
	} else {
		setIconGray()
		if a.authItem != nil {
			a.authItem.SetTitle("登录")
			a.authItem.SetTooltip("启动 OAuth2 设备授权流程")
		}
		if a.statusItem != nil {
			a.statusItem.SetTitle("未认证")
		}
	}
}

func (a *App) startServer() {
	err := a.startServerE()
	if err != nil {
		log.Printf("Failed to start server: %v", err)
		a.dispatchUI(func() {
			setIconError()
			setTrayTitle("启动失败")
			if a.statusItem != nil {
				a.statusItem.SetTitle("启动失败: " + err.Error())
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

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), maxBodySize(10<<20))

	auth.RegisterRoutes(r)
	proxy.RegisterRoutes(r)
	RegisterLogViewRoute(r, a.logWriter)

	log.Println("==================================================")
	log.Printf("  UniviewCodeBuddy Proxy %s", version.Version)
	log.Printf("  Commit: %s | Built: %s", version.Commit, version.Date)
	log.Printf("  URL: http://localhost:%d", config.Port)
	log.Printf("  Auth: http://localhost:%d/auth/start", config.Port)
	log.Printf("  Logs: http://localhost:%d/_logs", config.Port)
	log.Println("==================================================")

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

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("listen: %w", err)
	}

	log.Printf("HTTP server listening on :%d", config.Port)
	a.server = srv
	a.running = true
	return nil
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
