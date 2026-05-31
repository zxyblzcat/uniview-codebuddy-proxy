//go:build !gui

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

	"github.com/gin-gonic/gin"
)

// App holds state for the headless proxy (no system tray).
type App struct {
	mu            sync.Mutex
	server        *http.Server
	logWriter     *logbuf.MultiWriter
	running       bool
	stopCleanupCh chan struct{}
	authPending   bool
}

// New creates a new App instance.
func New(logWriter *logbuf.MultiWriter) *App {
	return &App{
		logWriter:     logWriter,
		stopCleanupCh: make(chan struct{}),
	}
}

// Run starts the proxy in headless mode. Blocks until signal received.
func (a *App) Run() {
	a.RunHeadless()
}

// RunHeadless runs the proxy without a system tray.
func (a *App) RunHeadless() {
	a.startServer()

	sigCh := make(chan os.Signal, 1)
	setupSignalNotify(sigCh)
	<-sigCh
	fmt.Println("\nReceived signal, shutting down...")
	telemetry.Shutdown()
	a.stopServer()
}

func (a *App) startServer() {
	err := a.startServerE()
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func (a *App) startServerE() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.running {
		return nil
	}

	if config.CacheEnabledAtomic() {
		cache.GlobalCache.SetEnabled(true)
		cache.GlobalCache.SetTTL(time.Duration(config.CacheTTLAtomic()) * time.Second)
		log.Printf("Cache enabled (TTL: %ds)", config.CacheTTLAtomic())
	}

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
	a.doCleanup()
	go a.cleanupLoop(a.stopCleanupCh)
	a.running = true
	return nil
}

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
				a.server = srv2
				return nil
			}
		}
		return fmt.Errorf("listen: %w", err)
	case <-time.After(500 * time.Millisecond):
		return nil
	}
}

func isAddrInUse(err error) bool {
	return err != nil && strings.Contains(err.Error(), "address already in use")
}

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

func (a *App) doCleanup() {
	maxBytes := int64(config.LogMaxSizeMBAtomic()) * 1024 * 1024
	if a.logWriter.TruncateIfOver(maxBytes) {
		log.Printf("Log file exceeded %dMB, truncated", config.LogMaxSizeMBAtomic())
	}
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
